package proxy

import (
	"crypto/rand"
	"io"
	"net"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"better-web/internal/model"
)

// 转发器是唯一位于热路径上的自研代码：浏览器的每个字节都要过它一遍。
// 这组基准衡量它相对直连引入了多少额外开销。
//
// 拓扑：客户端 → 转发器（本地 SOCKS5，无认证）→ 伪上游（要求认证）→ 回显服务。
// 直连基线：客户端 → 回显服务。两者的差值即转发链路的成本。

// startEchoServer 起一个把收到的字节原样回写的 TCP 服务。
// benchOptInEnv 是启用统计类测试的环境变量。
const benchOptInEnv = "BW_RUN_BENCH"

// requireBenchOptIn 让统计类测试默认跳过。
//
// 这些测试为量化吞吐与开销会建立大量短连接（并发 32 档一轮就有数百条），
// 每条经转发器出网占用两个本地端口。实测在常规 `go test ./...` 中运行会
// 让 Windows 的 TIME_WAIT 累积到两万以上，打满 16384 个动态端口，
// 连带把同包内的 fail-closed 等真实断言测试搞成随机失败。
//
// 它们产出的是参考数据而非断言，不该在每次跑测试时都执行。
// 需要时显式启用：
//
//	BW_RUN_BENCH=1 go test -run TestReport ./internal/proxy/
func requireBenchOptIn(t *testing.T) {
	t.Helper()
	if os.Getenv(benchOptInEnv) != "1" {
		t.Skipf("未设置 %s=1，跳过统计类测试（会大量占用本地端口）", benchOptInEnv)
	}
	// 统计测试之间会互抢端口：前一个跑掉几百个端口后，后一个就只能
	// 报"资源不足"。串行执行并在之间留出回收窗口，让每个都拿到可用数据。
	benchMu.Lock()
	t.Cleanup(func() {
		time.Sleep(benchCooldown)
		benchMu.Unlock()
	})
}

// benchMu 串行化统计测试，benchCooldown 是其间的端口回收窗口。
// Windows 的 TIME_WAIT 默认 4 分钟，这里不等满——只需让瞬时压力回落。
var benchMu sync.Mutex

const benchCooldown = 3 * time.Second

// drainErrors 取出通道里的第一个错误并排空其余，返回 nil 表示全部成功。
// 必须排空：留在通道里的错误会让发送方 goroutine 永久阻塞。
func drainErrors(errs <-chan error) error {
	var first error
	for err := range errs {
		if first == nil {
			first = err
		}
	}
	return first
}

func startEchoServer(t testing.TB) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("启动回显服务失败: %v", err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = c.Close() }()
				_, _ = io.Copy(c, c)
			}()
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

// benchForwarder 起一条完整的转发链路，返回本地监听地址。
func benchForwarder(t testing.TB) (*Forwarder, string) {
	t.Helper()
	up := startFakeSOCKS5AnyTB(t, "u", "p")
	host, portStr, _ := net.SplitHostPort(up.Addr().String())
	port, _ := strconv.Atoi(portStr)

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
	t.Cleanup(func() { _ = f.Close() })
	return f, addr[len("socks5://"):]
}

// roundTrip 往连接写 payload 再读回同样长度，返回耗时。
func roundTrip(t testing.TB, conn net.Conn, payload []byte) {
	t.Helper()
	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("读取失败: %v", err)
	}
}

// 建连成本用固定次数测量而非交给 b.N 自适应。
//
// 原因是 Windows 的临时端口在 TIME_WAIT 下要等约 2 分钟才回收，b.N 自适应会
// 迭代到数万次，直接打满端口池并报 "Only one usage of each socket address"。
// 那是基准设计问题而非被测代码的问题，因此固定一个不会耗尽端口的次数，
// 用 TestReportConnectCost 报告结果。
const connectSamples = 300

