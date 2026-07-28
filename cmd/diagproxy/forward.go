package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"better-web/internal/model"
	"better-web/internal/proxy"

	xproxy "golang.org/x/net/proxy"
)

// 上一轮诊断只测了 Forwarder.HTTPClient，那条路径直接用上游拨号器发请求。
// 但 Chromium 走的是完全不同的一段：连本地监听器，由 handle 完成本地 SOCKS5
// 握手后再转给上游。两段代码的超时与错误处理各自独立，只测前者会漏掉后者的
// 缺陷。runForward 补上这段，模拟浏览器视角。

// runForward 经本地转发监听器发请求，复刻 Chromium 的实际路径。
func runForward(up *model.Proxy, target string) {
	fmt.Println("=== 经本地转发器（Chromium 实际路径）===")
	fmt.Printf("上游: %s://%s:%d\n", up.Scheme, up.Host, up.Port)

	fwd, err := proxy.New(up)
	if err != nil {
		fmt.Printf("✗ 构造转发器失败: %v\n", err)
		return
	}
	// 转发器内部的失败原因只能从这里拿到：客户端侧只会看到笼统的 EOF。
	var errMu sync.Mutex
	fwd.OnError = func(err error) {
		errMu.Lock()
		defer errMu.Unlock()
		fmt.Printf("      ↳ 转发器内部: %v\n", err)
	}
	addr, err := fwd.Start()
	if err != nil {
		fmt.Printf("✗ 启动转发器失败: %v\n", err)
		return
	}
	defer func() { _ = fwd.Close() }()
	fmt.Printf("本地监听: %s（传给 --proxy-server 的正是这个）\n\n", addr)

	// 与 Chromium 一样，以无认证 SOCKS5 连本地监听器。
	d, err := xproxy.SOCKS5("tcp", strings.TrimPrefix(addr, "socks5://"), nil, &net.Dialer{})
	if err != nil {
		fmt.Printf("✗ 构造本地 SOCKS5 客户端失败: %v\n", err)
		return
	}
	cd, ok := d.(interface {
		DialContext(ctx context.Context, network, addr string) (net.Conn, error)
	})
	if !ok {
		fmt.Println("✗ 本地 SOCKS5 拨号器不支持 context")
		return
	}

	client := &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, a string) (net.Conn, error) {
				return cd.DialContext(ctx, network, a)
			},
			DisableKeepAlives: true,
		},
	}

	urls := []string{target}
	if target == "" {
		urls = []string{
			"https://api.ipify.org",
			"https://www.google.com/generate_204",
			"https://ipinfo.io/json",
		}
	}

	for _, u := range urls {
		for i := 1; i <= 3; i++ {
			start := time.Now()
			req, err := http.NewRequest(http.MethodGet, u, nil)
			if err != nil {
				fmt.Printf("   ✗ 构造请求失败: %v\n", err)
				break
			}
			req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
			resp, err := client.Do(req)
			ms := time.Since(start).Milliseconds()
			if err != nil {
				fmt.Printf("   ✗ #%d %-42s %6d ms  %v\n", i, u, ms, err)
				continue
			}
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
			_ = resp.Body.Close()
			fmt.Printf("   ✓ #%d %-42s %6d ms  HTTP %d  %s\n",
				i, u, ms, resp.StatusCode, oneLine(string(body)))
		}
	}
}
