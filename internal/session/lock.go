package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ErrLockedByOtherProcess 表示该 profile 已被另一个进程启动。
//
// Manager 自身的会话表只能防住同进程内的重复启动。跨进程（界面与命令行工具
// 同时运行）时两边各有一张表，都会放行——而两个 Chromium 进程打开同一个
// user-data-dir 会损坏 profile 数据，Chromium 自己的处理是让后来者静默退出，
// 用户看不到任何原因。
var ErrLockedByOtherProcess = errors.New("该 profile 已被其他进程启动")

// lockFileName 是写在 profile 目录内的锁文件名。
//
// 放在 profile 目录内而非全局位置：锁保护的正是这个目录，两者同生同灭。
// 前缀用点号，避免与 Chromium 自己的文件混淆。
const lockFileName = ".better-web-lock"

// lockInfo 是锁文件的内容。记录 PID 是为了识别陈旧锁——
// 进程崩溃或被强杀时锁文件会留下，只看文件存在会让该 profile 永久锁死。
type lockInfo struct {
	PID       int       `json:"pid"`
	ProfileID string    `json:"profileId"`
	Since     time.Time `json:"since"`
}

// acquireLock 尝试为 profile 目录加锁，返回释放函数。
//
// 已被活着的进程持有时返回 ErrLockedByOtherProcess；持有者已不存在（陈旧锁）
// 则直接接管。接管而非报错是有意的：崩溃后留下的锁不该需要人工清理。
func acquireLock(profileDir, profileID string) (release func(), err error) {
	path := filepath.Join(profileDir, lockFileName)

	if holder, stale := readLock(path); holder != nil && !stale {
		return nil, fmt.Errorf("%w（PID %d，自 %s 起）",
			ErrLockedByOtherProcess, holder.PID, holder.Since.Format(time.DateTime))
	}

	b, err := json.Marshal(lockInfo{
		PID: os.Getpid(), ProfileID: profileID, Since: time.Now(),
	})
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return nil, fmt.Errorf("写入锁文件失败: %w", err)
	}

	return func() {
		// 只删自己写的锁。会话结束时若锁已被别人接管（本进程曾长时间挂起），
		// 删掉它会让对方失去保护。
		if holder, _ := readLock(path); holder == nil || holder.PID == os.Getpid() {
			_ = os.Remove(path)
		}
	}, nil
}

// HeldByOtherProcess 报告该 profile 目录是否正被另一个活着的进程持有。
//
// 供链接接管路径判断"该开新实例还是把 URL 递给已运行的实例"。它只读锁文件、
// 不加锁，因此不影响持有者，也不会留下需要清理的状态。
//
// 与 acquireLock 的分工：那个是"我要用，占住它"，这个是"别人在用吗"。
// 本进程持有的锁同样算"被占用"——递送 URL 的路径不关心持有者是谁，
// 它只需要知道有没有一个活着的浏览器可以接收。
//
// 判断有竞态：返回后持有者可能立刻退出。这在本用途下无害——递送失败时
// Chromium 会自己起一个新实例，而那正是我们本来想要的结果。
func HeldByOtherProcess(profileDir string) bool {
	holder, stale := readLock(filepath.Join(profileDir, lockFileName))
	return holder != nil && !stale
}

// readLock 读取锁文件，返回持有者信息与它是否已陈旧。
//
// 锁文件不存在、无法解析或持有者进程已退出时，stale 为 true——
// 这三种情况都应允许接管：解析失败的锁文件没有任何保护价值，
// 留着它只会让 profile 永久不可用。
func readLock(path string) (info *lockInfo, stale bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, true
	}
	var li lockInfo
	if err := json.Unmarshal(b, &li); err != nil || li.PID <= 0 {
		return nil, true
	}
	// 本进程持有的锁同样算有效，不能因为 PID 相同就放行。
	//
	// 一个进程内可以有多个 Manager（界面与内嵌工具各建一个），它们的会话表
	// 互不可见，都会放行重复启动。此时只有锁文件能拦住——若在这里因
	// "PID 是自己"而判为陈旧，两个 Manager 就会同时打开一个 user-data-dir。
	// 有测试钉住这一点，见 TestTwoManagersCannotStartSameProfile。
	if li.PID == os.Getpid() {
		return &li, false
	}
	return &li, !processAlive(li.PID)
}