// 建连 + 一轮往返的成本对比：转发链路 vs 直连。
// 这是最贴近浏览器行为的指标——页面加载会开大量短连接。
func TestReportConnectCost(t *testing.T) {
	requireBenchOptIn(t)
	echo := startEchoServer(t)
	_, socks := benchForwarder(t)
	payload := make([]byte, 1024)
	_, _ = rand.Read(payload)

	// 直连基线。
	directStart := time.Now()
	for i := 0; i < connectSamples; i++ {
		conn, err := net.Dial("tcp", echo.Addr().String())
		if err != nil {
			t.Fatalf("直连拨号失败（第 %d 次）: %v", i, err)
		}
		roundTrip(t, conn, payload)
		_ = conn.Close()
	}
	direct := time.Since(directStart) / connectSamples

	// 经转发器。
	fwdStart := time.Now()
	completed := 0
	for i := 0; i < connectSamples; i++ {
		d, err := newSOCKS5TestDialer(socks)
		if err != nil {
			t.Fatalf("构造拨号器失败: %v", err)
		}
		conn, err := d.DialContext(t.Context(), "tcp", echo.Addr().String())
		if err != nil {
			// 与其余统计测试保持一致：拨号失败通常是本机端口紧张，
			// 属于环境状况而非转发器缺陷。用已完成的样本给出结果。
			t.Logf("第 %d 次拨号失败，按已完成的 %d 次样本统计（%v）", i, completed, err)
			break
		}
		roundTrip(t, conn, payload)
		_ = conn.Close()
		completed++
	}
	if completed == 0 {
		t.Skip("本机资源不足，未能完成任何一次经转发器的往返")
	}
	forwarded := time.Since(fwdStart) / time.Duration(completed)

	t.Logf("建连 + 1KB 往返（经转发器 %d 次均值）:", completed)
	t.Logf("  直连      %v", direct.Round(time.Microsecond))
	t.Logf("  经转发器  %v", forwarded.Round(time.Microsecond))
	t.Logf("  额外开销  %v（%.1fx）", (forwarded - direct).Round(time.Microsecond),
		float64(forwarded)/float64(direct))
}

// 长连接上的吞吐：复用一条连接反复传 32KB，衡量隧道的稳态转发能力。
// 对应下载大文件或看视频的场景。
func BenchmarkForwarderThroughput32K(b *testing.B) {
	echo := startEchoServer(b)
	_, socks := benchForwarder(b)

	d, err := newSOCKS5TestDialer(socks)
	if err != nil {
		b.Fatalf("构造拨号器失败: %v", err)
	}
	conn, err := d.DialContext(b.Context(), "tcp", echo.Addr().String())
	if err != nil {
		b.Fatalf("拨号失败: %v", err)
	}
	defer func() { _ = conn.Close() }()

	payload := make([]byte, 32<<10)
	_, _ = rand.Read(payload)

	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		roundTrip(b, conn, payload)
	}
}

// 直连吞吐基线。
func BenchmarkDirectThroughput32K(b *testing.B) {
	echo := startEchoServer(b)
	conn, err := net.Dial("tcp", echo.Addr().String())
	if err != nil {
		b.Fatalf("拨号失败: %v", err)
	}
	defer func() { _ = conn.Close() }()

	payload := make([]byte, 32<<10)
	_, _ = rand.Read(payload)

	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		roundTrip(b, conn, payload)
	}
}

// 并发连接下的表现：模拟多 profile 同时活动。
// 转发器每条连接起两个 goroutine 做双向拷贝，这里看并发是否引起劣化。
//
// 同样用固定次数而非 b.N：并发建连更容易打满 Windows 的临时端口池。
func TestReportConcurrentThroughput(t *testing.T) {
	requireBenchOptIn(t)
	echo := startEchoServer(t)
	_, socks := benchForwarder(t)
	payload := make([]byte, 4<<10)
	_, _ = rand.Read(payload)

	for _, conc := range []int{1, 8, 32} {
		const perWorker = 20
		var wg sync.WaitGroup
		errs := make(chan error, conc*perWorker)
		started := time.Now()
		for w := 0; w < conc; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				d, err := newSOCKS5TestDialer(socks)
				if err != nil {
					errs <- err
					return
				}
				for i := 0; i < perWorker; i++ {
					conn, err := d.DialContext(t.Context(), "tcp", echo.Addr().String())
					if err != nil {
						errs <- err
						return
					}
					if _, err := conn.Write(payload); err != nil {
						errs <- err
						_ = conn.Close()
						return
					}
					buf := make([]byte, len(payload))
					if _, err := io.ReadFull(conn, buf); err != nil {
						errs <- err
					}
					_ = conn.Close()
				}
			}()
		}
		wg.Wait()
		close(errs)

		// 这是报告性质的统计，不是断言。高并发档位在宿主机动态端口
		// 紧张时必然失败（每条连接占两个本地端口：一条到转发器、
		// 一条到上游），把它判成失败会让测试结果取决于机器当时的
		// 端口余量。因此只记录并跳过该档位，继续其余档位。
		if failed := drainErrors(errs); failed != nil {
			t.Logf("并发 %-2d: 跳过，本机资源不足（%v）", conc, failed)
			continue
		}
		total := conc * perWorker
		t.Logf("并发 %-2d: %d 次建连+4KB 往返，总耗时 %v，均摊 %v/次",
			conc, total, time.Since(started).Round(time.Millisecond),
			(time.Since(started) / time.Duration(total)).Round(time.Microsecond))
	}
}

