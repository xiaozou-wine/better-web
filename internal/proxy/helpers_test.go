package proxy

import (
	"errors"
	"net"

	"golang.org/x/net/proxy"
)

// newSOCKS5TestDialer 构造一个免认证的 SOCKS5 拨号器，模拟 Chromium
// 通过 --proxy-server 连接本地转发器的行为。
func newSOCKS5TestDialer(addr string) (contextDialer, error) {
	d, err := proxy.SOCKS5("tcp", addr, nil, &net.Dialer{})
	if err != nil {
		return nil, err
	}
	cd, ok := d.(contextDialer)
	if !ok {
		return nil, errors.New("SOCKS5 拨号器不支持 context")
	}
	return cd, nil
}
