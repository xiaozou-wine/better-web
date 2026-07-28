package geo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseOrgExtractsASN(t *testing.T) {
	cases := []struct {
		in       string
		wantASN  int
		wantName string
	}{
		{"AS16509 Amazon.com, Inc.", 16509, "Amazon.com, Inc."},
		{"AS7922 Comcast Cable Communications, LLC", 7922, "Comcast Cable Communications, LLC"},
		{"AS14061 DigitalOcean, LLC", 14061, "DigitalOcean, LLC"},
		// 无 ASN 前缀时应原样返回组织名，仍可据名字判定类型。
		{"Comcast Cable", 0, "Comcast Cable"},
		{"", 0, ""},
		// 前缀畸形不应误当成 ASN。
		{"ASXYZ Foo", 0, "ASXYZ Foo"},
	}
	for _, c := range cases {
		asn, name := ParseOrg(c.in)
		if asn != c.wantASN || name != c.wantName {
			t.Errorf("ParseOrg(%q) = (%d, %q), 期望 (%d, %q)",
				c.in, asn, name, c.wantASN, c.wantName)
		}
	}
}

func TestClassifyOrgDetectsHosting(t *testing.T) {
	hosting := []string{
		"Amazon.com, Inc.",
		"Amazon Technologies Inc.",
		"Google LLC",
		"Microsoft Corporation",
		"DigitalOcean, LLC",
		"Hetzner Online GmbH",
		"OVH SAS",
		"Vultr Holdings LLC",
		"Contabo GmbH",
		"M247 Europe SRL",
		"Alibaba Cloud LLC",
		"Some Random Hosting Provider",
		"Example Datacenter Ltd",
		// 回归：这些主机商名称不含任何通用托管词。
		// CYBERCON 是实测中被漏判成 unknown 的真实案例。
		"CYBERCON, INC.",
		"QuadraNet Enterprises LLC",
		"Psychz Networks",
		"FranTech Solutions",
	}
	for _, org := range hosting {
		if got := ClassifyOrg(org); got != IPKindHosting {
			t.Errorf("ClassifyOrg(%q) = %q, 期望 hosting", org, got)
		}
	}
}

func TestClassifyOrgDetectsResidential(t *testing.T) {
	residential := []string{
		"Comcast Cable Communications, LLC",
		"Verizon Business",
		"AT&T Services, Inc.",
		"Charter Communications",
		"Deutsche Telekom AG",
		"Vodafone GmbH",
		"China Telecom",
		"Sky Broadband",
		"Virgin Media Limited",
		"Some Wireless Carrier",
	}
	for _, org := range residential {
		if got := ClassifyOrg(org); got != IPKindResidential {
			t.Errorf("ClassifyOrg(%q) = %q, 期望 residential", org, got)
		}
	}
}

// 住宅关键词必须优先于托管关键词：部分正规 ISP 名称含 cloud 之类的噪声词，
// 若托管优先会把真住宅误判成机房，把用户无谓地拦下。
func TestClassifyOrgPrefersResidentialOnConflict(t *testing.T) {
	conflicting := []string{
		"CloudTel Broadband Services", // 同时含 cloud 与 broadband
		"Telecom Cloud Services",      // 同时含 telecom 与 cloud
	}
	for _, org := range conflicting {
		if got := ClassifyOrg(org); got != IPKindResidential {
			t.Errorf("ClassifyOrg(%q) = %q, 期望 residential（住宅词优先）", org, got)
		}
	}
}

// 判不出来必须是 unknown，且 unknown 要算作有风险——
// 信息不足时提醒用户，而非默认放行。
func TestClassifyOrgUnknownIsRisky(t *testing.T) {
	for _, org := range []string{"", "   ", "Foobar Ltd", "未知组织"} {
		if got := ClassifyOrg(org); got != IPKindUnknown {
			t.Errorf("ClassifyOrg(%q) = %q, 期望 unknown", org, got)
		}
	}
	if !IPKindUnknown.Risky() {
		t.Error("unknown 应视为有风险")
	}
	if !IPKindHosting.Risky() {
		t.Error("hosting 应视为有风险")
	}
	if IPKindResidential.Risky() {
		t.Error("residential 不应视为有风险")
	}
}

// LookupExit 应同时返回地理信息与 ASN 判定。
func TestLookupExitReturnsASNInfo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"ip": "203.0.113.7", "country": "US", "region": "California",
			"org": "AS16509 Amazon.com, Inc.",
		})
	}))
	defer srv.Close()

	r := &Resolver{
		Client:    srv.Client(),
		Endpoints: []Endpoint{withURL(DefaultEndpoints[0], srv.URL)},
	}
	info, err := r.LookupExit(context.Background())
	if err != nil {
		t.Fatalf("LookupExit 失败: %v", err)
	}
	if info.IP != "203.0.113.7" {
		t.Errorf("IP = %q", info.IP)
	}
	if info.ASN != 16509 {
		t.Errorf("ASN = %d, 期望 16509", info.ASN)
	}
	if info.Kind != IPKindHosting {
		t.Errorf("Kind = %q, 期望 hosting", info.Kind)
	}
	if info.Geo.CountryCode != "US" {
		t.Errorf("国家码 = %q", info.Geo.CountryCode)
	}
}

// 服务不返回 ASN 字段时，地理信息仍须有效，Kind 退化为 unknown。
func TestLookupExitToleratesMissingASN(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"country": "JP", "region": "Tokyo",
		})
	}))
	defer srv.Close()

	r := &Resolver{
		Client:    srv.Client(),
		Endpoints: []Endpoint{withURL(DefaultEndpoints[0], srv.URL)},
	}
	info, err := r.LookupExit(context.Background())
	if err != nil {
		t.Fatalf("LookupExit 失败: %v", err)
	}
	if info.Geo.CountryCode != "JP" {
		t.Errorf("国家码 = %q, 期望 JP", info.Geo.CountryCode)
	}
	if info.Kind != IPKindUnknown {
		t.Errorf("Kind = %q, 期望 unknown", info.Kind)
	}
}

// 全部服务失败时必须报错，不能返回零值让调用方误以为查到了。
func TestLookupExitFailsWhenAllEndpointsFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	r := &Resolver{
		Client:    srv.Client(),
		Endpoints: []Endpoint{withURL(DefaultEndpoints[0], srv.URL)},
	}
	if _, err := r.LookupExit(context.Background()); err == nil {
		t.Error("全部服务失败时期望报错")
	}
}

// withURL 把 endpoint 指向测试服务器，保留其解析函数。
func withURL(ep Endpoint, url string) Endpoint {
	ep.URL = url
	return ep
}
