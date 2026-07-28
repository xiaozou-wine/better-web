// Command diagproxy 对单个 profile 的代理做分层连通性诊断。
//
// 与 checkproxy 的区别：checkproxy 只回报"通/不通"，本命令逐层定位失败点——
// TCP 可达性、SOCKS5 握手与认证、经代理的 TCP 隧道、经代理的 TLS 与 HTTP。
// 排障时需要知道是哪一层断的，而不只是最终结果。
//
// 只读，不修改任何数据，不回显代理密码。
package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"better-web/internal/model"
	"better-web/internal/proxy"
	"better-web/internal/store"

	xproxy "golang.org/x/net/proxy"
)

func main() {
	name := flag.String("name", "", "要诊断的 profile 名称")
	sample := flag.Bool("sample", false, "改为重复采样模式，输出成功率与时延分布")
	burst := flag.Int("burst", 0, "裸 TCP 连续连接指定次数，检验是否为连接数限流")
	gap := flag.Duration("gap", 0, "burst 模式下每次连接之间的间隔")
	order := flag.Bool("order", false, "以反转顺序做隧道探测，区分目标阻断与限流")
	forward := flag.Bool("forward", false, "经本地转发监听器发请求，复刻 Chromium 实际路径")
	url := flag.String("url", "", "forward 模式下只测这一个 URL")
	proxyArg := flag.String("proxy", "", "直接指定上游代理（scheme://user:pass@host:port），不读数据库")
	flag.Parse()

	if *name == "" && *proxyArg == "" {
		fmt.Fprintln(os.Stderr, "用法: diagproxy -name <profile 名称> | -proxy <scheme://user:pass@host:port>")
		os.Exit(2)
	}

	target, err := resolveTarget(*name, *proxyArg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	switch {
	case *forward:
		runForward(target.Proxy, *url)
	case *burst > 0:
		runBurst(target.Proxy, *burst, *gap)
	case *order:
		runOrder(target.Proxy)
	case *sample:
		runSampling(target.Proxy)
	default:
		diagnose(target)
	}
}

// resolveTarget 取得待诊断的 profile：优先用 -proxy 直接构造（便于诊断已从
// 数据库删除或尚未录入的代理），否则按名称查库。
func resolveTarget(name, proxyArg string) (*model.Profile, error) {
	if proxyArg != "" {
		up, err := model.ParseProxy(proxyArg)
		if err != nil {
			return nil, fmt.Errorf("解析 -proxy 失败: %w", err)
		}
		return &model.Profile{Name: "(命令行指定)", Proxy: up}, nil
	}

	base, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("定位用户配置目录失败: %w", err)
	}
	db, err := store.Open(filepath.Join(base, "better-web", "profiles.db"))
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	defer func() { _ = db.Close() }()

	list, err := db.List()
	if err != nil {
		return nil, fmt.Errorf("读取 profile 失败: %w", err)
	}
	for _, p := range list {
		if p.Name == name {
			if p.Proxy == nil {
				return nil, fmt.Errorf("profile %q 未配置代理", name)
			}
			return p, nil
		}
	}
	return nil, fmt.Errorf("未找到名为 %q 的 profile", name)
}

