package session

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"better-web/internal/kernel"
	"better-web/internal/model"
)

// 指纹模式配了系统浏览器时，启动必须失败。
//
// 这是整条链路最关键的一道闸。官方 Chrome 不认识 --fingerprint，会静默
// 忽略全部伪造——页面照常打开、不报错，但报出的是宿主机真实指纹。
// 若这道闸失守，用户不会有任何察觉，直到账号被关联。
//
// 注意断言的是「启动前就拒绝」：不能先把浏览器起起来再发现问题，
// 那时进程已经带着真实指纹连上目标站点了。
func TestStartRejectsFingerprintWithSystemBrowser(t *testing.T) {
	m := NewManager(kernel.NewStore(t.TempDir()))
	p := &model.Profile{
		ID: "fp-1", Name: "指纹", Kind: model.KindFingerprint, Seed: 12345,
		ProfileDir:       filepath.Join(t.TempDir(), "fp-1"),
		UseSystemBrowser: true,
	}

	st, err := m.Start(context.Background(), p)
	if err == nil {
		t.Fatal("必须拒绝启动")
	}
	if !errors.Is(err, model.ErrSystemBrowserWithFingerprint) {
		t.Errorf("应返回 ErrSystemBrowserWithFingerprint，实际 %v", err)
	}
	if st.State != StateFailed {
		t.Errorf("状态应为 %s，实际 %s", StateFailed, st.State)
	}

	// 拒绝后不得留下会话记录，否则该 profile 会被当成正在运行。
	if len(m.Running()) != 0 {
		t.Errorf("拒绝后不应留下会话，实际 %d 个", len(m.Running()))
	}
}

// 这道闸必须先于内核解析生效。
//
// 内核目录是空的，若校验发生在解析之后，报的会是「未找到内核」——
// 那条错误信息会把用户引向"去装内核"，而真正的问题是配置组合非法。
func TestStartValidatesBeforeResolvingKernel(t *testing.T) {
	m := NewManager(kernel.NewStore(t.TempDir()))
	p := &model.Profile{
		ID: "fp-2", Name: "指纹", Kind: model.KindFingerprint, Seed: 1,
		ProfileDir:       filepath.Join(t.TempDir(), "fp-2"),
		UseSystemBrowser: true,
	}

	_, err := m.Start(context.Background(), p)
	if err == nil {
		t.Fatal("应报错")
	}
	if errors.Is(err, kernel.ErrNotFound) {
		t.Error("报的是「未找到内核」，说明校验发生在解析之后，" +
			"错误信息会把用户引向错误方向")
	}
}

// resolveBrowser 的分派：日常 + UseSystemBrowser 走系统 Chrome，
// 其余走指纹内核。
func TestResolveBrowserDispatch(t *testing.T) {
	m := NewManager(kernel.NewStore(t.TempDir()))

	// 日常 + 系统浏览器：应尝试系统 Chrome。本机装了就返回它，
	// 没装则报错——两种结果都不该是「未找到指纹内核」。
	daily := &model.Profile{
		ID: "d", Kind: model.KindDaily, UseSystemBrowser: true,
	}
	k, err := m.resolveBrowser(daily)
	if err != nil {
		t.Logf("本机未装 Chrome: %v", err)
	} else {
		if k.Source != kernel.SourceSystemChrome {
			t.Errorf("应解析到系统 Chrome，实际 Source=%q", k.Source)
		}
		t.Logf("解析到系统 Chrome: %s", k.ExecPath)
	}

	// 日常但不开系统浏览器：走指纹内核，此处内核目录为空故报 ErrNotFound。
	plain := &model.Profile{ID: "p", Kind: model.KindDaily}
	if _, err := m.resolveBrowser(plain); !errors.Is(err, kernel.ErrNotFound) {
		t.Errorf("不开系统浏览器时应查指纹内核，实际 %v", err)
	}
}

// 系统 Chrome 找不到时必须报错，不能静默回退到指纹内核。
//
// 用户明确选了系统 Chrome 是为了 Google 账号、同步与自动更新。静默换成
// 指纹内核后这些能力全没有，而界面显示的仍是「系统 Chrome」，
// 他会以为登不上账号是别的原因。
func TestResolveBrowserDoesNotFallBackSilently(t *testing.T) {
	m := NewManager(kernel.NewStore(t.TempDir()))
	daily := &model.Profile{
		ID: "d", Kind: model.KindDaily, UseSystemBrowser: true,
	}
	k, err := m.resolveBrowser(daily)
	if err != nil {
		// 未装 Chrome：错误信息必须指向系统 Chrome，而非指纹内核。
		if !errors.Is(err, kernel.ErrNotFound) {
			t.Errorf("应包装 ErrNotFound，实际 %v", err)
		}
		return
	}
	// 装了 Chrome：绝不能返回指纹内核。
	if k.Source == kernel.SourceFingerprint {
		t.Error("配了系统 Chrome 却回退到指纹内核，这是静默降级")
	}
}
