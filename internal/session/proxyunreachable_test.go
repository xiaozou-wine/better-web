package session

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"

	"better-web/internal/geo"
)

// proxyUnreachable 区分两类"全部出口地查询服务失败"：
// 代理本身连不通（建连阶段的 net.OpError）与查询服务抖动（隧道已建立
// 之后才失败，如超时）。resolveGeo 的 StrictGeo 分支依赖这条判定来决定
// 中止启动还是降级继续，见 session.go 的注释。

// 代理连不通：每次请求都在建连阶段失败。SOCKS5 是 "socks connect"，
// HTTP 代理是 "dial"，错误链里都带 net.OpError。
func TestProxyUnreachableDetectsDeadProxy(t *testing.T) {
	socksErr := &net.OpError{
		Op:  "socks connect",
		Net: "tcp",
		Err: &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")},
	}
	httpDialErr := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}

	err := fmt.Errorf("%w: %w", geo.ErrAllEndpointsFailed, errors.Join(
		fmt.Errorf("ipinfo.io: Get %q: %w", "https://ipinfo.io/json", socksErr),
		fmt.Errorf("ip-api.com: Get %q: %w", "http://ip-api.com/", socksErr),
		fmt.Errorf("cloudflare: Get %q: %w", "https://cloudflare.com/", httpDialErr),
	))
	if !proxyUnreachable(err) {
		t.Error("代理连不通时应判定为不可达，实际不是")
	}
}

// 查询服务全部抖动（超时）但代理本身可达：错误链里只有 context deadline，
// 没有建连阶段的 OpError，不得判定为代理不可达。
func TestProxyUnreachableAllowsQueryServiceJitter(t *testing.T) {
	err := fmt.Errorf("%w: %w", geo.ErrAllEndpointsFailed, errors.Join(
		fmt.Errorf("ipinfo.io: %w", context.DeadlineExceeded),
		fmt.Errorf("ip-api.com: %w", context.DeadlineExceeded),
		fmt.Errorf("cloudflare: %w", context.DeadlineExceeded),
	))
	if proxyUnreachable(err) {
		t.Error("查询服务超时不是代理不可达，应判定为可达，实际判定为不可达")
	}
}

func TestProxyUnreachableNil(t *testing.T) {
	if proxyUnreachable(nil) {
		t.Error("nil 错误不应判定为代理不可达")
	}
}