func diagnose(p *model.Profile) {
	up := p.Proxy
	hostPort := net.JoinHostPort(up.Host, fmt.Sprint(up.Port))

	fmt.Printf("=== 诊断 profile %q ===\n", p.Name)
	fmt.Printf("上游代理: %s://%s（认证: %v）\n\n", up.Scheme, hostPort, up.NeedsAuth())

	// 第 1 层：到代理服务器本身的 TCP 可达性。
	step("1. TCP 连接到代理服务器", func() error {
		c, err := net.DialTimeout("tcp", hostPort, 10*time.Second)
		if err != nil {
			return err
		}
		defer func() { _ = c.Close() }()
		fmt.Printf("   本地端口 %s -> 远端 %s\n", c.LocalAddr(), c.RemoteAddr())
		return nil
	})

	// 第 2 层：SOCKS5 握手与认证。凭据错误在这一层暴露。
	dialer, dialErr := upstreamDialer(up)
	step("2. 协议握手与认证（连 example.com:80 验证）", func() error {
		if dialErr != nil {
			return dialErr
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		c, err := dialer.DialContext(ctx, "tcp", "example.com:80")
		if err != nil {
			return err
		}
		defer func() { _ = c.Close() }()
		return nil
	})

	// 第 3 层：经代理到各目标的 TCP 隧道。分目标测，能区分
	// "代理整体不通" 与 "代理屏蔽了特定目标"。
	targets := []struct{ label, addr string }{
		{"ipinfo.io:443", "ipinfo.io:443"},
		{"ip-api.com:80", "ip-api.com:80"},
		{"cloudflare.com:443", "cloudflare.com:443"},
		{"google.com:443", "google.com:443"},
		{"www.google.com:443", "www.google.com:443"},
	}
	fmt.Println("3. 经代理建立 TCP 隧道（分目标）")
	for _, t := range targets {
		substep(t.label, func() error {
			if dialErr != nil {
				return dialErr
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			c, err := dialer.DialContext(ctx, "tcp", t.addr)
			if err != nil {
				return err
			}
			_ = c.Close()
			return nil
		})
	}

	// 第 4 层：经代理完成 TLS 握手。能与第 3 层区分出
	// "隧道通但 TLS 被打断"（典型的 SNI 阻断）。
	fmt.Println("\n4. 经代理完成 TLS 握手")
	for _, host := range []string{"ipinfo.io", "cloudflare.com", "www.google.com"} {
		substep(host, func() error {
			if dialErr != nil {
				return dialErr
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			raw, err := dialer.DialContext(ctx, "tcp", host+":443")
			if err != nil {
				return fmt.Errorf("隧道建立失败: %w", err)
			}
			defer func() { _ = raw.Close() }()

			tc := tls.Client(raw, &tls.Config{ServerName: host})
			if err := tc.HandshakeContext(ctx); err != nil {
				return fmt.Errorf("TLS 握手失败: %w", err)
			}
			st := tc.ConnectionState()
			fmt.Printf("      协议 %s, 证书 CN=%s\n",
				tls.VersionName(st.Version), certSubject(st))
			return nil
		})
	}

	// 第 5 层：完整 HTTP 请求。走 Forwarder 自己的 HTTPClient，
	// 与界面上"测试代理"用的是同一条代码路径。
	fmt.Println("\n5. 经代理发起 HTTP 请求（与界面测试同路径）")
	fwd, err := proxy.New(up)
	if err != nil {
		fmt.Printf("   ✗ 构造转发器失败: %v\n", err)
		return
	}
	client, err := fwd.HTTPClient()
	if err != nil {
		fmt.Printf("   ✗ 构造 HTTP 客户端失败: %v\n", err)
		return
	}
	for _, u := range []string{
		"https://ipinfo.io/json",
		"http://ip-api.com/json/?fields=countryCode,region,query,as,isp",
		"https://cloudflare.com/cdn-cgi/trace",
		"https://www.google.com/generate_204",
		"https://www.gstatic.com/generate_204",
	} {
		substep(u, func() error {
			req, err := http.NewRequest(http.MethodGet, u, nil)
			if err != nil {
				return err
			}
			// 部分服务对无 UA 的请求直接拒绝，带一个常规 UA 排除这个变量。
			req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
			resp, err := client.Do(req)
			if err != nil {
				return err
			}
			defer func() { _ = resp.Body.Close() }()
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
			fmt.Printf("      HTTP %d  %s\n", resp.StatusCode, oneLine(string(body)))
			return nil
		})
	}
}

// upstreamDialer 复刻 proxy.Forwarder 的上游拨号构造，供分层探测直接使用。
// 该逻辑在 internal/proxy 中未导出，这里只在诊断命令内重建。
func upstreamDialer(up *model.Proxy) (interface {
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
}, error) {
	hostPort := net.JoinHostPort(up.Host, fmt.Sprint(up.Port))
	switch up.Scheme {
	case model.ProxySOCKS5:
		var auth *xproxy.Auth
		if up.NeedsAuth() {
			auth = &xproxy.Auth{User: up.Username, Password: up.Password}
		}
		d, err := xproxy.SOCKS5("tcp", hostPort, auth, &net.Dialer{Timeout: 20 * time.Second})
		if err != nil {
			return nil, fmt.Errorf("构造 SOCKS5 拨号器失败: %w", err)
		}
		cd, ok := d.(interface {
			DialContext(ctx context.Context, network, addr string) (net.Conn, error)
		})
		if !ok {
			return nil, fmt.Errorf("SOCKS5 拨号器不支持 context")
		}
		return cd, nil
	default:
		// HTTP/HTTPS 代理的 CONNECT 拨号器在 internal/proxy 内未导出，
		// 这类代理直接用第 5 层的 Forwarder 路径即可定位。
		return nil, fmt.Errorf("本命令的分层探测目前只支持 socks5，当前为 %q", up.Scheme)
	}
}

func step(label string, fn func() error) {
	start := time.Now()
	err := fn()
	ms := time.Since(start).Milliseconds()
	if err != nil {
		fmt.Printf("%s ... ✗ 失败（%d ms）\n   %v\n\n", label, ms, err)
		return
	}
	fmt.Printf("%s ... ✓ 成功（%d ms）\n\n", label, ms)
}

func substep(label string, fn func() error) {
	start := time.Now()
	err := fn()
	ms := time.Since(start).Milliseconds()
	if err != nil {
		fmt.Printf("   ✗ %-50s %d ms\n      %v\n", label, ms, err)
		return
	}
	fmt.Printf("   ✓ %-50s %d ms\n", label, ms)
}

func certSubject(st tls.ConnectionState) string {
	if len(st.PeerCertificates) == 0 {
		return "(无证书)"
	}
	return st.PeerCertificates[0].Subject.CommonName
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.TrimSpace(s)
	if len(s) > 160 {
		return s[:160] + "…"
	}
	return s
}
