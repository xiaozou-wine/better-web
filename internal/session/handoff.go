package session

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"better-web/internal/launcher"
	"better-web/internal/model"
)

// handoffTimeout 是等待递送进程退出的上限。
//
// 实测递送约 1 秒返回（Chromium 打印 "Opening in existing browser session."
// 后立即退出）。给 15 秒余量覆盖机器繁忙的情况；超时则杀掉它并报错，
// 因为再等下去也不会有结果——那说明 singleton 握手没成功。
const handoffTimeout = 15 * time.Second

// Handoff 把 URL 递给该 profile 已运行的浏览器实例。
//
// 机制：用同一个 --user-data-dir 再启动一次内核，Chromium 自己的
// process singleton 发现已有实例后把命令行转交过去，本进程立即退出。
// 已在内核 148 上实测确认 --new-window 与 --incognito 都被接受。
//
// 不加跨进程锁：递送方不打开 user-data-dir，不存在两个进程同时写的风险。
// 也不起代理转发器与出口探测——那些是已运行实例在自己启动时定好的，
// 递送改不了，做了只是白费时间。
//
// 调用方必须先确认目标确实在运行（见 HeldByOtherProcess）。对没有实例的
// profile 调用它会让 Chromium 起一个**不带代理和指纹**的新实例，
// 那正是 fail-closed 要防的情形。
func (m *Manager) Handoff(ctx context.Context, p *model.Profile, opt *launcher.Options) error {
	if p == nil {
		return errors.New("profile 为空")
	}
	// 与启动路径同一道闸：指纹 profile 绝不能落到不支持伪造的内核上。
	// 递送时虽然不传指纹参数，但万一目标实例已经退出，Chromium 会用这条
	// 命令行起一个新实例——那时内核选错就是真的跑在错误环境上。
	if err := p.ValidateBrowserChoice(); err != nil {
		return err
	}

	k, err := m.resolveBrowser(p)
	if err != nil {
		return err
	}
	if p.Kind == model.KindFingerprint && !k.Source.SupportsFingerprint() {
		return fmt.Errorf(
			"内核 %s 不支持指纹伪造，拒绝递送: %w",
			k.ExecPath, model.ErrSystemBrowserWithFingerprint)
	}

	args, err := launcher.HandoffArgs(p, opt)
	if err != nil {
		return err
	}

	cctx, cancel := context.WithTimeout(ctx, handoffTimeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, k.ExecPath, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if cctx.Err() != nil {
			return fmt.Errorf("递送链接超时（%s），未能确认已交给运行中的实例", handoffTimeout)
		}
		// 带上内核输出：递送失败时它通常已说明原因（如目录被别的浏览器占用）。
		return fmt.Errorf("递送链接失败: %w%s", err, formatOutput(out))
	}
	return nil
}

// formatOutput 把内核输出整理成可附在错误信息后的一行。
//
// 截断而非全量附上：内核偶尔会刷出大段 GPU 或 DBus 警告，
// 全部塞进错误信息会把真正的原因顶出可见范围。
func formatOutput(b []byte) string {
	s := strings.TrimSpace(string(b))
	if s == "" {
		return ""
	}
	const max = 200
	if len(s) > max {
		s = s[:max] + "…"
	}
	return "（内核输出: " + s + "）"
}
