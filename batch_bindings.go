package main

import (
	"better-web/internal/app"
	"better-web/internal/store"
)

// 批量操作与分组筛选的前端绑定。
//
// 与 app.go 分开放：那边是单个 profile 的 CRUD 与会话操作，
// 这边是批量与浏览，两组接口的演进节奏不同。

// QueryProfiles 按筛选条件返回 profile。
func (a *App) QueryProfiles(f store.Filter) ([]app.ProfileView, error) {
	s, err := a.svc()
	if err != nil {
		return nil, err
	}
	return s.QueryProfiles(f)
}

// GroupTree 返回分组与标签统计，供界面构建侧边栏。
func (a *App) GroupTree() (app.GroupTree, error) {
	s, err := a.svc()
	if err != nil {
		return app.GroupTree{}, err
	}
	return s.GroupTree()
}

// GroupUnassignedKey 返回"未分组"的筛选值。
// 前端不能自己拼：它是个不可打印字符。
func (a *App) GroupUnassignedKey() (string, error) {
	s, err := a.svc()
	if err != nil {
		return "", err
	}
	return s.GroupUnassignedKey(), nil
}

// StartBatch 并发启动多个 profile。concurrency <= 0 时用默认上限。
func (a *App) StartBatch(ids []string, concurrency int) (app.BatchSummary, error) {
	s, err := a.svc()
	if err != nil {
		return app.BatchSummary{}, err
	}
	return s.StartBatch(a.ctx, ids, concurrency)
}

// StopBatch 停止多个 profile。
func (a *App) StopBatch(ids []string) (app.BatchSummary, error) {
	s, err := a.svc()
	if err != nil {
		return app.BatchSummary{}, err
	}
	return s.StopBatch(ids)
}

// DeleteBatch 删除多个 profile 的配置，保留其浏览数据。
func (a *App) DeleteBatch(ids []string) (app.BatchSummary, error) {
	s, err := a.svc()
	if err != nil {
		return app.BatchSummary{}, err
	}
	return s.DeleteBatch(ids)
}

// AssignGroupBatch 批量设置分组，空串表示移出分组。
func (a *App) AssignGroupBatch(ids []string, group string) (app.BatchSummary, error) {
	s, err := a.svc()
	if err != nil {
		return app.BatchSummary{}, err
	}
	return s.AssignGroupBatch(ids, group)
}

// TagBatch 批量修改标签。mode 取 add / remove / replace。
func (a *App) TagBatch(ids []string, tags []string, mode app.TagBatchMode) (app.BatchSummary, error) {
	s, err := a.svc()
	if err != nil {
		return app.BatchSummary{}, err
	}
	return s.TagBatch(ids, tags, mode)
}

// ImportProxies 批量导入代理并建 profile。
func (a *App) ImportProxies(req app.ImportRequest) (app.ImportResult, error) {
	s, err := a.svc()
	if err != nil {
		return app.ImportResult{}, err
	}
	return s.ImportProxies(a.ctx, req)
}

// DefaultBatchConcurrency 暴露默认并发上限，供界面显示与预填。
func (a *App) DefaultBatchConcurrency() int { return app.DefaultBatchConcurrency }
