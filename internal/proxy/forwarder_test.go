package proxy

import (
	"bufio"
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"better-web/internal/model"
)

// fakeSOCKS5Upstream 是一个最小的 SOCKS5 上游，强制要求用户名/密码认证，
// 用于验证转发器确实补上了凭据。
type fakeSOCKS5Upstream struct {
	ln       net.Listener
	wantUser string
	wantPass string

	gotAuth chan [2]string
}

func startFakeSOCKS5(t *testing.T, user, pass string) *fakeSOCKS5Upstream {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("启动伪上游失败: %v", err)
	}
	u := &fakeSOCKS5Upstream{ln: ln, wantUser: user, wantPass: pass, gotAuth: make(chan [2]string, 4)}
	go u.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return u
}

func (u *fakeSOCKS5Upstream) addrPort(t *testing.T) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(u.ln.Addr().String())
	if err != nil {
		t.Fatalf("解析伪上游地址失败: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("解析伪上游端口失败: %v", err)
	}
	return host, port
}

func (u *fakeSOCKS5Upstream) serve() {
	for {
		conn, err := u.ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer func() { _ = conn.Close() }()
			_ = u.handle(conn)
		}()
	}
}

// handle 实现 RFC 1928 的服务端 + RFC 1929 的用户名密码认证。
func (u *fakeSOCKS5Upstream) handle(conn net.Conn) error {
	head := make([]byte, 2)
	if _, err := io.ReadFull(conn, head); err != nil {
		return err
	}
	methods := make([]byte, int(head[1]))
	if _, err := io.ReadFull(conn, methods); err != nil {
		return err
	}
	// 只接受用户名密码认证（0x02），逼迫转发器出示凭据。
	if _, err := conn.Write([]byte{socks5Version, 0x02}); err != nil {
		return err
	}

	var vh [2]byte
	if _, err := io.ReadFull(conn, vh[:]); err != nil {
		return err
	}
	user := make([]byte, int(vh[1]))
	if _, err := io.ReadFull(conn, user); err != nil {
		return err
	}
	var pl [1]byte
	if _, err := io.ReadFull(conn, pl[:]); err != nil {
		return err
	}
	pass := make([]byte, int(pl[0]))
	if _, err := io.ReadFull(conn, pass); err != nil {
		return err
	}
	u.gotAuth <- [2]string{string(user), string(pass)}

	if string(user) != u.wantUser || string(pass) != u.wantPass {
		_, _ = conn.Write([]byte{0x01, 0x01})
		return nil
	}
	if _, err := conn.Write([]byte{0x01, 0x00}); err != nil {
		return err
	}

	target, err := readConnectRequest(conn)
	if err != nil {
		return err
	}
	remote, err := net.DialTimeout("tcp", target, 5*time.Second)
	if err != nil {
		_ = writeReply(conn, replyHostUnreachable)
		return err
	}
	defer func() { _ = remote.Close() }()
	if err := writeReply(conn, replySucceeded); err != nil {
		return err
	}
	tunnel(conn, remote)
	return nil
}

// 端到端：浏览器视角连本地无认证 SOCKS5，流量应经带认证的上游到达目标站点。
func TestForwarderTunnelsThroughAuthenticatedUpstream(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok-through-tunnel")
	}))
	defer origin.Close()

	up := startFakeSOCKS5(t, "u1", "p1")
	host, port := up.addrPort(t)

	f, err := New(&model.Proxy{
		Scheme: model.ProxySOCKS5, Host: host, Port: port,
		Username: "u1", Password: "p1",
	})
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}
	addr, err := f.Start()
	if err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	defer func() { _ = f.Close() }()

	// 返回给 Chromium 的地址必须是可直接用于 --proxy-server 的回环地址。
	if !strings.HasPrefix(addr, "socks5://127.0.0.1:") {
		t.Fatalf("监听地址 = %q, 期望 socks5://127.0.0.1:<port>", addr)
	}

	client := socks5Client(t, strings.TrimPrefix(addr, "socks5://"))
	resp, err := client.Get(origin.URL)
	if err != nil {
		t.Fatalf("经转发器请求失败: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}
	if string(body) != "ok-through-tunnel" {
		t.Errorf("响应体 = %q", body)
	}

	// 转发器必须替客户端补上上游凭据，客户端自己从不知道密码。
	select {
	case got := <-up.gotAuth:
		if got[0] != "u1" || got[1] != "p1" {
			t.Errorf("上游收到的凭据 = %q/%q, 期望 u1/p1", got[0], "***")
		}
	case <-time.After(5 * time.Second):
		t.Error("上游未收到认证信息")
	}
}

// 转发器只能绑回环地址：它不带认证，绑到 0.0.0.0 等于把代理白送给同网段。
func TestForwarderBindsLoopbackOnly(t *testing.T) {
	up := startFakeSOCKS5(t, "u", "p")
	host, port := up.addrPort(t)
	f, err := New(&model.Proxy{Scheme: model.ProxySOCKS5, Host: host, Port: port, Username: "u", Password: "p"})
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}
	addr, err := f.Start()
	if err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	defer func() { _ = f.Close() }()

	hostPart, _, err := net.SplitHostPort(strings.TrimPrefix(addr, "socks5://"))
	if err != nil {
		t.Fatalf("解析监听地址失败: %v", err)
	}
	ip := net.ParseIP(hostPart)
	if ip == nil || !ip.IsLoopback() {
		t.Errorf("监听地址 %q 不是回环地址", hostPart)
	}
}

