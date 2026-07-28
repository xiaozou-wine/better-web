package proxy

import (
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"better-web/internal/model"
)

// taggedUpstream 是一个带身份标记的伪上游 SOCKS5 代理。
// 它在隧道建立后先写入自己的标记，使得客户端能确认流量究竟从哪个上游出网——
// 这正是多 profile 隔离需要断言的事实。
type taggedUpstream struct {
	tag string
	ln  net.Listener

	mu    sync.Mutex
	users []string // 记录每条连接出示的用户名，用于检测凭据串用
}

func startTaggedUpstream(t *testing.T, tag, wantUser, wantPass string) *taggedUpstream {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("启动带标记上游失败: %v", err)
	}
	u := &taggedUpstream{tag: tag, ln: ln}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				_ = u.handle(conn, wantUser, wantPass)
			}()
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return u
}

func (u *taggedUpstream) addrPort(t *testing.T) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(u.ln.Addr().String())
	if err != nil {
		t.Fatalf("解析上游地址失败: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("解析上游端口失败: %v", err)
	}
	return host, port
}

func (u *taggedUpstream) seenUsers() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.users...)
}

// handle 完成 SOCKS5 + 用户名密码认证，随后直接把自己的标记写回客户端。
// 不真正连接目标：本测试关心的是流量走了哪个上游，而非目标响应内容。
func (u *taggedUpstream) handle(conn net.Conn, wantUser, wantPass string) error {
	head := make([]byte, 2)
	if _, err := io.ReadFull(conn, head); err != nil {
		return err
	}
	methods := make([]byte, int(head[1]))
	if _, err := io.ReadFull(conn, methods); err != nil {
		return err
	}
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

	u.mu.Lock()
	u.users = append(u.users, string(user))
	u.mu.Unlock()

	if string(user) != wantUser || string(pass) != wantPass {
		_, _ = conn.Write([]byte{0x01, 0x01})
		return nil
	}
	if _, err := conn.Write([]byte{0x01, 0x00}); err != nil {
		return err
	}
	if _, err := readConnectRequest(conn); err != nil {
		return err
	}
	if err := writeReply(conn, replySucceeded); err != nil {
		return err
	}

	// 必须先读完客户端的 HTTP 请求再回响应并关闭。
	// 带未读数据的 socket 执行 Close 会发出 RST，把已写出的响应截断，
	// 表现为客户端随机收到 EOF。
	if err := drainHTTPRequest(conn); err != nil {
		return err
	}

	// 隧道就绪，回一个最小 HTTP 响应，body 即本上游的身份标记。
	if _, err := conn.Write([]byte(
		"HTTP/1.1 200 OK\r\nContent-Length: " + strconv.Itoa(len(u.tag)) +
			"\r\nConnection: close\r\n\r\n" + u.tag)); err != nil {
		return err
	}
	// 半关闭写端，让客户端读到完整响应后再收到 EOF。
	if cw, ok := conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return nil
}

// drainHTTPRequest 读到 HTTP 头结束（CRLFCRLF）为止。
// 测试中的请求均为无 body 的 GET，读完头部即读完整个请求。
func drainHTTPRequest(conn net.Conn) error {
	var seen []byte
	buf := make([]byte, 1)
	for {
		if _, err := io.ReadFull(conn, buf); err != nil {
			return err
		}
		seen = append(seen, buf[0])
		if len(seen) >= 4 && string(seen[len(seen)-4:]) == "\r\n\r\n" {
			return nil
		}
		if len(seen) > 8192 {
			return io.ErrUnexpectedEOF
		}
	}
}

