package app

import (
	"better-web/internal/store"
)

// QueryProfiles 按筛选条件返回 profile 视图。
//
// 与 ListProfiles 的关系：后者等价于不带筛选的本方法，保留是为了让
// 界面的初次加载路径更直白。
func (s *Service) QueryProfiles(f store.Filter) ([]ProfileView, error) {
	list, err := s.store.Query(f)
	if err != nil {
		return nil, err
	}
	out := make([]ProfileView, 0, len(list))
	for _, p := range list {
		out = append(out, s.toView(p))
	}
	return out, nil
}

// GroupTree 是分组侧边栏所需的全部数据。
type GroupTree struct {
	// Groups 是已使用的分组及其数量，按名称升序。
	Groups []store.GroupStat `json:"groups"`
	// Unassigned 是未分组的 profile 数量。
	Unassigned int `json:"unassigned"`
	// Total 是全部 profile 数量。
	Total int `json:"total"`
	// Tags 是已使用的标签及其数量，按数量降序。
	Tags []store.TagStat `json:"tags"`
}

// GroupTree 返回分组与标签的统计，供界面构建侧边栏。
//
// 一次调用返回全部维度而非分成三个接口：侧边栏总是整体刷新，
// 分开调用会让三份数据在刷新间隙出现不一致（分组数加起来不等于总数）。
func (s *Service) GroupTree() (GroupTree, error) {
	groups, err := s.store.Groups()
	if err != nil {
		return GroupTree{}, err
	}
	unassigned, err := s.store.UnassignedCount()
	if err != nil {
		return GroupTree{}, err
	}
	tags, err := s.store.Tags()
	if err != nil {
		return GroupTree{}, err
	}

	total := unassigned
	for _, g := range groups {
		total += g.Count
	}
	return GroupTree{
		Groups: groups, Unassigned: unassigned, Total: total, Tags: tags,
	}, nil
}

// GroupUnassignedKey 暴露给前端的"未分组"筛选值。
//
// 前端不能自己拼这个哨兵：它是个不可打印字符，写在 TS 里既易错又难读。
func (s *Service) GroupUnassignedKey() string { return store.GroupUnassigned }
