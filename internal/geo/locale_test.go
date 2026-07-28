package geo

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// 美国跨 6 个时区，只靠国家码会大量出错，而美国又是最常见的代理出口地。
func TestResolveUSUsesRegionTimezone(t *testing.T) {
	cases := map[string]string{
		"CA": "America/Los_Angeles",
		"NY": "America/New_York",
		"TX": "America/Chicago",
		"CO": "America/Denver",
		"AZ": "America/Phoenix",
		"HI": "Pacific/Honolulu",
		"AK": "America/Anchorage",
	}
	for region, wantTZ := range cases {
		g := Resolve("US", region)
		if g.Timezone != wantTZ {
			t.Errorf("Resolve(US, %s).Timezone = %q, 期望 %q", region, g.Timezone, wantTZ)
		}
		if g.Locale != "en-US" {
			t.Errorf("Resolve(US, %s).Locale = %q, 期望 en-US", region, g.Locale)
		}
	}
}

// 无地区码时落到东部默认值，这是美国最大的人口时区。
func TestResolveUSWithoutRegion(t *testing.T) {
	g := Resolve("US", "")
	if g.Timezone != "America/New_York" {
		t.Errorf("时区 = %q, 期望 America/New_York", g.Timezone)
	}
}

func TestResolveKnownCountries(t *testing.T) {
	cases := map[string]struct{ tz, locale string }{
		"JP": {"Asia/Tokyo", "ja-JP"},
		"DE": {"Europe/Berlin", "de-DE"},
		"BR": {"America/Sao_Paulo", "pt-BR"},
		"GB": {"Europe/London", "en-GB"},
		"SG": {"Asia/Singapore", "en-SG"},
	}
	for cc, want := range cases {
		g := Resolve(cc, "")
		if g.Timezone != want.tz || g.Locale != want.locale {
			t.Errorf("Resolve(%s) = %q/%q, 期望 %q/%q", cc, g.Timezone, g.Locale, want.tz, want.locale)
		}
		if g.CountryCode != cc {
			t.Errorf("Resolve(%s).CountryCode = %q", cc, g.CountryCode)
		}
	}
}

// 未收录的国家保留真实国家码便于排查，但时区语言走兜底而非留空。
func TestResolveUnknownCountryKeepsCodeAndFallsBack(t *testing.T) {
	g := Resolve("ZZ", "")
	if g.CountryCode != "ZZ" {
		t.Errorf("CountryCode = %q, 期望保留 ZZ", g.CountryCode)
	}
	if g.Timezone == "" || g.Locale == "" {
		t.Errorf("未收录国家的时区/语言不该为空: %+v", g)
	}
}

func TestResolveEmptyCountryUsesFallback(t *testing.T) {
	if g := Resolve("", ""); g != Fallback() {
		t.Errorf("Resolve(\"\") = %+v, 期望兜底值 %+v", g, Fallback())
	}
}

// 所有收录条目都必须是可被 time.LoadLocation 解析的合法 IANA 时区名，
// 拼错的时区名会被内核拒绝或产生错误时间。
func TestAllTimezonesAreValidIANA(t *testing.T) {
	seen := map[string]bool{}
	collect := func(tz string) {
		if seen[tz] {
			return
		}
		seen[tz] = true
		if _, err := time.LoadLocation(tz); err != nil {
			t.Errorf("时区 %q 无法解析: %v", tz, err)
		}
	}
	for _, d := range countryDefault {
		collect(d.timezone)
	}
	for _, tz := range usTimezoneByRegion {
		collect(tz)
	}
	collect(Fallback().Timezone)
}

func TestLookupUsesFirstWorkingEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"country":"JP","region":"Tokyo"}`))
	}))
	defer srv.Close()

	r := &Resolver{
		Client: srv.Client(),
		Endpoints: []Endpoint{
			{Name: "坏的", URL: "http://127.0.0.1:1/", Parse: DefaultEndpoints[0].Parse},
			{Name: "好的", URL: srv.URL, Parse: DefaultEndpoints[0].Parse},
		},
	}
	g, err := r.Lookup(context.Background())
	if err != nil {
		t.Fatalf("Lookup 失败: %v", err)
	}
	if g.CountryCode != "JP" || g.Timezone != "Asia/Tokyo" {
		t.Errorf("Lookup = %+v, 期望 JP/Asia/Tokyo", g)
	}
}

// 全部服务失败必须明确报错，让上层决定中止还是回退，不能静默给个默认值。
func TestLookupReportsErrorWhenAllEndpointsFail(t *testing.T) {
	r := &Resolver{
		Client: &http.Client{Timeout: time.Second},
		Endpoints: []Endpoint{
			{Name: "坏1", URL: "http://127.0.0.1:1/", Parse: DefaultEndpoints[0].Parse},
			{Name: "坏2", URL: "http://127.0.0.1:2/", Parse: DefaultEndpoints[0].Parse},
		},
	}
	if _, err := r.Lookup(context.Background()); !errors.Is(err, ErrAllEndpointsFailed) {
		t.Errorf("期望 ErrAllEndpointsFailed, 实际 %v", err)
	}
}

// 响应里没有国家码时要跳到下一个服务，不能当成成功。
func TestLookupSkipsResponseWithoutCountry(t *testing.T) {
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer empty.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"country":"DE"}`))
	}))
	defer good.Close()

	r := &Resolver{
		Client: empty.Client(),
		Endpoints: []Endpoint{
			{Name: "空", URL: empty.URL, Parse: DefaultEndpoints[0].Parse},
			{Name: "有", URL: good.URL, Parse: DefaultEndpoints[0].Parse},
		},
	}
	g, err := r.Lookup(context.Background())
	if err != nil {
		t.Fatalf("Lookup 失败: %v", err)
	}
	if g.CountryCode != "DE" {
		t.Errorf("CountryCode = %q, 期望 DE", g.CountryCode)
	}
}

func TestParseCloudflareTrace(t *testing.T) {
	body := []byte("fl=123abc\nh=cloudflare.com\nip=1.2.3.4\nloc=NL\ntls=TLSv1.3\n")
	country, region, err := parseCloudflareTrace(body)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if country != "NL" {
		t.Errorf("国家码 = %q, 期望 NL", country)
	}
	// trace 接口不提供地区码。
	if region != "" {
		t.Errorf("地区码 = %q, 期望空", region)
	}
}

func TestParseCloudflareTraceMissingLoc(t *testing.T) {
	if _, _, err := parseCloudflareTrace([]byte("ip=1.2.3.4\n")); err == nil {
		t.Error("缺少 loc 字段时期望报错")
	}
}

// 超长响应必须报错而不是静默截断，截断的 JSON 会在解析阶段报出难定位的错误。
func TestLookupRejectsOversizedResponse(t *testing.T) {
	huge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 128<<10)
		for i := range buf {
			buf[i] = 'x'
		}
		_, _ = w.Write(buf)
	}))
	defer huge.Close()

	r := &Resolver{
		Client:    huge.Client(),
		Endpoints: []Endpoint{{Name: "超大", URL: huge.URL, Parse: DefaultEndpoints[0].Parse}},
	}
	if _, err := r.Lookup(context.Background()); err == nil {
		t.Error("超长响应期望报错")
	}
}
