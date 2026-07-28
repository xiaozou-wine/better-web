package store

import (
	"testing"

	"better-web/internal/model"
)

// seedProfiles 建一组带分组与标签的 profile，供筛选测试复用。
func seedProfiles(t *testing.T, s *Store) {
	t.Helper()
	fixtures := []struct {
		id, name, group string
		kind            model.ProfileKind
		tags            []string
		notes           string
	}{
		{"a", "美国-01", "电商-美国站", model.KindFingerprint, []string{"已验证", "待养号"}, ""},
		{"b", "美国-02", "电商-美国站", model.KindFingerprint, []string{"已验证"}, "备用"},
		{"c", "德国-01", "电商-欧洲站", model.KindFingerprint, []string{"待养号"}, ""},
		{"d", "日常浏览", "", model.KindDaily, nil, "自己用的"},
		{"e", "折扣50%测试", "", model.KindFingerprint, nil, ""},
	}
	for _, f := range fixtures {
		p := &model.Profile{
			ID: f.id, Name: f.name, Kind: f.kind, ProfileDir: "d/" + f.id,
			Group: f.group, Tags: f.tags, Notes: f.notes,
		}
		if err := s.Save(p); err != nil {
			t.Fatalf("保存 %s 失败: %v", f.id, err)
		}
	}
}

func ids(list []*model.Profile) []string {
	out := make([]string, 0, len(list))
	for _, p := range list {
		out = append(out, p.ID)
	}
	return out
}

func TestQueryNoFilterReturnsAll(t *testing.T) {
	s := openTemp(t)
	seedProfiles(t, s)
	got, err := s.Query(Filter{})
	if err != nil {
		t.Fatalf("Query 失败: %v", err)
	}
	if len(got) != 5 {
		t.Errorf("数量 = %d, 期望 5: %v", len(got), ids(got))
	}
}

