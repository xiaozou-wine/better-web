package model

import (
	"errors"
	"strings"
	"testing"
)

// 指纹模式 + 系统浏览器必须被拒绝。
//
// 这是本功能最重要的一条约束。官方 Chrome 不认识 --fingerprint，会把它
// 当成未知参数忽略，于是全部伪造静默失效——页面照常打开、脚本照常执行、
// 不报任何错，只是报出的是宿主机真实指纹、真实 GPU、真实时区。
//
// 这类故障比代理失效更隐蔽：代理挂了至少能从出口 IP 看出来，内核用错
// 什么痕迹都没有，等发现时账号可能已经被关联。所以必须 fail-closed。
func TestValidateBrowserChoiceRejectsFingerprintWithSystemBrowser(t *testing.T) {
	p := &Profile{
		Name: "指纹", Kind: KindFingerprint, UseSystemBrowser: true,
	}
	err := p.ValidateBrowserChoice()
	if err == nil {
		t.Fatal("指纹模式用系统浏览器必须被拒绝")
	}
	if !errors.Is(err, ErrSystemBrowserWithFingerprint) {
		t.Errorf("应返回 ErrSystemBrowserWithFingerprint，实际 %v", err)
	}
}

// 日常模式 + 系统浏览器是本功能的目标组合，必须放行。
func TestValidateBrowserChoiceAllowsDailyWithSystemBrowser(t *testing.T) {
	p := &Profile{Name: "日常", Kind: KindDaily, UseSystemBrowser: true}
	if err := p.ValidateBrowserChoice(); err != nil {
		t.Errorf("日常模式用系统浏览器应放行，实际 %v", err)
	}
}

// 不开系统浏览器时两种类型都放行，保证既有行为不变。
func TestValidateBrowserChoiceAllowsFingerprintKernel(t *testing.T) {
	for _, kind := range []ProfileKind{KindFingerprint, KindDaily} {
		p := &Profile{Name: "x", Kind: kind}
		if err := p.ValidateBrowserChoice(); err != nil {
			t.Errorf("kind=%s 用指纹内核应放行，实际 %v", kind, err)
		}
	}
}

// nil 接收者返回错误而非 panic。
func TestValidateBrowserChoiceRejectsNil(t *testing.T) {
	var p *Profile
	if err := p.ValidateBrowserChoice(); err == nil {
		t.Error("nil profile 应报错")
	}
}

// 错误信息必须说清后果，而不只是说"不允许"。
//
// 用户看到"指纹模式不能用系统 Chrome"会想绕过去（比如改成日常模式再改回来），
// 只有让他知道后果是"伪造全部失效且不报错"，才会理解这不是形式限制。
func TestSystemBrowserErrorExplainsConsequence(t *testing.T) {
	msg := ErrSystemBrowserWithFingerprint.Error()
	for _, want := range []string{"--fingerprint", "静默", "真实指纹"} {
		if !strings.Contains(msg, want) {
			t.Errorf("错误信息应包含 %q，实际: %s", want, msg)
		}
	}
}
