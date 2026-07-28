package store

import (
	"fmt"
	"slices"
	"strings"

	"better-web/internal/model"
)

// Filter 是 profile 列表的筛选条件。各字段为零值时表示该维度不筛选。
type Filter struct {
	// Group 按分组精确匹配。用 GroupUnassigned 匹配未分组的 profile。
	Group string `json:"group,omitempty"`
	// Tags 按标签筛选，需同时具备全部指定标签（AND 语义）。
	//
	// 用 AND 而非 OR：多标签筛选的实际意图通常是收窄范围
	// （"已验证"且"待养号"），OR 会让筛选结果越选越多，不符合直觉。
	Tags []string `json:"tags,omitempty"`
	// Keyword 在名称与备注中做子串匹配，忽略大小写。
	Keyword string `json:"keyword,omitempty"`
	// Kind 按 profile 类型筛选。
	Kind model.ProfileKind `json:"kind,omitempty"`
}

// GroupUnassigned 是筛选未分组 profile 的特殊分组值。
//
// 需要这个哨兵是因为空串在 Filter 中表示"不按分组筛选"，
// 无法用它表达"只看未分组的"。
const GroupUnassigned = "\x00unassigned"

// IsZero 报告该筛选条件是否不做任何过滤。
func (f Filter) IsZero() bool {
	return f.Group == "" && len(f.Tags) == 0 && f.Keyword == "" && f.Kind == ""
}

// Query 按筛选条件返回 profile，最近使用的在前。
//
// 分组、关键词、类型在 SQL 层过滤；标签在 Go 侧过滤——标签以 JSON 数组
// 存储，用 SQL 的 LIKE 匹配会把 "US" 误配到 "USA" 上，而 SQLite 的
// json_each 在不同构建中可用性不一，不值得为此引入不确定性。
// profile 数量在这类工具里是百级，内存过滤的开销可以忽略。
func (s *Store) Query(f Filter) ([]*model.Profile, error) {
	var (
		where []string
		args  []any
	)

	switch f.Group {
	case "":
		// 不按分组筛选。
	case GroupUnassigned:
		where = append(where, `grp = ''`)
	default:
		where = append(where, `grp = ?`)
		args = append(args, model.NormalizeGroup(f.Group))
	}

	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		// SQLite 的 LIKE 对 ASCII 默认不区分大小写，但对非 ASCII 区分，
		// 因此两侧都转小写以保证中文备注也能稳定匹配。
		//
		// ESCAPE 子句必不可少：escapeLike 用反斜杠转义 % 与 _，
		// 但 SQLite 默认没有转义字符，不声明的话反斜杠会被当成普通字符
		// 参与匹配，导致本应命中的记录一个都查不到。
		where = append(where,
			`(lower(name) LIKE ? ESCAPE '\' OR lower(notes) LIKE ? ESCAPE '\')`)
		pattern := "%" + strings.ToLower(escapeLike(kw)) + "%"
		args = append(args, pattern, pattern)
	}

	if f.Kind != "" {
		if !f.Kind.Valid() {
			return nil, fmt.Errorf("筛选条件中的 profile 类型 %q 无效", f.Kind)
		}
		where = append(where, `kind = ?`)
		args = append(args, string(f.Kind))
	}

	query := selectColumns
	if len(where) > 0 {
		query += ` WHERE ` + strings.Join(where, ` AND `)
	}
	query += ` ORDER BY last_use_at DESC, created_at DESC`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("查询 profile 失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	wantTags := model.NormalizeTags(f.Tags)
	var out []*model.Profile
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		if !hasAllTags(p, wantTags) {
			continue
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// hasAllTags 报告 profile 是否具备全部指定标签。
func hasAllTags(p *model.Profile, tags []string) bool {
	for _, t := range tags {
		if !p.HasTag(t) {
			return false
		}
	}
	return true
}

// escapeLike 转义 LIKE 模式中的通配符。
//
// 不转义的话，用户搜索 "50%" 会因 % 被当成通配符而匹配到全部记录，
// 搜索 "_" 则匹配任意单字符——都是静默的错误结果，比报错更难发现。
func escapeLike(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '%', '_', '\\':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Groups 返回全部已使用的分组名及其 profile 数量，按名称升序。
// 未分组的 profile 不计入，其数量由 UnassignedCount 单独给出。
func (s *Store) Groups() ([]GroupStat, error) {
	rows, err := s.db.Query(`
		SELECT grp, COUNT(*) FROM profiles
		WHERE grp <> '' GROUP BY grp ORDER BY grp ASC`)
	if err != nil {
		return nil, fmt.Errorf("查询分组失败: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []GroupStat
	for rows.Next() {
		var g GroupStat
		if err := rows.Scan(&g.Name, &g.Count); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// GroupStat 是一个分组及其 profile 数量。
type GroupStat struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// UnassignedCount 返回未分组的 profile 数量。
func (s *Store) UnassignedCount() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM profiles WHERE grp = ''`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("统计未分组 profile 失败: %w", err)
	}
	return n, nil
}

// Tags 返回全部已使用的标签及其 profile 数量，按数量降序。
//
// 标签存在 JSON 数组里，无法用 GROUP BY 直接统计，因此在 Go 侧聚合。
func (s *Store) Tags() ([]TagStat, error) {
	list, err := s.List()
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	// 大小写不同的同名标签合并计数，但展示时要保留一种原始写法，
	// 不能显示成全小写。
	//
	// 选"字典序最小"而非"首次出现"：List 按 last_use_at 排序，
	// 时间戳相同的记录顺序由 SQLite 决定，"首次出现"因此不稳定，
	// 会让同一份数据每次刷新显示不同的大小写。
	display := map[string]string{}
	for _, p := range list {
		for _, t := range p.Tags {
			key := strings.ToLower(t)
			counts[key]++
			if cur, ok := display[key]; !ok || t < cur {
				display[key] = t
			}
		}
	}

	out := make([]TagStat, 0, len(counts))
	for key, n := range counts {
		out = append(out, TagStat{Name: display[key], Count: n})
	}
	// 数量降序，同数量按名称升序，保证顺序稳定——否则界面每次刷新都在跳。
	sortTagStats(out)
	return out, nil
}

// TagStat 是一个标签及其 profile 数量。
type TagStat struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// sortTagStats 按数量降序、同数量按名称升序排列。
func sortTagStats(stats []TagStat) {
	slices.SortFunc(stats, func(a, b TagStat) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
}
