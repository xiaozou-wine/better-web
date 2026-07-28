package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// devToolsPortFile 是 Chromium 写入实际调试端口的文件名。
// 传 --remote-debugging-port=0 时内核选一个空闲端口，只能从这里读回来。
const devToolsPortFile = "DevToolsActivePort"

// waitDevToolsPort 等待内核写出 DevToolsActivePort 并返回其中的端口号。
//
// 为什么需要它：并发启动多个实例时写死端口会打架，交给内核分配是唯一可靠的
// 办法（与 proxy.Forwarder 用 127.0.0.1:0 同理）。代价是端口得回读。
//
// 文件格式是两行：第一行端口，第二行 WebSocket 路径。只用第一行——
// 浏览器级的 WebSocket 地址从 /json/version 取更稳，那是官方接口。
//
// 读到端口后仍要确认 HTTP 端点真的能应答：文件写入与端口开始监听之间有窗口，
// 此时连过去会被拒。只看文件存在就返回，调用方会遇到间歇性的连接失败。
func waitDevToolsPort(ctx context.Context, profileDir string, timeout time.Duration) (int, error) {
	path := filepath.Join(profileDir, devToolsPortFile)
	deadline := time.Now().Add(timeout)
	var lastErr error

	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		port, err := readPortFile(path)
		if err != nil {
			lastErr = err
			if !sleepCtx(ctx, 200*time.Millisecond) {
				return 0, ctx.Err()
			}
			continue
		}
		// 文件里有端口不等于端口已经在监听，必须实际探一次。
		if err := probeCDP(ctx, port); err != nil {
			lastErr = err
			if !sleepCtx(ctx, 200*time.Millisecond) {
				return 0, ctx.Err()
			}
			continue
		}
		return port, nil
	}
	return 0, fmt.Errorf("等待 %s 超时（%s）: %w", devToolsPortFile, timeout, lastErr)
}

// waitCDPReady 等待指定端口上的 CDP 端点可应答。
//
// 用于调用方显式指定了端口的情况。端口被别的进程占用时，浏览器会起来但
// 调试端口不在它手上——那样 connect_over_cdp 会连到错误的目标，
// 所以这里必须验证到 /json/version 能应答，而不是只看端口通不通。
func waitCDPReady(ctx context.Context, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := probeCDP(ctx, port); err != nil {
			lastErr = err
			if !sleepCtx(ctx, 200*time.Millisecond) {
				return ctx.Err()
			}
			continue
		}
		return nil
	}
	return fmt.Errorf("超时（%s）: %w", timeout, lastErr)
}

// readPortFile 解析 DevToolsActivePort 的第一行。
func readPortFile(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	first, _, _ := strings.Cut(strings.TrimSpace(string(b)), "\n")
	port, err := strconv.Atoi(strings.TrimSpace(first))
	if err != nil {
		// 文件可能被读到只写了一半，当作"还没好"重试。
		return 0, fmt.Errorf("%s 内容不是端口号 %q: %w", devToolsPortFile, first, err)
	}
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("%s 里的端口 %d 不合法", devToolsPortFile, port)
	}
	return port, nil
}

// probeCDP 确认该端口上的 CDP HTTP 端点能正常应答。
func probeCDP(ctx context.Context, port int) error {
	url := fmt.Sprintf("http://127.0.0.1:%d/json/version", port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("CDP 端点返回 %d", resp.StatusCode)
	}
	return nil
}

// sleepCtx 睡指定时长，ctx 取消时提前返回 false。
func sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}
