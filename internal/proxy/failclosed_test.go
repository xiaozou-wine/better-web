package proxy

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"better-web/internal/model"
)

// 上游代理不可用时，转发器必须让连接失败，绝不能退化成直连。
//
// 这是整个代理层最危险的失效模式：静默直连不会报错，浏览器照常打开页面，
// 但出口已是用户真实 IP，指纹伪装全部作废且用户毫无察觉。
// 因此这里断言的是"必须失败"，任何让它变得更"健壮"的兜底都属于回归。
func TestForwarderFailsClosedWhenUpstreamDown(t *testing.T) {
	// 起一个监听器随即关闭，得到一个确定无人监听的端口。
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("探测空闲端口失败: %v", err)
	}
	deadHost, deadPortStr, _ := net.SplitHostPort(probe.Addr().String())
	deadPort, _ := strconv.Atoi(deadPortStr)
	_ = probe.Close()

	// 目标站点记录是否被直接访问过。被访问即意味着流量绕过了代理。
	var directHits atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		directHits.Add(1)
		_, _ = io.WriteString(w, "leaked")
	}))
	defer origin.Close()

	f, err := New(&model.Proxy{
		Scheme: model.ProxySOCKS5, Host: deadHost, Port: deadPort,
		Username: "u", Password: "p",
	})
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}
	addr, err := f.Start()
	if err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	defer func() { _ = f.Close() }()

	client := socks5Client(t, strings.TrimPrefix(addr, "socks5://"))
	resp, err := client.Get(origin.URL)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("上游不可用时请求却成功了，说明发生了直连泄漏")
	}
	if n := directHits.Load(); n != 0 {
		t.Errorf("目标站点收到 %d 次直连请求，真实 IP 已泄漏", n)
	}
}

// 上游拨号失败时必须回标准 SOCKS5 失败码，而不是直接断开或回成功。
// 回成功会让浏览器以为隧道就绪，随后把明文请求写进一条通往空洞的连接。
func TestForwarderRepliesFailureCodeOnUpstreamError(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("探测空闲端口失败: %v", err)
	}
	deadHost, deadPortStr, _ := net.SplitHostPort(probe.Addr().String())
	deadPort, _ := strconv.Atoi(deadPortStr)
	_ = probe.Close()

	f, err := New(&model.Proxy{Scheme: model.ProxySOCKS5, Host: deadHost, Port: deadPort})
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}
	addr, err := f.Start()
	if err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	defer func() { _ = f.Close() }()

	conn, err := net.DialTimeout("tcp", strings.TrimPrefix(addr, "socks5://"), 5*time.Second)
	if err != nil {
		t.Fatalf("连接转发器失败: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if err := conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		t.Fatalf("设置超时失败: %v", err)
	}

	// 手写握手：声明支持免认证。
	if _, err := conn.Write([]byte{socks5Version, 0x01, authNoneRequired}); err != nil {
		t.Fatalf("发送握手失败: %v", err)
	}
	var greet [2]byte
	if _, err := io.ReadFull(conn, greet[:]); err != nil {
		t.Fatalf("读取握手响应失败: %v", err)
	}
	if greet[0] != socks5Version || greet[1] != authNoneRequired {
		t.Fatalf("握手响应 = %v, 期望 [5 0]", greet)
	}

	// 请求连接一个域名，转发器会尝试拨号已死的上游。
	const domain = "example.test"
	req := []byte{socks5Version, cmdConnect, 0x00, addrTypeDomain, byte(len(domain))}
	req = append(req, domain...)
	req = append(req, 0x01, 0xBB) // 443
	if _, err := conn.Write(req); err != nil {
		t.Fatalf("发送 CONNECT 失败: %v", err)
	}

	reply := make([]byte, 10)
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatalf("读取 CONNECT 响应失败（转发器直接断开而未回失败码）: %v", err)
	}
	if reply[0] != socks5Version {
		t.Errorf("响应版本 = %d, 期望 5", reply[0])
	}
	if reply[1] == replySucceeded {
		t.Error("上游不可用时却回了成功码，浏览器会向一条无效隧道发送明文请求")
	}
}

// 上游要求认证但凭据不被接受时，同样必须失败而非回退直连。
// 免费代理额度耗尽、账号过期都会表现为认证失败，这条路径比"端口不通"更常见。
func TestForwarderFailsClosedOnUpstreamAuthRejection(t *testing.T) {
	var directHits atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		directHits.Add(1)
		_, _ = io.WriteString(w, "leaked")
	}))
	defer origin.Close()

	// 上游只接受 right/pass，转发器持有的是错误凭据。
	up := startFakeSOCKS5(t, "right", "pass")
	host, port := up.addrPort(t)

	f, err := New(&model.Proxy{
		Scheme: model.ProxySOCKS5, Host: host, Port: port,
		Username: "wrong", Password: "creds",
	})
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}
	addr, err := f.Start()
	if err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	defer func() { _ = f.Close() }()

	client := socks5Client(t, strings.TrimPrefix(addr, "socks5://"))
	resp, err := client.Get(origin.URL)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("上游认证失败时请求却成功了，说明发生了直连泄漏")
	}
	if n := directHits.Load(); n != 0 {
		t.Errorf("目标站点收到 %d 次直连请求，真实 IP 已泄漏", n)
	}
}

// 会话运行中上游断开时，后续请求必须失败。
// 免费代理随时会掉，这条路径决定了掉线瞬间是"页面打不开"还是"真实 IP 出网"。
func TestForwarderFailsClosedAfterUpstreamDropsMidSession(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer origin.Close()

	up := startFakeSOCKS5(t, "u", "p")
	host, port := up.addrPort(t)

	f, err := New(&model.Proxy{
		Scheme: model.ProxySOCKS5, Host: host, Port: port,
		Username: "u", Password: "p",
	})
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}
	addr, err := f.Start()
	if err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	defer func() { _ = f.Close() }()

	client := socks5Client(t, strings.TrimPrefix(addr, "socks5://"))

	// 先确认正常状态下能通，排除测试本身的配置错误。
	resp, err := client.Get(origin.URL)
	if err != nil {
		t.Fatalf("上游正常时请求就失败了: %v", err)
	}
	_ = resp.Body.Close()

	// 模拟代理掉线。
	if err := up.ln.Close(); err != nil {
		t.Fatalf("关闭伪上游失败: %v", err)
	}

	resp2, err := client.Get(origin.URL)
	if err == nil {
		_ = resp2.Body.Close()
		t.Error("上游已断开但请求仍成功，说明退化成了直连")
	}
}
