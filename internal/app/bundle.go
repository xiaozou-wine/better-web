package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"better-web/internal/fingerprint"
	"better-web/internal/model"
	"better-web/internal/transfer"

	"github.com/google/uuid"
)

// 本文件是配置文件形式的整体导出与导入，与 import.go 的职责不同：
//   - import.go 的 ImportProxies：粘贴多行代理文本批量建号，从零开始
//   - 本文件：把现有 profile 配置导出成文件，再在别处还原
//
// 两者都叫"导入"但语义相反，故用 Bundle 前缀区分。

// ExportBundle 把全部 profile 配置导出到指定路径，返回导出条数。
//
// withSecrets 为 true 时包含代理密码明文。默认不含：导出文件常被复制、
// 通过聊天工具发送、甚至提交到仓库，凭据进入这类文件的风险远高于
// 导入后重填一次密码的成本。
//
// 只导出配置，不含 user-data-dir 里的 Cookie 与登录态——那是 Chromium 的
// 私有 SQLite 格式，几十到几百 MB，且两台机器同时使用会冲突。
func (s *Service) ExportBundle(path string, withSecrets bool) (int, error) {
	list, err := s.store.List()
	if err != nil {
		return 0, err
	}
	if len(list) == 0 {
		return 0, errors.New("没有可导出的 profile")
	}

	// 0o600 而非 0o644：即使不含密码，文件里也有代理地址与账号名。
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, fmt.Errorf("创建导出文件失败: %w", err)
	}
	defer func() { _ = f.Close() }()

	if err := transfer.Export(f, list, len(fingerprint.Catalog()), withSecrets,
		s.hostGPUBestEffort()); err != nil {
		return 0, err
	}
	return len(list), nil
}

// hostGPUBestEffort 探测本机 GPU 厂商族，失败时返回空而不报错。
//
// 尽力而为的理由：这个值只用于导入时的跨机器警告。探测要启动一次浏览器，
// 未装内核或探测失败时若让整个导出/导入失败，就是为了一条提示牺牲主功能。
// 返回空值时导入侧不产生警告，退化成本功能加入之前的行为。
func (s *Service) hostGPUBestEffort() model.GPUFamily {
	// 超时给得比单次探测的 45 秒短：导出是交互操作，用户在等。
	// 探不到就算了，不值得让他多等。
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	info, err := s.DetectHostGPU(ctx, "")
	if err != nil {
		return model.GPUFamilyUnknown
	}
	return info.Family
}

// BundleImportOptions 是配置文件导入的选项。
type BundleImportOptions struct {
	// NewSeeds 决定种子语义。没有安全的默认值，必须由用户明确选择：
	//   - false：备份恢复或迁移到新机器。保留原种子，否则还原出来的是另一台设备
	//   - true：以某份配置为模板批量建号。必须换种子，否则所有 profile
	//     共用同一套 canvas 指纹，平台侧可直接关联
	NewSeeds bool `json:"newSeeds"`
	// NamePrefix 非空时给导入的名称加前缀，用于区分批次或规避重名。
	NamePrefix string `json:"namePrefix,omitempty"`
	// Group 非空时把整批归入该分组，覆盖文件里的值。
	Group string `json:"group,omitempty"`
}

// BundleImportResult 是一次配置文件导入的结果，供界面逐条呈现。
type BundleImportResult struct {
	// Imported 是成功写入的数量。
	Imported int `json:"imported"`
	// SkippedNames 是因与既有 profile 重名而跳过的名称。
	SkippedNames []string `json:"skippedNames,omitempty"`
	// Failures 是校验或写入失败的条目。
	Failures []BundleImportFailure `json:"failures,omitempty"`
	// Warnings 是不阻断导入但需用户知晓的问题，如种子重复、档案库条目数不同。
	Warnings []string `json:"warnings,omitempty"`
}

// BundleImportFailure 是单条导入失败的记录。
type BundleImportFailure struct {
	// Index 是该条目在文件中的位置，从 1 开始，便于对照文件定位。
	Index int    `json:"index"`
	Name  string `json:"name"`
	Err   string `json:"err"`
}

// ImportBundle 从配置文件导入 profile。
//
// 先整体校验再逐条落库：校验阶段的失败不留下半写入状态。落库阶段仍可能
// 单条失败（如唯一索引冲突），那些记入 Failures 而已成功的部分保留——
// 整批回滚会让用户丢掉已经导入好的几十条，比部分成功更糟。
func (s *Service) ImportBundle(path string, opt BundleImportOptions) (BundleImportResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return BundleImportResult{}, fmt.Errorf("打开导入文件失败: %w", err)
	}
	defer func() { _ = f.Close() }()

	bundle, err := transfer.Parse(f)
	if err != nil {
		return BundleImportResult{}, err
	}

	// 收集既有名称用于跳过重名——名称在库中有唯一索引，撞了必然写入失败。
	existing, err := s.store.List()
	if err != nil {
		return BundleImportResult{}, err
	}
	taken := make(map[string]bool, len(existing))
	for _, p := range existing {
		taken[p.Name] = true
	}

	// 只在保留原种子且文件记录了导出机器 GPU 时才探测：其余情况这个值
	// 用不上，白花一次浏览器冷启动。
	var hostGPU model.GPUFamily
	if !opt.NewSeeds && bundle.HostGPUFamily.Known() {
		hostGPU = s.hostGPUBestEffort()
	}

	prep := transfer.Prepare(bundle, transfer.Options{
		NewSeeds:          opt.NewSeeds,
		NamePrefix:        opt.NamePrefix,
		Group:             opt.Group,
		SkipExistingNames: taken,
		HostGPUFamily:     hostGPU,
	}, len(fingerprint.Catalog()), uuid.NewString, fingerprint.NewSeed, s.paths.ProfileDir)

	out := BundleImportResult{
		SkippedNames: prep.Skipped,
		Warnings:     prep.Warnings,
	}
	for _, fail := range prep.Failed {
		out.Failures = append(out.Failures, BundleImportFailure{
			Index: fail.Index, Name: fail.Name, Err: fail.Err})
	}

	for i, p := range prep.Prepared {
		if err := s.store.Save(p); err != nil {
			out.Failures = append(out.Failures, BundleImportFailure{
				Index: i + 1, Name: p.Name, Err: "写入失败: " + err.Error()})
			continue
		}
		out.Imported++
	}
	return out, nil
}
