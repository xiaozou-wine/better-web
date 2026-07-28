package urlhandler

import "testing"

// TestValidateURLAccepts 钉住放行的形式。
func TestValidateURLAccepts(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://example.com", "https://example.com"},
		{"http://example.com/path?q=1", "http://example.com/path?q=1"},
		{"HTTPS://Example.com/", "https://Example.com/"},
		// 两侧空白由系统传参时很常见，不该因此拒绝。
		{"  https://example.com/a  ", "https://example.com/a"},
		// 带端口、用户名、片段都是正常链接。
		{"https://example.com:8443/x#frag", "https://example.com:8443/x#frag"},
	}
	for _, c := range cases {
		got, err := ValidateURL(c.in)
		if err != nil {
			t.Errorf("ValidateURL(%q) 意外报错: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ValidateURL(%q) = %q, 期望 %q", c.in, got, c.want)
		}
	}
}

// TestValidateURLRejects 钉住必须拒绝的形式。
//
// 这是安全边界：注册成默认浏览器后，机器上任何应用都能往这个入口传字符串。
// 放行任何一类都等于把浏览器当跳板，因此逐类断言。
func TestValidateURLRejects(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"空串", ""},
		{"只有空白", "   "},
		{"file 能读本地文件", "file:///C:/Windows/win.ini"},
		{"javascript 能执行代码", "javascript:alert(1)"},
		{"大写 JavaScript 同样拒绝", "JavaScript:alert(1)"},
		{"data 能执行代码", "data:text/html,<script>alert(1)</script>"},
		{"chrome 能改浏览器设置", "chrome://settings/"},
		{"扩展页面", "chrome-extension://abc/page.html"},
		{"自定义 scheme 会二次跳转", "myapp://do-something"},
		{"缺少 scheme", "example.com"},
		{"缺少主机名", "http://"},
		{"主机名以 - 开头会被当开关", "http://-foo/"},
		{"含换行符", "https://example.com\nhttps://evil.com"},
		{"含 NUL", "https://example.com\x00"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got, err := ValidateURL(c.in); err == nil {
				t.Errorf("ValidateURL(%q) 应当报错，却返回了 %q", c.in, got)
			}
		})
	}
}
