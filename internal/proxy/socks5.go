package proxy

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

// SOCKS5 协议常量，见 RFC 1928。
const (
	socks5Version = 0x05

	authNoneRequired = 0x00
	authNoAcceptable = 0xFF

	cmdConnect = 0x01

	addrTypeIPv4   = 0x01
	addrTypeDomain = 0x03
	addrTypeIPv6   = 0x04

	replySucceeded          = 0x00
	replyGeneralFailure     = 0x01
	replyHostUnreachable    = 0x04
	replyCmdNotSupported    = 0x07
	replyAddrTypeNotSupport = 0x08
)

// clientHandshakeTimeout 限制读取本地客户端握手的耗时，避免半开连接堆积。
//
// 只覆盖「读客户端的协商与 CONNECT 请求」这一段：对端是本机的 Chromium，
// 字节早已在内核缓冲里，正常在微秒级完成，10s 已是极宽松的上限。
//
// 刻意不覆盖后续的上游拨号：那一段的预算是 dialTimeout，跨公网且可能重试，
// 远长于本值。若用同一个 deadline 罩住两段，上游握手慢于本值时会出现
// 「上游已建好隧道、本地却因 deadline 到期无法回复应答」的静默失败。
const clientHandshakeTimeout = 10 * time.Second

// replyTimeout 限制把 SOCKS 应答写回本地客户端的耗时。
// 与拨号分离设置，确保上游耗时不挤占回复的预算。
const replyTimeout = 10 * time.Second

// serve 接受本地连接并逐个转发。仅在 Start 启动的 goroutine 中调用。
func (f *Forwarder) serve(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			// 监听器关闭是正常退出路径，其余错误无法在此处恢复，
			// 交由调用方通过 Close 感知。
			return
		}
		f.wg.Add(1)
		go func() {
			defer f.wg.Done()
			defer func() { _ = conn.Close() }()
			// 单条连接失败不应影响其他连接，错误在此处终结；
			// 但要经 OnError 上报，否则故障不可观测。
			if err := f.handle(conn); err != nil && f.OnError != nil {
				f.OnError(err)
			}
		}()
	}
}

// handle 处理一条本地 SOCKS5 连接：完成握手，向上游建立隧道，然后双向透传。
func (f *Forwarder) handle(local net.Conn) error {
	// 第一段：读本地客户端的握手。对端在本机，预算短。
	if err := local.SetDeadline(time.Now().Add(clientHandshakeTimeout)); err != nil {
		return err
	}

	if err := negotiateAuth(local); err != nil {
		return err
	}
	target, err := readConnectRequest(local)
	if err != nil {
		return err
	}

	dialer, err := f.upstreamDialer()
	if err != nil {
		_ = replyWithDeadline(local, replyGeneralFailure)
		return err
	}

	// 第二段：拨上游。清掉本地 deadline，否则跨公网的拨号会被本机侧的
	// 短 deadline 提前掐断——上游明明成功了，却无法把应答写回客户端。
	// 这一段的超时由下面的 ctx 独立控制。
	if err := local.SetDeadline(time.Time{}); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()
	remote, err := dialer.DialContext(ctx, "tcp", target)
	if err != nil {
		_ = replyWithDeadline(local, replyHostUnreachable)
		return fmt.Errorf("连接上游失败: %w", err)
	}
	defer func() { _ = remote.Close() }()

	// 第三段：回应答。重新设一个独立的短 deadline，防止客户端不收时挂死。
	if err := replyWithDeadline(local, replySucceeded); err != nil {
		return err
	}
	// 隧道建立后取消超时：浏览器会话可能长时间空闲（如 WebSocket 长连接）。
	if err := local.SetDeadline(time.Time{}); err != nil {
		return err
	}
	if err := tunnel(local, remote); err != nil {
		return fmt.Errorf("隧道 %s 中断: %w", target, err)
	}
	return nil
}

