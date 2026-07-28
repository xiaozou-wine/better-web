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

// slowSOCKS5Upstream 是一个在握手前刻意延迟的 SOCKS5 上游，用于模拟真实
// 住宅/机房代理常见的高延迟：TCP 能连上，但 SOCKS 协商与 CONNECT 要等几秒。
//
// 存在意义：原有测试全部用零延迟的本地伪上游，握手在微秒级完成，因此
// handle 里「local deadline 早于上游拨号超时」这个缺陷从未被触发过。
type slowSOCKS5Upstream struct {
	ln    net.Listener
	delay time.Duration

	// handled 记录成功完成隧道建立的连接数，用于区分
	// 「上游没收到请求」与「上游做完了但本地已放弃」。
	handled atomic.Int32
}

func startSlowSOCKS5(t *testing.T, delay time.Duration) *slowSOCKS5Upstream {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("启动慢速伪上游失败: %v", err)
	}
	u := &slowSOCKS5Upstream{ln: ln, delay: delay}
	go u.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return u
}

func (u *slowSOCKS5Upstream) addrPort(t *testing.T) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(u.ln.Addr().String())
	if err != nil {
		t.Fatalf("解析慢速伪上游地址失败: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("解析慢速伪上游端口失败: %v", err)
	}
	return host, port
}

func (u *slowSOCKS5Upstream) serve() {
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

// handle 在方法协商之前等待 delay，把延迟压在握手阶段——
// 这正是真实代理最慢的一段。
func (u *slowSOCKS5Upstream) handle(conn net.Conn) error {
	time.Sleep(u.delay)

	head := make([]byte, 2)
	if _, err := io.ReadFull(conn, head); err != nil {
		return err
	}
	methods := make([]byte, int(head[1]))
	if _, err := io.ReadFull(conn, methods); err != nil {
		return err
	}
	// 声明免认证，把变量收敛到延迟这一项上。
	if _, err := conn.Write([]byte{socks5Version, authNoneRequired}); err != nil {
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
	u.handled.Add(1)
	tunnel(conn, remote)
	return nil
}

// 回归测试：上游握手耗时超过 handshakeTimeout 但仍在 dialTimeout 之内时，
// 转发器必须成功建立隧道。
//
// 缺陷所在：handle 对本地连接设了 handshakeTimeout(10s) 的绝对 deadline，
// 之后才去做一件最长允许 dialTimeout(20s) 的上游拨号。当上游握手耗时落在
// 这两个值之间，上游其实成功了，但写 SOCKS 应答给本地时 deadline 已过，
// 于是浏览器侧收到连接失败。表现为「代理明明能用，浏览器就是打不开」。
func TestForwarderSurvivesSlowUpstreamHandshake(t *testing.T) {
	if testing.Short() {
		t.Skip("依赖真实等待，-short 下跳过")
	}

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "slow-but-ok")
	}))
	defer origin.Close()

	// 12s 落在 handshakeTimeout(10s) 与 dialTimeout(20s) 之间：
	// 按设计意图这应当成功，因为拨号预算是 20s。
	const upstreamDelay = 12 * time.Second
	up := startSlowSOCKS5(t, upstreamDelay)
	host, port := up.addrPort(t)

	f, err := New(&model.Proxy{Scheme: model.ProxySOCKS5, Host: host, Port: port})
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}
	addr, err := f.Start()
	if err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	defer func() { _ = f.Close() }()

	// 客户端超时给足，确保失败归因于转发器而非客户端。
	client := socks5Client(t, strings.TrimPrefix(addr, "socks5://"))
	client.Timeout = 40 * time.Second

	resp, err := client.Get(origin.URL)
	if err != nil {
		t.Fatalf("上游握手耗时 %s（< dialTimeout %s）本应成功，实际失败: %v",
			upstreamDelay, dialTimeout, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}
	if string(body) != "slow-but-ok" {
		t.Errorf("响应体 = %q, 期望 slow-but-ok", body)
	}
}

// 佐证测试：证明上述失败发生在「上游已成功、本地已超时」这个组合上，
// 而不是上游压根没被连上。
func TestSlowUpstreamActuallyCompletesHandshake(t *testing.T) {
	if testing.Short() {
		t.Skip("依赖真实等待，-short 下跳过")
	}

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "direct-ok")
	}))
	defer origin.Close()

	const upstreamDelay = 12 * time.Second
	up := startSlowSOCKS5(t, upstreamDelay)
	host, port := up.addrPort(t)

	f, err := New(&model.Proxy{Scheme: model.ProxySOCKS5, Host: host, Port: port})
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}
	addr, err := f.Start()
	if err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	defer func() { _ = f.Close() }()

	client := socks5Client(t, strings.TrimPrefix(addr, "socks5://"))
	client.Timeout = 40 * time.Second
	resp, err := client.Get(origin.URL)
	if err == nil {
		_ = resp.Body.Close()
	}

	// 无论转发器是否把结果传回客户端，上游都应当完成了握手。
	if got := up.handled.Load(); got == 0 {
		t.Fatalf("上游未完成任何隧道建立，说明失败原因不是本地 deadline 而是连不上上游")
	}
}
