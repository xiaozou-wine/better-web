package main

import (
	"strings"
	"testing"
)

// ParseProxyLine 必须在服务未初始化时也能用。
//
// 值得测：其余绑定都先调 a.svc() 拿服务，照那个模式写会让解析在
// 数据目录不可用时一并失败——而解析是纯函数，没有理由受此牵连。
// 这里用零值 App（service 为 nil，initErr 也为 nil）复现那个状态。
func TestParseProxyLineWithoutService(t *testing.T) {
	a := NewApp()

	p, err := a.ParseProxyLine("198.51.100.10:31280:user:pass")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if p.Host != "198.51.100.10" || p.Port != 31280 {
		t.Errorf("地址 = %s:%d, 期望 198.51.100.10:31280", p.Host, p.Port)
	}
	if p.Username != "user" || p.Password != "pass" {
		t.Errorf("凭据不符: user=%q, 密码是否为空=%v", p.Username, p.Password == "")
	}
	if p.Scheme != "socks5" {
		t.Errorf("协议 = %q, 期望省略时默认 socks5", p.Scheme)
	}
}

// 解析失败时错误要上抛给界面，而非返回一个空 Proxy 让表单填进垃圾值。
func TestParseProxyLineRejectsBadInput(t *testing.T) {
	a := NewApp()

	for name, in := range map[string]string{
		"空串":    "",
		"缺端口":   "198.51.100.10",
		"端口非数字": "198.51.100.10:abc",
		"协议不支持": "ftp://198.51.100.10:21",
		"段数不对":  "198.51.100.10:31280:user",
	} {
		p, err := a.ParseProxyLine(in)
		if err == nil {
			t.Errorf("%s: ParseProxyLine(%q) 期望报错，实际返回 %+v", name, in, p)
		}
		if p != nil {
			t.Errorf("%s: 报错时应返回 nil，实际 %+v", name, p)
		}
	}
}

// 密码含 @ 时必须按 LastIndex 切，这正是不在前端重写解析的理由——
// 两份实现在这个用例上很容易分叉。
func TestParseProxyLinePasswordWithAt(t *testing.T) {
	a := NewApp()

	p, err := a.ParseProxyLine("socks5://user:pa@ss@198.51.100.10:31280")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if p.Host != "198.51.100.10" || p.Port != 31280 {
		t.Errorf("地址 = %s:%d, 期望 198.51.100.10:31280", p.Host, p.Port)
	}
	if p.Username != "user" || !strings.Contains(p.Password, "@") {
		t.Errorf("凭据解析错误: user=%q, 密码含 @ = %v",
			p.Username, strings.Contains(p.Password, "@"))
	}
}
