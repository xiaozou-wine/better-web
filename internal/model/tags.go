package model

import "strings"

// MaxTagLength 是单个标签的长度上限。
//
// 加上限是为了界面：标签要在卡片上并排显示，过长的标签会把布局挤坏。
// 超长时截断而非报错——用户填标签时不该被打断。
const MaxTagLength = 24

// NormalizeTags 清洗标签集合：去空白、去空串、去重、截断超长项。
//
// 保持写入顺序而非排序：用户输入的先后往往体现重要性，
// 排序会让"主要标签"跑到列表中间。
func NormalizeTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(tags))
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if len([]rune(t)) > MaxTagLength {
			t = string([]rune(t)[:MaxTagLength])
		}
		// 大小写不同的同名标签视为同一个，否则筛选时会漏。
		key := strings.ToLower(t)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// NormalizeGroup 清洗分组名。空白视为未分组。
func NormalizeGroup(group string) string {
	group = strings.TrimSpace(group)
	if len([]rune(group)) > MaxTagLength {
		group = string([]rune(group)[:MaxTagLength])
	}
	return group
}

// HasTag 报告 profile 是否带有指定标签，比较时忽略大小写。
func (p *Profile) HasTag(tag string) bool {
	want := strings.ToLower(strings.TrimSpace(tag))
	if want == "" {
		return false
	}
	for _, t := range p.Tags {
		if strings.ToLower(t) == want {
			return true
		}
	}
	return false
}
