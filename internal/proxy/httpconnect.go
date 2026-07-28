package proxy

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
)

// httpConnectDialer 通过 HTTP 代理的 CONNECT 方法建立 TCP 隧道。
// x/net/proxy 只提供 SOCKS5 客户端，HTTP 代理需自行实现。
type httpConnectDialer struct {
	// proxyURL 的 Scheme 为 http 或 https，User 中携带可选的认证凭据。
	proxyURL *url.URL
}

// DialContext 与代理建立连接并请求隧道到 addr。
//
// 注意 scheme 为 https 时，这里的 TLS 只加密"浏览器到代理"这一段；
// 隧道内的流量仍是浏览器自己的 TLS，全程不被解密，因此 JA3/JA4 指纹保持原样。
func (d *httpConnectDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, fmt.Errorf("HTTP 代理不支持 %q 网络", network)
	}

	dialer := &net.Dialer{Timeout: dialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", d.proxyURL.Host)
	if err != nil {
		return nil, fmt.Errorf("连接 HTTP 代理失败: %w", err)
	}

	if d.proxyURL.Scheme == "https" {
		host, _, splitErr := net.SplitHostPort(d.proxyURL.Host)
		if splitErr != nil {
			host = d.proxyURL.Host
		}
		tlsConn := tls.Client(conn, &tls.Config{ServerName: host})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("与 HTTP 代理的 TLS 握手失败: %w", err)
		}
		conn = tlsConn
	}

	if err := d.doConnect(ctx, conn, addr); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func (d *httpConnectDialer) doConnect(ctx context.Context, conn net.Conn, addr string) error {
	req := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Opaque: addr},
		Host:   addr,
		Header: make(http.Header),
	}
	if user := d.proxyURL.User; user != nil {
		pass, _ := user.Password()
		cred := base64.StdEncoding.EncodeToString([]byte(user.Username() + ":" + pass))
		req.Header.Set("Proxy-Authorization", "Basic "+cred)
	}

	if err := req.WriteProxy(conn); err != nil {
		return fmt.Errorf("发送 CONNECT 请求失败: %w", err)
	}

	// 若 ctx 在读应答期间取消，关闭连接以中断阻塞的读。
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		return fmt.Errorf("读取 CONNECT 应答失败: %w", err)
	}
	// CONNECT 成功时 Body 为空，无需读取，但仍需关闭以释放资源。
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		// 不回显应答体：代理返回的错误页可能包含凭据或账号信息。
		return fmt.Errorf("代理拒绝 CONNECT: HTTP %d", resp.StatusCode)
	}
	if n := br.Buffered(); n > 0 {
		// 代理在应答后就抢先发了数据，说明它不是纯隧道，透传会错位。
		return fmt.Errorf("代理在 CONNECT 应答后附带了 %d 字节数据，无法建立纯隧道", n)
	}
	return nil
}
