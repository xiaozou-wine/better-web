package main

import (
	"context"
	"fmt"
	"net"
	"time"

	"better-web/internal/model"
)

// 采样模式下观察到一个矛盾：裸 TCP 连代理 8/8 成功且 255ms，但经 SOCKS5
// 拨号却报「连代理超时」，且失败耗时恒定等于拨号器超时（SYN 无响应）。
//
// 两种假说会产生不同的处置：
//   A. 目标相关——代理对特定目标（如 google）做了阻断
//   B. 顺序相关——短时间内连接数累积后被限流，谁排在后面谁失败
//
// 下面两个实验分别否证其中一个：burst 只做裸连接不带 SOCKS，看纯连接数
// 是否触发失败；order 把目标顺序反过来，看失败是否跟着目标走还是跟着位置走。

// burstDialTimeout 单独定义而不复用 upstreamDialer 里的 20s：
// 这里要的是快速判定 SYN 是否被丢，不需要等满超时。
const burstDialTimeout = 8 * time.Second

// runBurst 连续做裸 TCP 连接，检验失败是否由连接数累积触发。
func runBurst(up *model.Proxy, rounds int, gap time.Duration) {
	hostPort := net.JoinHostPort(up.Host, fmt.Sprint(up.Port))
	fmt.Printf("=== 实验 B：裸 TCP 连续连接 %d 次（间隔 %s，不含 SOCKS 握手）===\n", rounds, gap)
	fmt.Printf("上游: %s\n目的：若失败随次数累积出现，则为连接数限流而非目标阻断\n\n", hostPort)

	fails := 0
	for i := 1; i <= rounds; i++ {
		start := time.Now()
		c, err := net.DialTimeout("tcp", hostPort, burstDialTimeout)
		ms := time.Since(start).Milliseconds()
		if err != nil {
			fails++
			fmt.Printf("  #%02d ✗ %5d ms  %v\n", i, ms, err)
		} else {
			fmt.Printf("  #%02d ✓ %5d ms  本地 %s\n", i, ms, c.LocalAddr())
			_ = c.Close()
		}
		if gap > 0 && i < rounds {
			time.Sleep(gap)
		}
	}
	fmt.Printf("\n  裸连接失败 %d/%d\n", fails, rounds)
}

// runOrder 以反转的目标顺序做隧道探测，检验失败是否跟着目标走。
func runOrder(up *model.Proxy) {
	dialer, err := upstreamDialer(up)
	if err != nil {
		fmt.Printf("构造拨号器失败: %v\n", err)
		return
	}

	fmt.Println("\n=== 实验 C：反转顺序的隧道探测 ===")
	fmt.Println("目的：若 google 排在最前也失败、ipinfo 排在最后也成功，则是目标阻断；")
	fmt.Println("     若失败总落在靠后的位置，则是限流。")
	fmt.Println()

	// google 刻意放在最前，ipinfo/example 放在后面——与采样模式相反。
	seq := []string{
		"www.google.com:443",
		"www.google.com:443",
		"www.google.com:443",
		"ipinfo.io:443",
		"ipinfo.io:443",
		"example.com:80",
		"www.google.com:443",
		"ipinfo.io:443",
	}
	for i, t := range seq {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		c, err := dialer.DialContext(ctx, "tcp", t)
		ms := time.Since(start).Milliseconds()
		cancel()
		if err != nil {
			fmt.Printf("  #%02d ✗ %-22s %5d ms  %v\n", i+1, t, ms, err)
			continue
		}
		_ = c.Close()
		fmt.Printf("  #%02d ✓ %-22s %5d ms\n", i+1, t, ms)
	}
}
