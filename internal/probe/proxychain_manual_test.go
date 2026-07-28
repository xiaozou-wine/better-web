package probe

import (
	"context"
	"net"
	"os"
	"strconv"
	"testing"

	"better-web/internal/fingerprint"
	"better-web/internal/geo"
	"better-web/internal/launcher"
	"better-web/internal/model"
	"better-web/internal/proxy"
)

// 走完整真实链路：真实代理 → 出口 IP 反查 → 时区语言自动对齐 → 真实内核。
//
// 默认跳过：需要一个可用的上游代理。用 BW_TEST_PROXY 指定，例如：
//
//	BW_TEST_PROXY=socks5://127.0.0.1:10808 go test -run TestRealProxyChain -v ./internal/probe/
//
// 这是整个项目最关键的一致性断言：浏览器报出的时区必须与代理出口地一致。
// 二者不符等同于自报身份，前面所有指纹伪造都会失效。
func TestRealProxyChain(t *testing.T) {
	up := parseTestProxy(t)
	k := realKernel(t)

	fwd, err := proxy.New(up)
	if err != nil {
		t.Fatalf("创建转发器失败: %v", err)
	}
	addr, err := fwd.Start()
	if err != nil {
		t.Fatalf("启动转发器失败: %v", err)
	}
	defer func() { _ = fwd.Close() }()

	// 出口地必须经代理查，直连查到的是本机地址。
	client, err := fwd.HTTPClient()
	if err != nil {
		t.Fatalf("构造代理客户端失败: %v", err)
	}
	resolved, err := geo.NewResolver(client).Lookup(context.Background())
	if err != nil {
		t.Fatalf("查询代理出口地失败: %v", err)
	}
	t.Logf("代理出口地: country=%s tz=%s locale=%s",
		resolved.CountryCode, resolved.Timezone, resolved.Locale)

	fp := fingerprint.Derive(20260727, &resolved)
	p := &model.Profile{
		ID: "chain", Name: "chain", Kind: model.KindFingerprint,
		Seed: fp.Seed, ProfileDir: t.TempDir(),
	}
	args, err := launcher.BuildArgs(p, &fp, addr, nil)
	if err != nil {
		t.Fatalf("BuildArgs 失败: %v", err)
	}

	res, err := (&Probe{ExecPath: k.ExecPath}).Collect(context.Background(), args)
	if err != nil {
		t.Fatalf("采集失败: %v", err)
	}

	// 核心断言：浏览器报的时区与代理出口地推导出的时区一致。
	if res.Timezone != resolved.Timezone {
		t.Errorf("浏览器时区 %q 与代理出口地时区 %q 不一致，构成可被检测的矛盾",
			res.Timezone, resolved.Timezone)
	}
	if res.Language != resolved.Locale {
		t.Errorf("浏览器语言 %q 与出口地语言 %q 不一致", res.Language, resolved.Locale)
	}
	// 配了代理就必须带上 WebRTC 泄露防护。
	if !hasFlag(args, "--disable-non-proxied-udp") {
		t.Error("配了代理却未关闭非代理 UDP，WebRTC 会泄露真实 IP")
	}
	if res.Webdriver {
		t.Error("navigator.webdriver 为 true")
	}

	t.Logf("链路一致: tz=%q lang=%q UA=%q", res.Timezone, res.Language, res.UserAgent)
}

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

// parseTestProxy 解析 BW_TEST_PROXY，未设置时跳过测试。
func parseTestProxy(t *testing.T) *model.Proxy {
	t.Helper()
	raw := os.Getenv("BW_TEST_PROXY")
	if raw == "" {
		t.Skip("未设置 BW_TEST_PROXY，跳过真实代理链路测试")
	}

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
		t.Fatalf("BW_TEST_PROXY 端口非法: %v", err)
	}
	return &model.Proxy{Scheme: scheme, Host: host, Port: port}
}