// negotiateAuth 完成方法协商。本地转发器只监听回环地址，因此声明"无需认证"；
// 上游所需的凭据由 upstreamDialer 负责补上。
func negotiateAuth(conn net.Conn) error {
	head := make([]byte, 2)
	if _, err := io.ReadFull(conn, head); err != nil {
		return fmt.Errorf("读取 SOCKS5 握手失败: %w", err)
	}
	if head[0] != socks5Version {
		return fmt.Errorf("不支持的 SOCKS 版本: %d", head[0])
	}
	methods := make([]byte, int(head[1]))
	if _, err := io.ReadFull(conn, methods); err != nil {
		return fmt.Errorf("读取认证方法列表失败: %w", err)
	}
	for _, m := range methods {
		if m == authNoneRequired {
			_, err := conn.Write([]byte{socks5Version, authNoneRequired})
			return err
		}
	}
	_, _ = conn.Write([]byte{socks5Version, authNoAcceptable})
	return errors.New("客户端不支持免认证方式")
}

// readConnectRequest 解析 CONNECT 请求，返回 host:port 形式的目标地址。
func readConnectRequest(conn net.Conn) (string, error) {
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		return "", fmt.Errorf("读取 SOCKS5 请求失败: %w", err)
	}
	if head[0] != socks5Version {
		return "", fmt.Errorf("不支持的 SOCKS 版本: %d", head[0])
	}
	if head[1] != cmdConnect {
		_ = writeReply(conn, replyCmdNotSupported)
		return "", fmt.Errorf("不支持的命令: %d", head[1])
	}

	var host string
	switch head[3] {
	case addrTypeIPv4:
		b := make([]byte, net.IPv4len)
		if _, err := io.ReadFull(conn, b); err != nil {
			return "", err
		}
		host = net.IP(b).String()
	case addrTypeIPv6:
		b := make([]byte, net.IPv6len)
		if _, err := io.ReadFull(conn, b); err != nil {
			return "", err
		}
		host = net.IP(b).String()
	case addrTypeDomain:
		var n [1]byte
		if _, err := io.ReadFull(conn, n[:]); err != nil {
			return "", err
		}
		b := make([]byte, int(n[0]))
		if _, err := io.ReadFull(conn, b); err != nil {
			return "", err
		}
		// 域名保持原样交给上游解析：本地解析会导致 DNS 请求绕过代理，
		// 泄露访问过的站点。
		host = string(b)
	default:
		_ = writeReply(conn, replyAddrTypeNotSupport)
		return "", fmt.Errorf("不支持的地址类型: %d", head[3])
	}

	var portBuf [2]byte
	if _, err := io.ReadFull(conn, portBuf[:]); err != nil {
		return "", err
	}
	port := binary.BigEndian.Uint16(portBuf[:])
	return net.JoinHostPort(host, strconv.Itoa(int(port))), nil
}

// replyWithDeadline 在独立的写超时下回复应答。
//
// 上游拨号期间本地 deadline 被清空（见 handle），因此写回时必须自带一个
// 新的 deadline，否则客户端不读时这次写会无限期挂住，goroutine 泄漏。
func replyWithDeadline(conn net.Conn, code byte) error {
	if err := conn.SetWriteDeadline(time.Now().Add(replyTimeout)); err != nil {
		return err
	}
	return writeReply(conn, code)
}

// writeReply 回复一个 SOCKS5 应答。绑定地址填 0.0.0.0:0，
// 客户端在 CONNECT 场景下不使用该字段。
func writeReply(conn net.Conn, code byte) error {
	_, err := conn.Write([]byte{
		socks5Version, code, 0x00, addrTypeIPv4,
		0, 0, 0, 0,
		0, 0,
	})
	return err
}

// tunnel 在两条连接间双向透传，任一方向结束即收敛，返回该方向的错误。
// 全程不解析载荷，保证 TLS 握手原样透传，不破坏 Chromium 的 JA3/JA4 指纹。
//
// 刻意只等一个方向：等两个方向都结束会让 Close 的 wg.Wait 卡在空闲连接上。
// 先结束的一方已 CloseWrite，调用方随后关闭两条连接，另一个 copy 随之解除
// 阻塞——channel 有缓冲，那个 goroutine 不会泄漏。
func tunnel(a, b net.Conn) error {
	errs := make(chan error, 2)
	cp := func(dst, src net.Conn) {
		_, err := io.Copy(dst, src)
		// 单向结束时关闭写端，让对端感知 EOF 而不是一直等待。
		if cw, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
		errs <- err
	}
	go cp(a, b)
	go cp(b, a)
	return <-errs
}