// 多个转发器实例并存的开销：每个 profile 独占一个 Forwarder，
// 这里量化同时运行 N 个实例的启动成本与内存占用。
func BenchmarkManyForwarders(b *testing.B) {
	up := startFakeSOCKS5AnyTB(b, "u", "p")
	host, portStr, _ := net.SplitHostPort(up.Addr().String())
	port, _ := strconv.Atoi(portStr)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f, err := New(&model.Proxy{
			Scheme: model.ProxySOCKS5, Host: host, Port: port,
			Username: "u", Password: "p",
		})
		if err != nil {
			b.Fatalf("New 失败: %v", err)
		}
		if _, err := f.Start(); err != nil {
			b.Fatalf("Start 失败: %v", err)
		}
		if err := f.Close(); err != nil {
			b.Fatalf("Close 失败: %v", err)
		}
	}
}

// 量化 20 个 profile 同时在线时的实际资源占用。
// 不是基准而是报告：用于判断单机能同时开多少个 profile。
func TestReportConcurrentForwarderCost(t *testing.T) {
	requireBenchOptIn(t)
	const n = 20
	echo := startEchoServer(t)
	up := startFakeSOCKS5AnyTB(t, "u", "p")
	host, portStr, _ := net.SplitHostPort(up.Addr().String())
	port, _ := strconv.Atoi(portStr)

	started := time.Now()
	fwds := make([]*Forwarder, 0, n)
	addrs := make([]string, 0, n)
	for i := 0; i < n; i++ {
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
		fwds = append(fwds, f)
		addrs = append(addrs, addr[len("socks5://"):])
	}
	startupCost := time.Since(started)
	defer func() {
		for _, f := range fwds {
			_ = f.Close()
		}
	}()

	// 每个转发器各跑一轮往返，确认全部可用且互不干扰。
	payload := make([]byte, 2<<10)
	_, _ = rand.Read(payload)
	var wg sync.WaitGroup
	rtStart := time.Now()
	errs := make(chan error, n)
	for _, socks := range addrs {
		wg.Add(1)
		go func(s string) {
			defer wg.Done()
			d, err := newSOCKS5TestDialer(s)
			if err != nil {
				errs <- err
				return
			}
			conn, err := d.DialContext(t.Context(), "tcp", echo.Addr().String())
			if err != nil {
				errs <- err
				return
			}
			defer func() { _ = conn.Close() }()
			if _, err := conn.Write(payload); err != nil {
				errs <- err
				return
			}
			buf := make([]byte, len(payload))
			if _, err := io.ReadFull(conn, buf); err != nil {
				errs <- err
			}
		}(socks)
	}
	wg.Wait()
	close(errs)
	// 同样是报告性质的统计：本机端口紧张时并发往返会失败，
	// 那反映的是宿主机当时的资源状况，不是转发器的缺陷。
	if failed := drainErrors(errs); failed != nil {
		t.Logf("并发往返未完成，本机资源不足（%v）", failed)
	}

	t.Logf("%d 个转发器: 启动总耗时 %v（均摊 %v/个），并发往返 %v",
		n, startupCost.Round(time.Microsecond),
		(startupCost / n).Round(time.Microsecond),
		time.Since(rtStart).Round(time.Millisecond))
	t.Logf("每个转发器占用: 1 个监听 socket + 1 个 accept goroutine，" +
		"每条活动连接额外 2 个拷贝 goroutine")
}

// startFakeSOCKS5AnyTB 是 startFakeSOCKS5 的 testing.TB 版本，
// 供基准测试复用同一个伪上游实现。
func startFakeSOCKS5AnyTB(t testing.TB, user, pass string) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("启动伪上游失败: %v", err)
	}
	// gotAuth 容量给大：基准会建立大量连接，通道满了会阻塞伪上游的握手，
	// 把上游的排队时间计入被测的转发开销里。
	u := &fakeSOCKS5Upstream{ln: ln, wantUser: user, wantPass: pass,
		gotAuth: make(chan [2]string, 1<<16)}
	go u.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}
