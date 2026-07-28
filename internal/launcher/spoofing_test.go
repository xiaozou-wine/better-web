package launcher

import (
	"strings"
	"testing"

	"better-web/internal/model"
)

func fpProfile(name string, targets ...model.SpoofTarget) *model.Profile {
	return &model.Profile{
		Name: name, Kind: model.KindFingerprint,
		ProfileDir: `C:\p\` + name, Seed: 1234,
		DisableSpoofing: targets,
	}
}

// 不指定排障开关时不得出现该参数：传空值可能被内核解读为关闭全部伪造。
func TestNoDisableSpoofingFlagByDefault(t *testing.T) {
	args, err := BuildArgs(fpProfile("clean"), testFingerprint(), "", nil)
	if err != nil {
		t.Fatalf("BuildArgs 失败: %v", err)
	}
	if hasFlag(args, flagDisableSpoofing) {
		t.Errorf("未指定排障开关却出现了 %s: %v", flagDisableSpoofing, args)
	}
}

func TestDisableSpoofingJoinsTargets(t *testing.T) {
	p := fpProfile("debug", model.SpoofGPU, model.SpoofFont)
	args, err := BuildArgs(p, testFingerprint(), "", nil)
	if err != nil {
		t.Fatalf("BuildArgs 失败: %v", err)
	}
	// 顺序应保持用户给定的顺序，便于用户核对自己关了什么。
	if got := flagValue(t, args, flagDisableSpoofing); got != "gpu,font" {
		t.Errorf("%s = %q, 期望 gpu,font", flagDisableSpoofing, got)
	}
}

// 全部子系统都应能关闭，且值必须是内核认识的名字。
func TestDisableSpoofingAcceptsAllKnownTargets(t *testing.T) {
	all := model.SpoofTargets()
	p := fpProfile("all", all...)
	args, err := BuildArgs(p, testFingerprint(), "", nil)
	if err != nil {
		t.Fatalf("BuildArgs 失败: %v", err)
	}
	got := flagValue(t, args, flagDisableSpoofing)
	parts := strings.Split(got, ",")
	if len(parts) != len(all) {
		t.Fatalf("%s = %q, 期望 %d 项", flagDisableSpoofing, got, len(all))
	}
	for _, part := range parts {
		if !model.SpoofTarget(part).Valid() {
			t.Errorf("值 %q 不是内核已知的子系统名", part)
		}
	}
}

// 重复项必须去掉：gpu,gpu 这类输入内核行为未定义。
func TestDisableSpoofingDeduplicates(t *testing.T) {
	p := fpProfile("dup", model.SpoofGPU, model.SpoofCanvas, model.SpoofGPU)
	args, err := BuildArgs(p, testFingerprint(), "", nil)
	if err != nil {
		t.Fatalf("BuildArgs 失败: %v", err)
	}
	if got := flagValue(t, args, flagDisableSpoofing); got != "gpu,canvas" {
		t.Errorf("%s = %q, 期望去重后的 gpu,canvas", flagDisableSpoofing, got)
	}
}

// 未知子系统名必须报错而非静默丢弃：用户以为关掉了某项而实际没关，
// 会把排障引向完全错误的结论。
func TestDisableSpoofingRejectsUnknownTarget(t *testing.T) {
	p := fpProfile("bad", model.SpoofTarget("webrtc"))
	_, err := BuildArgs(p, testFingerprint(), "", nil)
	if err == nil {
		t.Fatal("未知子系统名期望报错，实际通过")
	}
	// 错误信息应列出可选值，否则用户不知道该填什么。
	if !strings.Contains(err.Error(), "canvas") {
		t.Errorf("错误信息未列出可选值: %v", err)
	}
}

// 日常模式不做任何伪造，也就没有可关闭的伪造项。
// 误传该参数说明配置串了，但不应影响日常模式"绝不注入指纹参数"的保证。
func TestDailyProfileIgnoresDisableSpoofing(t *testing.T) {
	p := &model.Profile{
		Name: "日常", Kind: model.KindDaily, ProfileDir: `C:\p\daily`,
		DisableSpoofing: []model.SpoofTarget{model.SpoofGPU},
	}
	args, err := BuildArgs(p, testFingerprint(), "", nil)
	if err != nil {
		t.Fatalf("BuildArgs 失败: %v", err)
	}
	if hasFlag(args, flagDisableSpoofing) {
		t.Errorf("日常模式不应包含 %s: %v", flagDisableSpoofing, args)
	}
}

// 关闭伪造子系统不得影响 WebRTC 防泄漏：
// --disable-spoofing 只关伪造，代理与 UDP 策略是另一回事，两者不应互相干扰。
func TestDisableSpoofingKeepsWebRTCProtection(t *testing.T) {
	p := fpProfile("debug-proxied", model.SpoofGPU, model.SpoofCanvas)
	args, err := BuildArgs(p, testFingerprint(), "socks5://127.0.0.1:41234", nil)
	if err != nil {
		t.Fatalf("BuildArgs 失败: %v", err)
	}
	if !hasFlag(args, flagDisableNonProxiedUDP) {
		t.Errorf("关闭伪造后丢了 %s，WebRTC 会经 STUN 泄漏真实 IP: %v",
			flagDisableNonProxiedUDP, args)
	}
	if got := flagValue(t, args, flagDisableSpoofing); got != "gpu,canvas" {
		t.Errorf("%s = %q, 期望 gpu,canvas", flagDisableSpoofing, got)
	}
}
