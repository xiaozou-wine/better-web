package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"better-web/internal/launcher"
	"better-web/internal/model"
	"better-web/internal/session"
	"better-web/internal/store"
	"better-web/internal/urlhandler"
)

// URLHandlerView 是链接接管配置的完整状态，供界面一次性呈现。
type URLHandlerView struct {
	// Registered 表示已写入注册表、会出现在系统默认应用的候选列表里。
	Registered bool `json:"registered"`
	// IsDefault 表示用户已在系统设置里把 better-web 选为默认浏览器。
	//
	// 与 Registered 分开：注册是程序能做到的，设为默认只有用户能做——
	// 系统不允许应用自己抢。合成一个状态会让"已注册但未生效"这个最常见的
	// 中间态无法表达，而那正是用户需要看到提示的时候。
	IsDefault bool `json:"isDefault"`
	// Supported 为 false 表示当前平台没有实现注册，界面应隐藏相关按钮。
	Supported bool `json:"supported"`

	// ProfileID 是接管链接的目标 profile，空串表示尚未配置。
	ProfileID string `json:"profileId,omitempty"`
	// ProfileName 是该 profile 的名称，供界面显示。
	//
	// 一并返回而非让前端自己查：配置的 profile 可能已被删除，
	// 那时这里为空而 ProfileID 有值，界面据此提示需要重新选。
	ProfileName string `json:"profileName,omitempty"`
	// Incognito 为 true 时用无痕窗口打开接管的链接。
	Incognito bool `json:"incognito"`
}

// URLHandler 返回链接接管的当前配置与注册状态。
func (s *Service) URLHandler() (URLHandlerView, error) {
	st, err := urlhandler.Query()
	if err != nil {
		return URLHandlerView{}, err
	}

	v := URLHandlerView{
		Registered: st.Registered,
		IsDefault:  st.IsDefault,
		Supported:  urlhandler.Supported(),
	}

	if v.ProfileID, err = s.store.Setting(store.KeyURLHandlerProfileID); err != nil {
		return URLHandlerView{}, err
	}
	if v.Incognito, err = s.store.SettingBool(store.KeyURLHandlerIncognito); err != nil {
		return URLHandlerView{}, err
	}
	// profile 可能已被删除。此时留空 ProfileName，界面据此提示重新选择，
	// 而不是报错——配置指向一个不存在的 profile 是可恢复的状态。
	if v.ProfileID != "" {
		if p, err := s.store.Get(v.ProfileID); err == nil {
			v.ProfileName = p.Name
		}
	}
	return v, nil
}

// SetURLHandler 设置接管链接的目标 profile 与无痕开关。
//
// profileID 为空串时清除配置，此时链接接管不再生效（已注册的身份仍在，
// 但点链接会报"未配置目标 profile"）。
func (s *Service) SetURLHandler(profileID string, incognito bool) error {
	if profileID != "" {
		p, err := s.store.Get(profileID)
		if err != nil {
			return fmt.Errorf("指定的 profile 不可用: %w", err)
		}
		// 指纹模式 + 无痕是要拒绝的组合，不是可以警告后放行的问题。
		//
		// 无痕不落盘，但指纹伪造与代理照旧生效：出口 IP 和指纹一个字没变，
		// 用户以为"无痕更干净"，实际只是丢掉了养号要留的 Cookie 与登录态。
		// 这种"以为得到了实际没得到"的误解，与 fail-closed 拦的是同一类问题。
		if incognito && p.Kind == model.KindFingerprint {
			return errors.New(
				"指纹模式不能用无痕窗口：无痕只是不落盘，指纹伪造与代理照旧生效，" +
					"出口 IP 与指纹都不会变，反而丢掉了养号需要的登录态")
		}
	}

	if err := s.store.SetSetting(store.KeyURLHandlerProfileID, profileID); err != nil {
		return err
	}
	return s.store.SetSettingBool(store.KeyURLHandlerIncognito, incognito)
}

// RegisterURLHandler 把 better-web 注册成系统的候选默认浏览器。
//
// 注册成功不代表已生效：系统不允许应用自己抢默认浏览器，用户还需在
// "设置 → 默认应用"里手动选。界面文案必须说清这点，否则用户点完就以为好了。
func (s *Service) RegisterURLHandler() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("定位当前程序路径失败: %w", err)
	}
	return urlhandler.Register(exe)
}

// UnregisterURLHandler 清除注册信息。
func (s *Service) UnregisterURLHandler() error { return urlhandler.Unregister() }

// OpenDefaultAppsSettings 打开系统的默认应用设置页。
//
// 需要它是因为注册的最后一步只能由用户手动完成，而"设置 → 应用 → 默认应用"
// 藏得深。直接跳到那一页能省掉一段说不清的路径描述。
func (s *Service) OpenDefaultAppsSettings() error { return urlhandler.OpenSettings() }

// OpenURL 用配置好的 profile 打开一个链接。
//
// 这是链接接管的核心路径，行为对齐"没有 better-web 时点链接"的体验：
//   - 目标 profile 未运行 → 完整启动它（代理、出口探测、指纹全部照旧），打开该 URL
//   - 目标 profile 已运行 → 在已有实例里新开窗口，本调用立即返回
//
// 返回的 started 表示是否新起了一个会话。为 true 时调用方必须阻塞到浏览器
// 退出——代理转发器跑在本进程内，进程一退出转发器就没了，浏览器随即失去
// 代理，那正是 fail-closed 要防的情形。为 false 时说明只是递送，可以直接退出。
//
// rawURL 必须已经过 urlhandler.ValidateURL 校验。这里再校验一次而非信任
// 调用方：这个入口的输入来自机器上任意应用，多一道白名单不亏。
func (s *Service) OpenURL(ctx context.Context, rawURL string) (started bool, id string, err error) {
	target, err := urlhandler.ValidateURL(rawURL)
	if err != nil {
		return false, "", err
	}

	profileID, err := s.store.Setting(store.KeyURLHandlerProfileID)
	if err != nil {
		return false, "", err
	}
	if profileID == "" {
		return false, "", errors.New(
			"尚未指定接管链接的 profile，请在 better-web 的设置里选一个")
	}
	p, err := s.store.Get(profileID)
	if err != nil {
		return false, "", fmt.Errorf(
			"配置的 profile 不可用（可能已被删除），请重新指定: %w", err)
	}
	incognito, err := s.store.SettingBool(store.KeyURLHandlerIncognito)
	if err != nil {
		return false, "", err
	}
	opt := &launcher.Options{URLs: []string{target}, Incognito: incognito}

	// 已有实例在跑时递送过去。判断用跨进程锁而非本进程的会话表：
	// 这个调用来自一个刚起来的短命进程，它的会话表必然是空的。
	if session.HeldByOtherProcess(p.ProfileDir) {
		if err := s.sessions.Handoff(ctx, p, opt); err != nil {
			return false, p.ID, err
		}
		return false, p.ID, nil
	}

	if _, err := s.sessions.StartWith(ctx, p, opt); err != nil {
		return false, p.ID, err
	}
	// 使用时间只用于列表排序，写失败不影响会话，与 Service.Start 同样处理。
	_ = s.store.TouchLastUse(p.ID, time.Now())
	return true, p.ID, nil
}
