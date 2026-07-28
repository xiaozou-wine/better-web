package store

import (
	"testing"

	"better-web/internal/model"
)

// marshalOptional 必须把每种"空值"都映射成 SQL NULL。
//
// 这里锁住一个易犯的错误：新增可选字段时若忘了在 marshalOptional 的 switch
// 里加一个 case，nil 指针会落到默认分支被 json.Marshal 成字符串 "null"，
// 入库后读取时再按 JSON 解析——得到的不是 nil 而是一个零值结构体，
// 且不报错。表现为"配置明明没设置，读出来却有个空配置"。
func TestMarshalOptionalMapsEmptyValuesToNull(t *testing.T) {
	cases := map[string]any{
		"nil":              nil,
		"nil *Proxy":       (*model.Proxy)(nil),
		"nil *Geo":         (*model.Geo)(nil),
		"nil *Startup":     (*model.Startup)(nil),
		"空 []string":       []string(nil),
		"空 SpoofTarget 切片": []model.SpoofTarget(nil),
		"长度为 0 的切片":        []string{},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := marshalOptional(in)
			if err != nil {
				t.Fatalf("marshalOptional 失败: %v", err)
			}
			if got != nil {
				t.Errorf("= %#v, 期望 nil（即 SQL NULL）", got)
			}
		})
	}
}

// 非空值应被序列化成 JSON 字符串。
func TestMarshalOptionalSerializesNonEmpty(t *testing.T) {
	cases := map[string]any{
		"Proxy":   &model.Proxy{Scheme: model.ProxySOCKS5, Host: "h", Port: 1},
		"Startup": &model.Startup{Mode: model.StartupURLs, URLs: []string{"https://a"}},
		"标签":      []string{"a"},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := marshalOptional(in)
			if err != nil {
				t.Fatalf("marshalOptional 失败: %v", err)
			}
			s, ok := got.(string)
			if !ok {
				t.Fatalf("= %#v, 期望 string", got)
			}
			if s == "" || s == "null" {
				t.Errorf("= %q, 非空值不应序列化成空或 null", s)
			}
		})
	}
}

// 未设置启动页配置的 profile 读回后必须仍是 nil，不能变成零值结构体。
func TestNilStartupRoundTripsAsNil(t *testing.T) {
	s := openTemp(t)
	if err := s.Save(&model.Profile{
		ID: "x", Name: "无启动页", Kind: model.KindDaily, ProfileDir: "d",
	}); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}
	got, err := s.Get("x")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if got.Startup != nil {
		t.Errorf("Startup = %#v, 期望 nil", got.Startup)
	}
}

func TestStartupRoundTrip(t *testing.T) {
	s := openTemp(t)
	want := &model.Startup{
		Mode:      model.StartupURLs,
		URLs:      []string{"https://example.com", "https://example.org"},
		NewTabURL: "https://newtab.example",
	}
	if err := s.Save(&model.Profile{
		ID: "x", Name: "带启动页", Kind: model.KindDaily, ProfileDir: "d",
		Startup: want,
	}); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}

	got, err := s.Get("x")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if got.Startup == nil {
		t.Fatal("Startup 丢失")
	}
	if got.Startup.Mode != want.Mode {
		t.Errorf("Mode = %q, 期望 %q", got.Startup.Mode, want.Mode)
	}
	if len(got.Startup.URLs) != 2 || got.Startup.URLs[0] != want.URLs[0] {
		t.Errorf("URLs = %v", got.Startup.URLs)
	}
	if got.Startup.NewTabURL != want.NewTabURL {
		t.Errorf("NewTabURL = %q", got.Startup.NewTabURL)
	}
}
