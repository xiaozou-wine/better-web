package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"better-web/internal/session"
)

// DefaultBatchConcurrency 是批量启动的默认并发上限。
//
// 不放开到无限并发：每个 Chromium 实例连带渲染、GPU、网络子进程约占
// 300-500MB，同时冷启动十几个会把内存和 CPU 打满，反而全都变慢；
// 每个实例还各自要建代理连接，齐发容易撞上本机动态端口的瞬时压力。
// 4 是在"够快"和"不拖垮机器"之间的折中，可由调用方覆盖。
const DefaultBatchConcurrency = 4

// BatchResult 是批量操作中单个 profile 的结果。
type BatchResult struct {
	ProfileID string `json:"profileId"`
	// Name 便于界面直接展示，不用再回查。
	Name string `json:"name"`
	// OK 为 false 时 Err 说明原因。单个失败不影响其余。
	OK  bool   `json:"ok"`
	Err string `json:"err,omitempty"`
	// Status 仅在启动成功时有值。
	Status *session.Status `json:"status,omitempty"`
}

// BatchSummary 汇总一次批量操作。
type BatchSummary struct {
	Total     int           `json:"total"`
	Succeeded int           `json:"succeeded"`
	Failed    int           `json:"failed"`
	Results   []BatchResult `json:"results"`
}

// StartBatch 并发启动多个 profile。
//
// 单个失败不中断其余：批量启动二十个时因为第三个代理挂了就全部放弃，
// 比部分成功更糟。全部结果都在返回值里，由界面呈现哪些成功哪些失败。
//
// concurrency <= 0 时用 DefaultBatchConcurrency。
func (s *Service) StartBatch(ctx context.Context, ids []string, concurrency int) (BatchSummary, error) {
	if len(ids) == 0 {
		return BatchSummary{}, errors.New("未指定要启动的 profile")
	}
	if concurrency <= 0 {
		concurrency = DefaultBatchConcurrency
	}
	if concurrency > len(ids) {
		concurrency = len(ids)
	}

	results := make([]BatchResult, len(ids))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i, id := range ids {
		wg.Add(1)
		go func(idx int, profileID string) {
			defer wg.Done()

			// 取信号量前先看 ctx：用户取消后不该再启动新实例。
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results[idx] = BatchResult{
					ProfileID: profileID, OK: false,
					Err: "已取消: " + ctx.Err().Error(),
				}
				return
			}
			defer func() { <-sem }()

			results[idx] = s.startOne(ctx, profileID)
		}(i, id)
	}
	wg.Wait()

	return summarize(results), nil
}

// startOne 启动单个 profile 并把结果规整成 BatchResult。
func (s *Service) startOne(ctx context.Context, id string) BatchResult {
	res := BatchResult{ProfileID: id}

	// 先取名称：失败信息里带上名称，用户才知道是哪个出了问题。
	if p, err := s.store.Get(id); err == nil {
		res.Name = p.Name
	}

	st, err := s.Start(ctx, id)
	if err != nil {
		res.Err = err.Error()
		return res
	}
	res.OK = true
	res.Status = &st
	return res
}

// StopBatch 停止多个 profile。
//
// 停止不设并发上限：它只是投递关闭消息，不消耗资源，
// 而串行停止二十个实例会让界面等上好几秒。
func (s *Service) StopBatch(ids []string) (BatchSummary, error) {
	if len(ids) == 0 {
		return BatchSummary{}, errors.New("未指定要停止的 profile")
	}

	results := make([]BatchResult, len(ids))
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		go func(idx int, profileID string) {
			defer wg.Done()
			res := BatchResult{ProfileID: profileID}
			if p, err := s.store.Get(profileID); err == nil {
				res.Name = p.Name
			}
			if err := s.Stop(profileID); err != nil {
				res.Err = err.Error()
			} else {
				res.OK = true
			}
			results[idx] = res
		}(i, id)
	}
	wg.Wait()

	return summarize(results), nil
}

