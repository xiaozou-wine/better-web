// Package proxy 提供本地代理转发。
//
// 存在原因：fingerprint-chromium 的 --proxy-server 不支持带密码的认证
// （见其 README："password authentication not supported"），而住宅代理几乎
// 都需要认证。转发器在本地监听一个无认证端口，向上游补上凭据。
//
// 关键约束：转发必须是纯 TCP 隧道，绝不解密或重新协商 TLS。Chromium 的
// ClientHello 由 BoringSSL 生成，与真实 Chrome 一致，这是资产而非负担；
// 任何 TLS 中间人都会改写 JA3/JA4 指纹，反而制造破绽。
package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"better-web/internal/model"
	"golang.org/x/net/proxy"
)

// dialTimeout 是连接上游代理的超时。
const dialTimeout = 20 * time.Second

// Forwarder 是一个本地 SOCKS5 监听器，把流量透传到需要认证的上游代理。
// 每个运行中的 profile 独占一个 Forwarder，互不影响。
type Forwarder struct {
	upstream *model.Proxy

	// OnError 在单条连接处理失败时被调用，可为 nil。
	//
	// 存在原因：转发失败必须能被观测。单条连接的错误不能中断转发器，
	// 但静默丢弃会让「代理明明能用、浏览器就是打不开」这类故障无法定位。
	// 回调里不得回显凭据——传入的错误已由 handle 保证不含密码。
	//
	// 可能被多个连接的 goroutine 并发调用，实现方需自行保证并发安全。
	OnError func(err error)

	mu       sync.Mutex
	listener net.Listener
	closed   bool
	wg       sync.WaitGroup
}

// New 为指定上游代理创建转发器。
func New(upstream *model.Proxy) (*Forwarder, error) {
	if upstream == nil {
		return nil, errors.New("上游代理为空")
	}
	if upstream.Host == "" || upstream.Port <= 0 || upstream.Port > 65535 {
		return nil, fmt.Errorf("上游代理地址无效: %s:%d", upstream.Host, upstream.Port)
	}
	switch upstream.Scheme {
	case model.ProxyHTTP, model.ProxyHTTPS, model.ProxySOCKS5:
	default:
		return nil, fmt.Errorf("不支持的代理协议: %q", upstream.Scheme)
	}
	return &Forwarder{upstream: upstream}, nil
}

// Start 在 127.0.0.1 的随机端口上开始监听，返回可直接传给
// --proxy-server 的地址（形如 socks5://127.0.0.1:41234）。
//
// 只绑定回环地址：转发器不带认证，绑到 0.0.0.0 会让同网段任何人都能白用
// 你的代理。
func (f *Forwarder) Start() (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return "", errors.New("转发器已关闭")
	}
	if f.listener != nil {
		return f.addr(), nil
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("监听本地端口失败: %w", err)
	}
	f.listener = ln

	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		f.serve(ln)
	}()
	return f.addr(), nil
}

// addr 返回监听地址，调用方须持有 f.mu。
func (f *Forwarder) addr() string {
	return "socks5://" + f.listener.Addr().String()
}

// Close 停止监听并等待在途连接结束。
func (f *Forwarder) Close() error {
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return nil
	}
	f.closed = true
	ln := f.listener
	f.mu.Unlock()

	var err error
	if ln != nil {
		err = ln.Close()
	}
	f.wg.Wait()
	return err
}

// HTTPClient 返回一个经上游代理出网的 HTTP 客户端，用于查询代理出口地。
// 出口地必须经代理查，直连查到的是本机地址。
func (f *Forwarder) HTTPClient() (*http.Client, error) {
	dialer, err := f.upstreamDialer()
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialer.DialContext(ctx, network, addr)
			},
			// 出口地探测是一次性的短请求，不必保留长连接。
			DisableKeepAlives: true,
		},
	}, nil
}

// contextDialer 是能感知 context 的拨号器。
type contextDialer interface {
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
}

// upstreamDialer 构造连接上游代理的拨号器，并带上认证凭据。
func (f *Forwarder) upstreamDialer() (contextDialer, error) {
	up := f.upstream
	hostPort := net.JoinHostPort(up.Host, fmt.Sprint(up.Port))

	switch up.Scheme {
	case model.ProxySOCKS5:
		var auth *proxy.Auth
		if up.NeedsAuth() {
			auth = &proxy.Auth{User: up.Username, Password: up.Password}
		}
		d, err := proxy.SOCKS5("tcp", hostPort, auth, &net.Dialer{Timeout: dialTimeout})
		if err != nil {
			return nil, fmt.Errorf("构造 SOCKS5 拨号器失败: %w", err)
		}
		cd, ok := d.(contextDialer)
		if !ok {
			return nil, errors.New("SOCKS5 拨号器不支持 context")
		}
		return cd, nil

	case model.ProxyHTTP, model.ProxyHTTPS:
		u := &url.URL{Scheme: string(up.Scheme), Host: hostPort}
		if up.NeedsAuth() {
			u.User = url.UserPassword(up.Username, up.Password)
		}
		return &httpConnectDialer{proxyURL: u}, nil
	}
	return nil, fmt.Errorf("不支持的代理协议: %q", up.Scheme)
}