// 每个 profile 独占一个转发器，多实例并发运行时出口不得串流。
//
// 这是多账号场景的底线：两个 profile 的流量一旦经同一出口出网，
// 平台侧立刻能把两个账号关联起来，profile 隔离形同虚设。
func TestConcurrentForwardersDoNotCrossTalk(t *testing.T) {
	upA := startTaggedUpstream(t, "upstream-A", "userA", "passA")
	upB := startTaggedUpstream(t, "upstream-B", "userB", "passB")

	hostA, portA := upA.addrPort(t)
	hostB, portB := upB.addrPort(t)

	fA, err := New(&model.Proxy{
		Scheme: model.ProxySOCKS5, Host: hostA, Port: portA,
		Username: "userA", Password: "passA",
	})
	if err != nil {
		t.Fatalf("创建转发器 A 失败: %v", err)
	}
	fB, err := New(&model.Proxy{
		Scheme: model.ProxySOCKS5, Host: hostB, Port: portB,
		Username: "userB", Password: "passB",
	})
	if err != nil {
		t.Fatalf("创建转发器 B 失败: %v", err)
	}

	addrA, err := fA.Start()
	if err != nil {
		t.Fatalf("启动转发器 A 失败: %v", err)
	}
	defer func() { _ = fA.Close() }()
	addrB, err := fB.Start()
	if err != nil {
		t.Fatalf("启动转发器 B 失败: %v", err)
	}
	defer func() { _ = fB.Close() }()

	// 两个转发器必须占用不同端口，否则根本谈不上隔离。
	if addrA == addrB {
		t.Fatalf("两个转发器监听同一地址: %s", addrA)
	}

	// 并发施压，放大共享状态导致串流的概率。
	const rounds = 12
	type result struct {
		which string
		body  string
		err   error
	}
	results := make(chan result, rounds*2)
	var wg sync.WaitGroup
	for i := 0; i < rounds; i++ {
		for _, c := range []struct {
			which string
			addr  string
		}{{"A", addrA}, {"B", addrB}} {
			wg.Add(1)
			go func(which, addr string) {
				defer wg.Done()
				body, err := fetchViaSOCKS5(strings.TrimPrefix(addr, "socks5://"))
				results <- result{which: which, body: body, err: err}
			}(c.which, c.addr)
		}
	}
	wg.Wait()
	close(results)

	for r := range results {
		if r.err != nil {
			t.Errorf("经转发器 %s 请求失败: %v", r.which, r.err)
			continue
		}
		want := "upstream-" + r.which
		if r.body != want {
			t.Errorf("经转发器 %s 的流量从 %q 出网，期望 %q：出口串流", r.which, r.body, want)
		}
	}

	// 凭据同样不能串：A 的用户名绝不应出现在 B 的上游上。
	for _, u := range upA.seenUsers() {
		if u != "userA" {
			t.Errorf("上游 A 收到了非本 profile 的用户名，凭据发生串用")
		}
	}
	for _, u := range upB.seenUsers() {
		if u != "userB" {
			t.Errorf("上游 B 收到了非本 profile 的用户名，凭据发生串用")
		}
	}
}

// 关闭一个 profile 的转发器不得影响另一个正在运行的 profile。
// 用户关掉一个窗口时，其余窗口必须照常工作。
func TestClosingOneForwarderLeavesOthersRunning(t *testing.T) {
	upA := startTaggedUpstream(t, "upstream-A", "userA", "passA")
	upB := startTaggedUpstream(t, "upstream-B", "userB", "passB")
	hostA, portA := upA.addrPort(t)
	hostB, portB := upB.addrPort(t)

	fA, _ := New(&model.Proxy{
		Scheme: model.ProxySOCKS5, Host: hostA, Port: portA,
		Username: "userA", Password: "passA",
	})
	fB, _ := New(&model.Proxy{
		Scheme: model.ProxySOCKS5, Host: hostB, Port: portB,
		Username: "userB", Password: "passB",
	})
	addrA, err := fA.Start()
	if err != nil {
		t.Fatalf("启动转发器 A 失败: %v", err)
	}
	addrB, err := fB.Start()
	if err != nil {
		t.Fatalf("启动转发器 B 失败: %v", err)
	}
	defer func() { _ = fB.Close() }()

	if _, err := fetchViaSOCKS5(strings.TrimPrefix(addrA, "socks5://")); err != nil {
		t.Fatalf("关闭前经 A 请求失败: %v", err)
	}

	if err := fA.Close(); err != nil {
		t.Fatalf("关闭转发器 A 失败: %v", err)
	}

	// A 已关闭，应连不上。
	if _, err := fetchViaSOCKS5(strings.TrimPrefix(addrA, "socks5://")); err == nil {
		t.Error("转发器 A 已关闭但仍可用")
	}

	// B 不受影响。
	body, err := fetchViaSOCKS5(strings.TrimPrefix(addrB, "socks5://"))
	if err != nil {
		t.Fatalf("关闭 A 后经 B 请求失败: %v", err)
	}
	if body != "upstream-B" {
		t.Errorf("经 B 的流量从 %q 出网，期望 upstream-B", body)
	}
}

// fetchViaSOCKS5 经指定 SOCKS5 地址发一次 HTTP 请求，返回响应体。
// 目标地址用域名形式，确保走的是"域名交上游解析"这条路径。
func fetchViaSOCKS5(socksAddr string) (string, error) {
	d, err := newSOCKS5TestDialer(socksAddr)
	if err != nil {
		return "", err
	}
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			DialContext:       d.DialContext,
			DisableKeepAlives: true,
		},
	}
	resp, err := client.Get("http://profile-isolation.test/")
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}