// DeleteBatch 删除多个 profile 的配置记录。
//
// 与单个删除一致：只删配置，保留磁盘上的浏览数据。
// 运行中的 profile 会被拒绝，避免删掉正在用的配置。
func (s *Service) DeleteBatch(ids []string) (BatchSummary, error) {
	if len(ids) == 0 {
		return BatchSummary{}, errors.New("未指定要删除的 profile")
	}

	// 串行执行：删除是写操作，SQLite 单写入者，并发只会互相等锁。
	results := make([]BatchResult, 0, len(ids))
	for _, id := range ids {
		res := BatchResult{ProfileID: id}
		if p, err := s.store.Get(id); err == nil {
			res.Name = p.Name
		}
		if err := s.DeleteProfile(id); err != nil {
			res.Err = err.Error()
		} else {
			res.OK = true
		}
		results = append(results, res)
	}
	return summarize(results), nil
}

// AssignGroupBatch 批量设置分组。空串表示移出分组。
func (s *Service) AssignGroupBatch(ids []string, group string) (BatchSummary, error) {
	if len(ids) == 0 {
		return BatchSummary{}, errors.New("未指定要修改的 profile")
	}

	results := make([]BatchResult, 0, len(ids))
	for _, id := range ids {
		res := BatchResult{ProfileID: id}
		p, err := s.store.Get(id)
		if err != nil {
			res.Err = err.Error()
			results = append(results, res)
			continue
		}
		res.Name = p.Name
		p.Group = group
		if err := s.store.Save(p); err != nil {
			res.Err = err.Error()
		} else {
			res.OK = true
		}
		results = append(results, res)
	}
	return summarize(results), nil
}

// TagBatchMode 指定批量标签操作的方式。
type TagBatchMode string

const (
	// TagAdd 追加标签，保留已有标签。
	TagAdd TagBatchMode = "add"
	// TagRemove 移除指定标签。
	TagRemove TagBatchMode = "remove"
	// TagReplace 用指定标签整体替换。
	TagReplace TagBatchMode = "replace"
)

// TagBatch 批量修改标签。
//
// 提供三种模式而非只做替换：多账号场景常见的是"给这批加个已验证标签"，
// 若只能替换，用户得先读出原标签再合并，容易误删。
func (s *Service) TagBatch(ids []string, tags []string, mode TagBatchMode) (BatchSummary, error) {
	if len(ids) == 0 {
		return BatchSummary{}, errors.New("未指定要修改的 profile")
	}
	switch mode {
	case TagAdd, TagRemove, TagReplace:
	default:
		return BatchSummary{}, fmt.Errorf("标签操作模式 %q 无效", mode)
	}

	results := make([]BatchResult, 0, len(ids))
	for _, id := range ids {
		res := BatchResult{ProfileID: id}
		p, err := s.store.Get(id)
		if err != nil {
			res.Err = err.Error()
			results = append(results, res)
			continue
		}
		res.Name = p.Name
		p.Tags = applyTagMode(p.Tags, tags, mode)
		if err := s.store.Save(p); err != nil {
			res.Err = err.Error()
		} else {
			res.OK = true
		}
		results = append(results, res)
	}
	return summarize(results), nil
}

// applyTagMode 按模式计算新的标签集合。规范化由 store 层负责。
func applyTagMode(existing, tags []string, mode TagBatchMode) []string {
	switch mode {
	case TagReplace:
		return tags
	case TagAdd:
		return append(append([]string{}, existing...), tags...)
	case TagRemove:
		out := make([]string, 0, len(existing))
		for _, t := range existing {
			if !containsFold(tags, t) {
				out = append(out, t)
			}
		}
		return out
	}
	return existing
}

// containsFold 报告 list 中是否有与 want 忽略大小写相等的项。
func containsFold(list []string, want string) bool {
	for _, s := range list {
		if strings.EqualFold(strings.TrimSpace(s), strings.TrimSpace(want)) {
			return true
		}
	}
	return false
}

func summarize(results []BatchResult) BatchSummary {
	sum := BatchSummary{Total: len(results), Results: results}
	for _, r := range results {
		if r.OK {
			sum.Succeeded++
		} else {
			sum.Failed++
		}
	}
	return sum
}
