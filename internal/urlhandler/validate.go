package urlhandler

import (
	"fmt"
	"net/url"
	"strings"
)

// ValidateURL 校验一个从系统传进来的 URL 是否可以交给浏览器打开。
//
// 这是本项目唯一接受任意外部字符串并把它交给浏览器的入口——注册成默认
// 浏览器后，机器上任何应用都能往这里传值。因此按白名单放行而非黑名单拦截：
// 黑名单永远漏，而漏掉的后果是拿浏览器当跳板。
//
// 只放行 http 与 https。被拒绝的几类及其原因：
//   - file:// 能读本机任意文件，且读到的内容会渲染在页面里
//   - javascript: 与 data: 能在页面上下文执行代码
//   - chrome:// 与 chrome-extension:// 能改浏览器自身设置、访问扩展页面
//   - 自定义 scheme 会被浏览器再次转交给别的应用，形成二次跳转
//
// 返回清洗后的 URL 而非原串：解析再重新序列化能消掉畸形输入（如
// "http:/example.com" 这种少一个斜杠的形式），避免把原样字符串交给内核。
func ValidateURL(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", fmt.Errorf("URL 为空")
	}

	// 命令行参数不该含控制字符。出现就说明输入被构造过，直接拒绝——
	// 换行符尤其危险，某些解析路径会把它当成参数分隔。
	if i := strings.IndexFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f }); i >= 0 {
		return "", fmt.Errorf("URL 含控制字符（位置 %d），拒绝打开", i)
	}

	u, err := url.Parse(s)
	if err != nil {
		return "", fmt.Errorf("URL 格式无效: %w", err)
	}

	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	case "":
		return "", fmt.Errorf("URL %q 缺少 http:// 或 https:// 前缀，拒绝打开", s)
	default:
		return "", fmt.Errorf(
			"只接受 http 与 https 链接，拒绝打开 %s: 链接", u.Scheme)
	}

	// 有 scheme 却没有主机名的形式（如 "http://"）交给浏览器只会得到错误页。
	if u.Host == "" {
		return "", fmt.Errorf("URL %q 缺少主机名，拒绝打开", s)
	}

	// 以 - 开头的主机名会让内核把整个 URL 当成命令行开关。
	// launcher 把 URL 放在参数末尾正是为了避免这类混淆，这里再挡一道。
	if strings.HasPrefix(u.Host, "-") {
		return "", fmt.Errorf("URL 主机名 %q 以 - 开头，会被内核当成命令行开关", u.Host)
	}
	return u.String(), nil
}