// 重复 Start 应返回同一地址，不能泄漏出第二个监听器。
func TestForwarderStartIsIdempotent(t *testing.T) {
	up := startFakeSOCKS5(t, "u", "p")
	host, port := up.addrPort(t)
	f, _ := New(&model.Proxy{Scheme: model.ProxySOCKS5, Host: host, Port: port})
	a1, err := f.Start()
	if err != nil {
		t.Fatalf("首次 Start 失败: %v", err)
	}
	a2, err := f.Start()
	if err != nil {
		t.Fatalf("二次 Start 失败: %v", err)
	}
	if a1 != a2 {
		t.Errorf("两次 Start 返回不同地址: %q vs %q", a1, a2)
	}
	if err := f.Close(); err != nil {
		t.Errorf("Close 失败: %v", err)
	}
	// 关闭后不应再能启动。
	if _, err := f.Start(); err == nil {
		t.Error("关闭后 Start 仍成功")
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	up := startFakeSOCKS5(t, "u", "p")
	host, port := up.addrPort(t)
	f, _ := New(&model.Proxy{Scheme: model.ProxySOCKS5, Host: host, Port: port})
	if _, err := f.Start(); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := f.Close(); err != nil {
			t.Errorf("第 %d 次 Close 报错: %v", i+1, err)
		}
	}
}

func TestNewRejectsInvalidUpstream(t *testing.T) {
	cases := map[string]*model.Proxy{
		"nil":    nil,
		"缺 host": {Scheme: model.ProxySOCKS5, Port: 1080},
		"端口为 0":  {Scheme: model.ProxySOCKS5, Host: "127.0.0.1", Port: 0},
		"端口越界":   {Scheme: model.ProxySOCKS5, Host: "127.0.0.1", Port: 70000},
		"协议不支持":  {Scheme: "ftp", Host: "127.0.0.1", Port: 1080},
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := New(p); err == nil {
				t.Error("期望报错，实际通过")
			}
		})
	}
}

// 域名必须原样交给上游解析，本地解析会让 DNS 请求绕过代理泄露访问记录。
func TestConnectRequestPassesDomainVerbatim(t *testing.T) {
	client, server := net.Pipe()
	defer func() { _ = client.Close() }()
	defer func() { _ = server.Close() }()

	const domain = "example.internal"
	go func() {
		req := []byte{socks5Version, cmdConnect, 0x00, addrTypeDomain, byte(len(domain))}
		req = append(req, domain...)
		req = binary.BigEndian.AppendUint16(req, 8443)
		_, _ = client.Write(req)
	}()

	got, err := readConnectRequest(server)
	if err != nil {
		t.Fatalf("readConnectRequest 失败: %v", err)
	}
	if got != domain+":8443" {
		t.Errorf("目标地址 = %q, 期望 %s:8443", got, domain)
	}
}

func TestNegotiateAuthRejectsNonSOCKS5(t *testing.T) {
	client, server := net.Pipe()
	defer func() { _ = client.Close() }()
	defer func() { _ = server.Close() }()
	go func() { _, _ = client.Write([]byte{0x04, 0x01, 0x00}) }()
	if err := negotiateAuth(server); err == nil {
		t.Error("对 SOCKS4 握手期望报错，实际通过")
	}
}

// HTTP CONNECT 上游路径：验证凭据以 Proxy-Authorization 头送出且隧道可用。
func TestHTTPConnectDialerAuthenticatesAndTunnels(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "http-proxy-ok")
	}))
	defer origin.Close()

	gotAuthHeader := make(chan string, 1)
	proxyLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("启动伪 HTTP 代理失败: %v", err)
	}
	defer func() { _ = proxyLn.Close() }()

	go func() {
		conn, err := proxyLn.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		br := bufio.NewReader(conn)
		req, err := http.ReadRequest(br)
		if err != nil {
			return
		}
		gotAuthHeader <- req.Header.Get("Proxy-Authorization")
		if req.Method != http.MethodConnect {
			_, _ = conn.Write([]byte("HTTP/1.1 405 Method Not Allowed\r\n\r\n"))
			return
		}
		remote, err := net.DialTimeout("tcp", req.Host, 5*time.Second)
		if err != nil {
			_, _ = conn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
			return
		}
		defer func() { _ = remote.Close() }()
		if _, err := conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
			return
		}
		tunnel(conn, remote)
	}()

	host, portStr, _ := net.SplitHostPort(proxyLn.Addr().String())
	port, _ := strconv.Atoi(portStr)
	f, err := New(&model.Proxy{
		Scheme: model.ProxyHTTP, Host: host, Port: port,
		Username: "hu", Password: "hp",
	})
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}
	client, err := f.HTTPClient()
	if err != nil {
		t.Fatalf("HTTPClient 失败: %v", err)
	}
	resp, err := client.Get(origin.URL)
	if err != nil {
		t.Fatalf("经 HTTP 代理请求失败: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "http-proxy-ok" {
		t.Errorf("响应体 = %q", body)
	}

	select {
	case h := <-gotAuthHeader:
		// 只断言认证头存在且是 Basic，不回显凭据内容。
		if !strings.HasPrefix(h, "Basic ") {
			t.Errorf("Proxy-Authorization 头缺失或格式不符")
		}
	case <-time.After(5 * time.Second):
		t.Error("伪 HTTP 代理未收到请求")
	}
}

// socks5Client 构造一个经指定 SOCKS5 地址出网的 HTTP 客户端。
func socks5Client(t *testing.T, socksAddr string) *http.Client {
	t.Helper()
	d, err := newSOCKS5TestDialer(socksAddr)
	if err != nil {
		t.Fatalf("构造测试用 SOCKS5 客户端失败: %v", err)
	}
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return d.DialContext(ctx, network, addr)
			},
			DisableKeepAlives: true,
		},
	}
}
