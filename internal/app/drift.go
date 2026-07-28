package app

import (
	"fmt"
	"strconv"
	"strings"
)

// ErrKernelDrift 表示改动会导致已用过的 profile 指纹漂移。
//
// 反检测的核心不是"伪造得多完美"，而是"同一 profile 每次看起来都是同一台机器"。
// 平台封号的常见依据不是"这个指纹是假的"，而是"这个账号上次是 A 设备现在是 B 设备"。
// 内核大版本之间指纹算法会变（如 148 重做了 audio 与 canvas 实现），同一 seed
// 在不同大版本上推导出的环境并不相同，因此换内核等价于给账号换了台机器。
type ErrKernelDrift struct {
	From string
	To   string
}

func (e *ErrKernelDrift) Error() string {
	return fmt.Sprintf(
		"该 profile 已使用过内核 %s，切换到 %s 会改变其指纹（相当于账号换了设备）。"+
			"确认要切换请设置 ConfirmKernelChange", e.From, e.To)
}

// ErrDeviceDrift 表示换机型会导致已用过的 profile 设备特征漂移。
//
// 与 ErrKernelDrift 同理：机型决定 OS、核数、GPU 与屏幕等一整组特征，
// 换掉它等同于账号从 A 设备换到了 B 设备。
type ErrDeviceDrift struct {
	From string
	To   string
}

func (e *ErrDeviceDrift) Error() string {
	from, to := e.From, e.To
	if from == "" {
		from = "按种子抽取"
	}
	if to == "" {
		to = "按种子抽取"
	}
	return fmt.Sprintf(
		"该 profile 已使用过机型「%s」，改为「%s」会改变其设备特征"+
			"（相当于账号换了设备）。确认要切换请设置 ConfirmDeviceChange", from, to)
}

// majorOf 取版本号的主版本部分。解析失败返回 0，调用方需视为未知。
func majorOf(version string) int {
	major, _, _ := strings.Cut(version, ".")
	n, err := strconv.Atoi(strings.TrimSpace(major))
	if err != nil {
		return 0
	}
	return n
}

// crossesMajor 报告两个版本号是否跨越了主版本。
//
// 只拦大版本变化：同一大版本内的小版本更新（安全补丁）不改指纹算法，
// 拦下来只会让用户无法打补丁，反而有害。任一版本解析失败时保守地判为
// 未跨越——宁可漏一次提醒，也不要用一个解析 bug 把用户锁死。
func crossesMajor(from, to string) bool {
	if from == "" || to == "" {
		// 空值表示"跟随默认内核"，本身就没有锁定，不构成显式切换。
		return false
	}
	if from == to {
		return false
	}
	a, b := majorOf(from), majorOf(to)
	if a == 0 || b == 0 {
		return false
	}
	return a != b
}
