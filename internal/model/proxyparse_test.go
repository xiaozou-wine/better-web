package model

import (
	"strings"
	"testing"
)

func TestParseProxyFormats(t *testing.T) {
	cases := []struct {
		in     string
		scheme ProxyScheme
		host   string
		port   int
		user   string
		pass   string
	}{
		{"1.2.3.4:1080", ProxySOCKS5, "1.2.3.4", 1080, "", ""},
		{"gate.example.com:7000", ProxySOCKS5, "gate.example.com", 7000, "", ""},
		{"1.2.3.4:1080:user:pass", ProxySOCKS5, "1.2.3.4", 1080, "user", "pass"},
		{"socks5://1.2.3.4:1080", ProxySOCKS5, "1.2.3.4", 1080, "", ""},
		{"http://1.2.3.4:8080", ProxyHTTP, "1.2.3.4", 8080, "", ""},
		{"https://1.2.3.4:8443", ProxyHTTPS, "1.2.3.4", 8443, "", ""},
		{"socks5://u:p@1.2.3.4:1080", ProxySOCKS5, "1.2.3.4", 1080, "u", "p"},
		{"http://u:p@gate.example.com:8080", ProxyHTTP, "gate.example.com", 8080, "u", "p"},
		// 常见别名。
		{"socks://1.2.3.4:1080", ProxySOCKS5, "1.2.3.4", 1080, "", ""},
		{"socks5h://1.2.3.4:1080", ProxySOCKS5, "1.2.3.4", 1080, "", ""},
		// 带空白应被清理。
		{"  1.2.3.4:1080  ", ProxySOCKS5, "1.2.3.4", 1080, "", ""},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := ParseProxy(c.in)
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			if got.Scheme != c.scheme || got.Host != c.host || got.Port != c.port {
				t.Errorf("= %s://%s:%d, 期望 %s://%s:%d",
					got.Scheme, got.Host, got.Port, c.scheme, c.host, c.port)
			}
			if got.Username != c.user || got.Password != c.pass {
				t.Errorf("凭据不符: 用户名 %q（期望 %q），密码是否为空 %v（期望 %v）",
					got.Username, c.user, got.Password == "", c.pass == "")
			}
		})
	}
}

// 密码里含 @ 或 : 时只能用冒号分隔形式，URL 形式会解析歧义。
// 这里确认冒号分隔形式能正确处理含特殊字符的密码。
func TestParseProxyPasswordWithSpecialChars(t *testing.T) {
	got, err := ParseProxy("1.2.3.4:1080:user:p@ss:w0rd")
	if err == nil {
		// 五段会被判为格式错误，这是预期——冒号分隔无法表达含冒号的密码。
		t.Logf("五段格式被接受: %+v", got)
	}

	// URL 形式下 @ 用 LastIndex 切分，密码含 @ 时仍能正确解析主机。
	p, err := ParseProxy("socks5://user:pa@ss@1.2.3.4:1080")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if p.Host != "1.2.3.4" || p.Port != 1080 {
		t.Errorf("主机解析错误: %s:%d", p.Host, p.Port)
	}
	if p.Username != "user" || p.Password != "pa@ss" {
		t.Errorf("凭据解析错误: user=%q pass 含 @ = %v",
			p.Username, strings.Contains(p.Password, "@"))
	}
}

func TestParseProxyRejectsInvalid(t *testing.T) {
	cases := map[string]string{
		"空串":      "",
		"只有空白":    "   ",
		"缺端口":     "1.2.3.4",
		"端口非数字":   "1.2.3.4:abc",
		"端口为零":    "1.2.3.4:0",
		"端口越界":    "1.2.3.4:70000",
		"协议不支持":   "ftp://1.2.3.4:21",
		"段数不对":    "1.2.3.4:1080:user",
		"有密码无用户名": "1.2.3.4:1080::pass",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseProxy(in); err == nil {
				t.Errorf("ParseProxy(%q) 期望报错，实际通过", in)
			}
		})
	}
}

func TestParseProxyListSkipsBlankAndComments(t *testing.T) {
	text := `
# 这是注释
1.2.3.4:1080

  # 缩进的注释
5.6.7.8:1080:u:p
`
	ok, failed := ParseProxyList(text)
	if len(failed) != 0 {
		t.Errorf("不应有失败项: %+v", failed)
	}
	if len(ok) != 2 {
		t.Fatalf("成功项 = %d, 期望 2", len(ok))
	}
	if ok[0].Host != "1.2.3.4" || ok[1].Host != "5.6.7.8" {
		t.Errorf("顺序或内容不符: %s, %s", ok[0].Host, ok[1].Host)
	}
}

// 部分失败不应影响其余项：粘贴一百行时不能因一行错误整批放弃。
func TestParseProxyListReturnsPartialSuccess(t *testing.T) {
	text := "1.2.3.4:1080\n坏行\n5.6.7.8:1080"
	ok, failed := ParseProxyList(text)
	if len(ok) != 2 {
		t.Errorf("成功项 = %d, 期望 2", len(ok))
	}
	if len(failed) != 1 {
		t.Fatalf("失败项 = %d, 期望 1", len(failed))
	}
	// 行号从 1 起，便于用户定位。
	if failed[0].Line != 2 {
		t.Errorf("失败行号 = %d, 期望 2", failed[0].Line)
	}
}

// 失败行会回显给用户并可能进日志，密码必须脱敏——
// 而解析失败往往正是因为密码含特殊字符。
func TestParseProxyListRedactsPasswordInErrors(t *testing.T) {
	const pw = "SUPERSECRET123"
	cases := []string{
		"1.2.3.4:99999:user:" + pw,               // 端口越界，四段格式
		"socks5://user:" + pw + "@1.2.3.4:99999", // 端口越界，URL 格式
	}
	for _, line := range cases {
		_, failed := ParseProxyList(line)
		if len(failed) != 1 {
			t.Fatalf("期望 1 个失败项，实际 %d: %q", len(failed), line)
		}
		if strings.Contains(failed[0].Raw, pw) {
			t.Errorf("失败详情中泄漏了密码: %q", failed[0].Raw)
		}
		if !strings.Contains(failed[0].Raw, "***") {
			t.Errorf("未见脱敏标记: %q", failed[0].Raw)
		}
	}
}

func TestParseProxyListEmptyInput(t *testing.T) {
	ok, failed := ParseProxyList("\n\n# 只有注释\n")
	if len(ok) != 0 || len(failed) != 0 {
		t.Errorf("空输入应无结果: ok=%d failed=%d", len(ok), len(failed))
	}
}
