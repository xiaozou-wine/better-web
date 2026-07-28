package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"better-web/internal/model"
)

// ImportRequest 是批量导入代理并建 profile 的入参。
type ImportRequest struct {
	// Text 是多行代理配置，格式见 model.ParseProxy。
	Text string `json:"text"`
	// NamePrefix 是生成的 profile 名称前缀，留空时用「导入」。
	// 实际名称形如「<前缀>-01」，序号按导入顺序递增。
	NamePrefix string `json:"namePrefix,omitempty"`
	// Group 是导入后归入的分组，留空表示不分组。
	Group string `json:"group,omitempty"`
	// Tags 是给全部导入项打上的标签。
	Tags []string `json:"tags,omitempty"`
	// Kind 是 profile 类型，留空时按指纹模式创建——导入代理的场景
	// 几乎都是为了多账号，日常模式用不着批量。
	Kind model.ProfileKind `json:"kind,omitempty"`
	// KernelVersion 锁定内核版本，留空表示跟随最新。
	KernelVersion string `json:"kernelVersion,omitempty"`
}

// ImportResult 是一次批量导入的结果。
type ImportResult struct {
	// Created 是成功创建的 profile 视图。
	Created []ProfileView `json:"created"`
	// ParseFailed 是解析失败的行，密码已脱敏。
	ParseFailed []model.ProxyParseError `json:"parseFailed,omitempty"`
	// CreateFailed 是解析成功但创建失败的项（多为名称冲突）。
	CreateFailed []BatchResult `json:"createFailed,omitempty"`
}

// ImportProxies 解析多行代理并逐个创建 profile。
//
// 每个 profile 拿到独立的随机种子——这是必须的：多账号场景下若共用种子，
// canvas 指纹完全一致，平台一眼就能把这批账号关联起来。
//
// 解析失败与创建失败分开返回，且都不中断其余项：粘贴一百行时因为
// 第三行格式错误就整批放弃，用户得逐行排查。
// 批量导入不支持按宿主机 GPU 筛选种子：筛一个种子最多要冷启动内核 24 次
// （约 50 秒），导入 20 条就是十几分钟。而"筛一次给所有 profile 复用"是
// 不能接受的——相同种子等于同一台设备，canvas 哈希完全一致，一眼被关联，
// 恰好违背导入功能给每个 profile 独立种子的初衷。
//
// 需要匹配 GPU 的 profile 请在界面上逐个新建。
func (s *Service) ImportProxies(ctx context.Context, req ImportRequest) (ImportResult, error) {
	if strings.TrimSpace(req.Text) == "" {
		return ImportResult{}, errors.New("导入内容为空")
	}
	kind := req.Kind
	if kind == "" {
		kind = model.KindFingerprint
	}
	if !kind.Valid() {
		return ImportResult{}, fmt.Errorf("profile 类型 %q 无效", kind)
	}

	proxies, parseFailed := model.ParseProxyList(req.Text)
	if len(proxies) == 0 {
		return ImportResult{ParseFailed: parseFailed},
			errors.New("没有可导入的有效代理配置")
	}

	prefix := strings.TrimSpace(req.NamePrefix)
	if prefix == "" {
		prefix = "导入"
	}

	out := ImportResult{ParseFailed: parseFailed}
	for i, p := range proxies {
		// 序号从 1 起、补零到两位，保证界面按名称排序时顺序正确。
		name := fmt.Sprintf("%s-%02d", prefix, i+1)
		view, err := s.CreateProfile(ctx, CreateRequest{
			Name: name, Kind: kind, Proxy: p,
			Group: req.Group, Tags: req.Tags,
			KernelVersion: req.KernelVersion,
		})
		if err != nil {
			out.CreateFailed = append(out.CreateFailed, BatchResult{
				Name: name, OK: false, Err: err.Error(),
			})
			continue
		}
		out.Created = append(out.Created, view)
	}
	return out, nil
}
