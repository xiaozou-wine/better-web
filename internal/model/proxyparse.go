package model

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// ParseProxy 解析一行代理配置。
//
// 支持两类写法，因为代理商给的格式两种都常见：
//
//	scheme://user:pass@host:port     URL 形式
//	scheme://host:port
//	host:port:user:pass              冒号分隔（代理商列表最常见）
//	host:port
//
// scheme 省略时默认 socks5。冒号分隔形式无法表达 scheme，
// 需要 HTTP 代理时必须用 URL 形式。
//
// 密码中含 @ 或 : 时只能用冒号分隔形式：URL 形式下这些字符会破坏解析，
// 而要求用户做百分号编码不现实。
func ParseProxy(line string) (*Proxy, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, fmt.Errorf("空行")
	}

	scheme := ProxySOCKS5
	rest := line
	if idx := strings.Index(line, "://"); idx >= 0 {
		got := ProxyScheme(strings.ToLower(line[:idx]))
		switch got {
		case ProxyHTTP, ProxyHTTPS, ProxySOCKS5:
			scheme = got
		case "socks", "socks5h":
			// 常见别名，都按 socks5 处理。
			scheme = ProxySOCKS5
		default:
			return nil, fmt.Errorf("不支持的代理协议 %q", line[:idx])
		}
		rest = line[idx+3:]
	}

	// URL 形式：凭据在 @ 之前。用 LastIndex 是因为密码里可能含 @。
	if at := strings.LastIndex(rest, "@"); at >= 0 {
		creds, hostPort := rest[:at], rest[at+1:]
		host, port, err := splitHostPort(hostPort)
		if err != nil {
			return nil, err
		}
		user, pass, _ := strings.Cut(creds, ":")
		return newProxy(scheme, host, port, user, pass)
	}

	// 冒号分隔形式。
	parts := strings.Split(rest, ":")
	switch len(parts) {
	case 2:
		host, port, err := splitHostPort(rest)
		if err != nil {
			return nil, err
		}
		return newProxy(scheme, host, port, "", "")
	case 4:
		port, err := parsePort(parts[1])
		if err != nil {
			return nil, err
		}
		return newProxy(scheme, parts[0], port, parts[2], parts[3])
	default:
		return nil, fmt.Errorf("格式无法识别，应为 host:port、host:port:user:pass 或 scheme://user:pass@host:port")
	}
}

func splitHostPort(s string) (string, int, error) {
	host, portStr, err := net.SplitHostPort(s)
	if err != nil {
		return "", 0, fmt.Errorf("地址 %q 格式不正确: %w", s, err)
	}
	port, err := parsePort(portStr)
	if err != nil {
		return "", 0, err
	}
	return host, port, nil
}

func parsePort(s string) (int, error) {
	port, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("端口 %q 不是数字", s)
	}
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("端口 %d 超出 1-65535", port)
	}
	return port, nil
}

func newProxy(scheme ProxyScheme, host string, port int, user, pass string) (*Proxy, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil, fmt.Errorf("缺少主机地址")
	}
	// 只有用户名没密码是可疑的配置，多半是漏填，明确拦掉而非静默接受。
	if user == "" && pass != "" {
		return nil, fmt.Errorf("填了密码但缺少用户名")
	}
	return &Proxy{
		Scheme: scheme, Host: host, Port: port,
		Username: strings.TrimSpace(user), Password: pass,
	}, nil
}

// ParseProxyList 解析多行代理配置。
//
// 返回全部成功项与全部失败项，而非遇错即停：批量粘贴一百行时因为第三行
// 格式错误就整批放弃，用户得逐行排查；分开返回可以先导入正确的，
// 再针对失败行提示。
//
// 空行与以 # 开头的注释行会被跳过。
func ParseProxyList(text string) (ok []*Proxy, failed []ProxyParseError) {
	for i, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		p, err := ParseProxy(trimmed)
		if err != nil {
			failed = append(failed, ProxyParseError{
				Line: i + 1, Raw: redactProxyLine(trimmed), Err: err.Error(),
			})
			continue
		}
		ok = append(ok, p)
	}
	return ok, failed
}

// ProxyParseError 是一行解析失败的详情。
type ProxyParseError struct {
	// Line 是 1 起的行号，便于用户定位。
	Line int `json:"line"`
	// Raw 是该行内容，密码已脱敏——错误信息会进日志和界面。
	Raw string `json:"raw"`
	Err string `json:"err"`
}

// redactProxyLine 遮蔽一行代理配置里可能的密码部分。
//
// 必须脱敏：解析失败的行会原样回显给用户并可能进日志，
// 而失败往往正是因为密码含特殊字符——那时密码就在这一行里。
func redactProxyLine(line string) string {
	if at := strings.LastIndex(line, "@"); at >= 0 {
		creds := line[:at]
		if colon := strings.Index(creds, ":"); colon >= 0 {
			// 保留 scheme://user，遮蔽密码。
			return creds[:colon] + ":***@" + line[at+1:]
		}
		return line
	}
	parts := strings.Split(line, ":")
	if len(parts) == 4 {
		return strings.Join([]string{parts[0], parts[1], parts[2], "***"}, ":")
	}
	return line
}
