package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"sort"
	"time"

	"better-web/internal/model"
)

// sampleRounds 是每项探测的重复次数。单次结果无法区分"不通"与"偶发丢包"，
// 而这两者的处置完全不同，因此所有结论都基于多次采样。
//
// 取 4 而非更多：实测中 8 轮 × 多目标的连打足以把并发敏感的上游打进限流，
// 于是测出来的失败是自己造成的，而非线路固有属性——诊断工具污染了自己的证据。
// 4 轮已能区分"稳定不通"与"偶发丢包"，再多只是增加污染风险。
const sampleRounds = 4

// sampleGap 是同一项内两次探测之间的间隔。
//
// 与降低轮次同样重要：打崩上游的是请求密度而不只是总次数。留出间隔让上游的
// 并发计数回落，测到的才是线路本身的表现。
const sampleGap = 2 * time.Second

// sampleTimeout 刻意放宽到远超正常水平：目的是测出真实时延分布，
// 而不是在某个阈值上得到一个二元结果。
const sampleTimeout = 45 * time.Second

// runSampling 对代理做重复采样，输出成功率与时延分布。
func runSampling(up *model.Proxy) {
	hostPort := net.JoinHostPort(up.Host, fmt.Sprint(up.Port))
	fmt.Printf("=== 重复采样（每项 %d 次，间隔 %s，单次上限 %s）===\n",
		sampleRounds, sampleGap, sampleTimeout)
	fmt.Printf("上游: %s://%s\n\n", up.Scheme, hostPort)

	fmt.Println("A. 裸 TCP 连到代理端口（不含 SOCKS 握手）")
	report("tcp-connect", func() (string, error) {
		c, err := net.DialTimeout("tcp", hostPort, sampleTimeout)
		if err != nil {
			return "", err
		}
		_ = c.Close()
		return "", nil
	})

	dialer, err := upstreamDialer(up)
	if err != nil {
		fmt.Printf("构造拨号器失败: %v\n", err)
		return
	}

	fmt.Println("\nB. 经代理建立隧道（SOCKS 握手 + 认证 + CONNECT）")
	for _, t := range []string{"example.com:80", "ipinfo.io:443", "www.google.com:443"} {
		report(t, func() (string, error) {
			ctx, cancel := context.WithTimeout(context.Background(), sampleTimeout)
			defer cancel()
			c, err := dialer.DialContext(ctx, "tcp", t)
			if err != nil {
				return "", err
			}
			_ = c.Close()
			return "", nil
		})
	}

	fmt.Println("\nC. 经代理完成 TLS 握手（隧道之上）")
	for _, host := range []string{"ipinfo.io", "www.google.com"} {
		report(host, func() (string, error) {
			ctx, cancel := context.WithTimeout(context.Background(), sampleTimeout)
			defer cancel()
			raw, err := dialer.DialContext(ctx, "tcp", host+":443")
			if err != nil {
				return "", fmt.Errorf("隧道: %w", err)
			}
			defer func() { _ = raw.Close() }()
			tc := tls.Client(raw, &tls.Config{ServerName: host})
			if err := tc.HandshakeContext(ctx); err != nil {
				return "", fmt.Errorf("TLS: %w", err)
			}
			return tls.VersionName(tc.ConnectionState().Version), nil
		})
	}
}

// report 执行 sampleRounds 次探测并打印统计。
func report(label string, fn func() (string, error)) {
	var okMs []int64
	var failMs []int64
	var lastErr error
	var note string

	for i := range sampleRounds {
		if i > 0 {
			time.Sleep(sampleGap)
		}
		start := time.Now()
		n, err := fn()
		ms := time.Since(start).Milliseconds()
		if err != nil {
			failMs = append(failMs, ms)
			lastErr = err
			continue
		}
		okMs = append(okMs, ms)
		if n != "" {
			note = n
		}
	}

	sort.Slice(okMs, func(i, j int) bool { return okMs[i] < okMs[j] })
	fmt.Printf("   %-22s 成功 %d/%d", label, len(okMs), sampleRounds)
	if len(okMs) > 0 {
		fmt.Printf("  最快 %d ms / 中位 %d ms / 最慢 %d ms",
			okMs[0], okMs[len(okMs)/2], okMs[len(okMs)-1])
	}
	if note != "" {
		fmt.Printf("  [%s]", note)
	}
	fmt.Println()
	if lastErr != nil {
		fmt.Printf("      失败样本时延 %v ms，最后一次错误: %v\n", failMs, lastErr)
	}
}