func TestQueryByGroup(t *testing.T) {
	s := openTemp(t)
	seedProfiles(t, s)

	got, err := s.Query(Filter{Group: "电商-美国站"})
	if err != nil {
		t.Fatalf("Query 失败: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("数量 = %d, 期望 2: %v", len(got), ids(got))
	}
	for _, p := range got {
		if p.Group != "电商-美国站" {
			t.Errorf("%s 的分组 = %q", p.ID, p.Group)
		}
	}
}

// 空串表示"不按分组筛选"，因此需要一个哨兵值来表达"只看未分组的"。
func TestQueryUnassignedGroup(t *testing.T) {
	s := openTemp(t)
	seedProfiles(t, s)

	got, err := s.Query(Filter{Group: GroupUnassigned})
	if err != nil {
		t.Fatalf("Query 失败: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("未分组数量 = %d, 期望 2: %v", len(got), ids(got))
	}
	for _, p := range got {
		if p.Group != "" {
			t.Errorf("%s 不应有分组: %q", p.ID, p.Group)
		}
	}
}

// 多标签是 AND 语义：筛选的意图是收窄范围，OR 会越选越多。
func TestQueryByTagsUsesAndSemantics(t *testing.T) {
	s := openTemp(t)
	seedProfiles(t, s)

	single, err := s.Query(Filter{Tags: []string{"已验证"}})
	if err != nil {
		t.Fatalf("Query 失败: %v", err)
	}
	if len(single) != 2 {
		t.Errorf("单标签数量 = %d, 期望 2: %v", len(single), ids(single))
	}

	both, err := s.Query(Filter{Tags: []string{"已验证", "待养号"}})
	if err != nil {
		t.Fatalf("Query 失败: %v", err)
	}
	if len(both) != 1 || both[0].ID != "a" {
		t.Errorf("双标签结果 = %v, 期望只有 a", ids(both))
	}
}

func TestQueryByTagIgnoresCase(t *testing.T) {
	s := openTemp(t)
	if err := s.Save(&model.Profile{
		ID: "x", Name: "大小写", Kind: model.KindDaily, ProfileDir: "d",
		Tags: []string{"Verified"},
	}); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}
	for _, q := range []string{"verified", "VERIFIED", "Verified"} {
		got, err := s.Query(Filter{Tags: []string{q}})
		if err != nil {
			t.Fatalf("Query 失败: %v", err)
		}
		if len(got) != 1 {
			t.Errorf("按 %q 筛选得到 %d 项, 期望 1", q, len(got))
		}
	}
}

func TestQueryByKeywordMatchesNameAndNotes(t *testing.T) {
	s := openTemp(t)
	seedProfiles(t, s)

	byName, err := s.Query(Filter{Keyword: "德国"})
	if err != nil {
		t.Fatalf("Query 失败: %v", err)
	}
	if len(byName) != 1 || byName[0].ID != "c" {
		t.Errorf("按名称搜索 = %v, 期望 c", ids(byName))
	}

	byNotes, err := s.Query(Filter{Keyword: "自己用"})
	if err != nil {
		t.Fatalf("Query 失败: %v", err)
	}
	if len(byNotes) != 1 || byNotes[0].ID != "d" {
		t.Errorf("按备注搜索 = %v, 期望 d", ids(byNotes))
	}
}

// 关键词里的 LIKE 通配符必须转义。
// 不转义时搜 "%" 会匹配全部记录——静默的错误结果比报错更难发现。
func TestQueryEscapesLikeWildcards(t *testing.T) {
	s := openTemp(t)
	seedProfiles(t, s)

	got, err := s.Query(Filter{Keyword: "50%"})
	if err != nil {
		t.Fatalf("Query 失败: %v", err)
	}
	if len(got) != 1 || got[0].ID != "e" {
		t.Errorf("搜索 \"50%%\" = %v, 期望只有 e", ids(got))
	}

	// 单独一个 % 不应匹配任何记录（没有名称含字面的 %，除了 e）。
	only, err := s.Query(Filter{Keyword: "%"})
	if err != nil {
		t.Fatalf("Query 失败: %v", err)
	}
	if len(only) != 1 || only[0].ID != "e" {
		t.Errorf("搜索 \"%%\" = %v, 期望只有含字面 %% 的 e", ids(only))
	}

	// 下划线同理，是单字符通配符。
	if _, err := s.Query(Filter{Keyword: "_"}); err != nil {
		t.Fatalf("搜索下划线失败: %v", err)
	}
}

func TestQueryByKind(t *testing.T) {
	s := openTemp(t)
	seedProfiles(t, s)

	daily, err := s.Query(Filter{Kind: model.KindDaily})
	if err != nil {
		t.Fatalf("Query 失败: %v", err)
	}
	if len(daily) != 1 || daily[0].ID != "d" {
		t.Errorf("日常模式 = %v, 期望 d", ids(daily))
	}

	if _, err := s.Query(Filter{Kind: "bogus"}); err == nil {
		t.Error("无效类型应报错")
	}
}

// 多个条件应当同时生效（AND）。
func TestQueryCombinesFilters(t *testing.T) {
	s := openTemp(t)
	seedProfiles(t, s)

	got, err := s.Query(Filter{
		Group: "电商-美国站", Tags: []string{"已验证"}, Keyword: "02",
	})
	if err != nil {
		t.Fatalf("Query 失败: %v", err)
	}
	if len(got) != 1 || got[0].ID != "b" {
		t.Errorf("组合筛选 = %v, 期望 b", ids(got))
	}
}

func TestQueryNoMatchReturnsEmpty(t *testing.T) {
	s := openTemp(t)
	seedProfiles(t, s)
	got, err := s.Query(Filter{Keyword: "不存在的关键词"})
	if err != nil {
		t.Fatalf("Query 失败: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("期望空结果, 实际 %v", ids(got))
	}
}

func TestGroupsAndUnassignedCount(t *testing.T) {
	s := openTemp(t)
	seedProfiles(t, s)

	groups, err := s.Groups()
	if err != nil {
		t.Fatalf("Groups 失败: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("分组数 = %d, 期望 2: %+v", len(groups), groups)
	}
	// 按名称升序。
	if groups[0].Name != "电商-欧洲站" || groups[1].Name != "电商-美国站" {
		t.Errorf("分组顺序 = %q, %q", groups[0].Name, groups[1].Name)
	}
	if groups[1].Count != 2 {
		t.Errorf("电商-美国站 的数量 = %d, 期望 2", groups[1].Count)
	}

	n, err := s.UnassignedCount()
	if err != nil {
		t.Fatalf("UnassignedCount 失败: %v", err)
	}
	if n != 2 {
		t.Errorf("未分组数量 = %d, 期望 2", n)
	}
}

func TestTagsAggregation(t *testing.T) {
	s := openTemp(t)
	seedProfiles(t, s)

	tags, err := s.Tags()
	if err != nil {
		t.Fatalf("Tags 失败: %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("标签数 = %d, 期望 2: %+v", len(tags), tags)
	}
	// 数量降序：已验证(2) 在 待养号(2) 之前时按名称升序，两者都是 2。
	for _, tg := range tags {
		if tg.Count != 2 {
			t.Errorf("标签 %q 数量 = %d, 期望 2", tg.Name, tg.Count)
		}
	}
}

// 标签统计应保留一种原始大小写，不能显示成全小写。
//
// 展示名取字典序最小者而非"首次出现"：List 按 last_use_at 排序，
// 时间戳相同时顺序由 SQLite 决定，依赖出现顺序会让结果随机漂移
// （实测同一份数据多次运行会给出不同的大小写）。
func TestTagsPreservesOriginalCase(t *testing.T) {
	s := openTemp(t)
	if err := s.Save(&model.Profile{
		ID: "x", Name: "n1", Kind: model.KindDaily, ProfileDir: "d1",
		Tags: []string{"Verified"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(&model.Profile{
		ID: "y", Name: "n2", Kind: model.KindDaily, ProfileDir: "d2",
		Tags: []string{"verified"},
	}); err != nil {
		t.Fatal(err)
	}

	tags, err := s.Tags()
	if err != nil {
		t.Fatalf("Tags 失败: %v", err)
	}
	if len(tags) != 1 {
		t.Fatalf("大小写不同的同名标签应合并为 1 项, 实际 %+v", tags)
	}
	if tags[0].Count != 2 {
		t.Errorf("合并后数量 = %d, 期望 2", tags[0].Count)
	}
	// 大写字母的码点小于小写，故 "Verified" < "verified"。
	if tags[0].Name != "Verified" {
		t.Errorf("展示名 = %q, 期望字典序最小的 Verified", tags[0].Name)
	}
}

func TestGroupAndTagsRoundTrip(t *testing.T) {
	s := openTemp(t)
	p := &model.Profile{
		ID: "x", Name: "往返", Kind: model.KindFingerprint, ProfileDir: "d",
		Group: "  分组带空白  ", Tags: []string{" 标签A ", "", "标签A", "标签B"},
	}
	if err := s.Save(p); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}
	// Save 应把规范化结果写回入参，让调用方与库内一致。
	if p.Group != "分组带空白" {
		t.Errorf("Save 后入参的分组 = %q", p.Group)
	}
	if len(p.Tags) != 2 {
		t.Errorf("Save 后入参的标签 = %v, 期望去重去空后 2 项", p.Tags)
	}

	got, err := s.Get("x")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if got.Group != "分组带空白" {
		t.Errorf("读回的分组 = %q", got.Group)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "标签A" || got.Tags[1] != "标签B" {
		t.Errorf("读回的标签 = %v", got.Tags)
	}
}

// 旧库没有 grp/tags 列，迁移后读取不应报错，值为零值。
func TestQueryWorksOnMigratedLegacyDatabase(t *testing.T) {
	s := openTemp(t)
	// 直接插入不含新列的记录，模拟迁移后的历史数据。
	if _, err := s.db.Exec(`
		INSERT INTO profiles (id, name, kind, seed, profile_dir,
			kernel_version, notes, created_at, updated_at, last_use_at)
		VALUES ('old','历史','daily',0,'d','','',0,0,0)`); err != nil {
		t.Fatalf("插入历史记录失败: %v", err)
	}

	got, err := s.Query(Filter{})
	if err != nil {
		t.Fatalf("Query 失败: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("数量 = %d", len(got))
	}
	if got[0].Group != "" || got[0].Tags != nil {
		t.Errorf("历史记录的分组/标签应为零值: %q / %v", got[0].Group, got[0].Tags)
	}
}
