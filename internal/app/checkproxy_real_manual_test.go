package app

import (
	"context"
	"net"
	"os"
	"strconv"
	"testing"

	"better-web/internal/model"
)

// 用真实可用的代理验证 CheckProxy 的完整判定链路。
//
// 默认跳过：需要一个可用的上游代理。用 BW_TEST_PROXY 指定，例如：
//
//	BW_TEST_PROXY=socks5://127.0.0.1:10808 go test -run TestCheckProxyReal -v ./internal/app/
func TestCheckProxyRealUpstream(t *testing.T) {
	raw := os.Getenv("BW_TEST_PROXY")
	if raw == "" {
		t.Skip("未设置 BW_TEST_PROXY，跳过真实代理检测")
	}
	s, _ := newTestService(t)

	got := s.CheckProxy(context.Background(), parseProxyURL(t, raw))
	if !got.OK {
		t.Fatalf("真实代理检测失败: %s", got.Err)
	}
	if got.Exit == nil {
		t.Fatal("成功时未返回出口信息")
	}
	if got.Aligned == nil {
		t.Fatal("成功时未返回对齐后的时区语言")
	}
	// 出口国家码必须解析出来，否则时区无从推导。
	if got.Exit.Geo.CountryCode == "" {
		t.Error("未解析出出口国家码")
	}
	// 对齐结果必须自洽：有国家码就必须有时区和语言。
	if got.Aligned.Timezone == "" || got.Aligned.Locale == "" {
		t.Errorf("对齐结果不完整: %+v", got.Aligned)
	}
	if got.ElapsedMs <= 0 {
		t.Error("未记录耗时")
	}

	t.Logf("出口: country=%s org=%q kind=%s",
		got.Exit.Geo.CountryCode, got.Exit.Org, got.Exit.Kind)
	t.Logf("对齐: tz=%s locale=%s", got.Aligned.Timezone, got.Aligned.Locale)
	for _, w := range got.Warnings {
		t.Logf("警告: %s", w)
	}
}

// parseProxyURL 把 scheme://host:port 解析成 model.Proxy。
func parseProxyURL(t *testing.T, raw string) *model.Proxy {
	t.Helper()
	scheme := model.ProxySOCKS5
	rest := raw
	for _, s := range []model.ProxyScheme{model.ProxySOCKS5, model.ProxyHTTPS, model.ProxyHTTP} {
		prefix := string(s) + "://"
		if len(raw) > len(prefix) && raw[:len(prefix)] == prefix {
			scheme, rest = s, raw[len(prefix):]
			break
		}
	}
	host, portStr, err := net.SplitHostPort(rest)
	if err != nil {
		t.Fatalf("BW_TEST_PROXY=%q 格式不正确: %v", raw, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("端口非法: %v", err)
	}
	return &model.Proxy{Scheme: scheme, Host: host, Port: port}
}
