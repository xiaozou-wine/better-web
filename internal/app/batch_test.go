package app

import (
	"context"
	"strings"
	"testing"

	"better-web/internal/model"
	"better-web/internal/store"
)

// makeProfiles 建 n 个 profile 并返回其 ID。
func makeProfiles(t *testing.T, s *Service, n int) []string {
	t.Helper()
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		v, err := s.CreateProfile(context.Background(), CreateRequest{
			Name: "批量-" + string(rune('A'+i)), Kind: model.KindDaily,
		})
		if err != nil {
			t.Fatalf("创建第 %d 个失败: %v", i, err)
		}
		ids = append(ids, v.ID)
	}
	return ids
}

func TestBatchRejectsEmptyInput(t *testing.T) {
	s, _ := newTestService(t)
	if _, err := s.StopBatch(nil); err == nil {
		t.Error("StopBatch 空输入应报错")
	}
	if _, err := s.DeleteBatch(nil); err == nil {
		t.Error("DeleteBatch 空输入应报错")
	}
	if _, err := s.AssignGroupBatch(nil, "g"); err == nil {
		t.Error("AssignGroupBatch 空输入应报错")
	}
	if _, err := s.TagBatch(nil, []string{"t"}, TagAdd); err == nil {
		t.Error("TagBatch 空输入应报错")
	}
}

func TestAssignGroupBatch(t *testing.T) {
	s, _ := newTestService(t)
	ids := makeProfiles(t, s, 3)

	sum, err := s.AssignGroupBatch(ids, "电商-美国站")
	if err != nil {
		t.Fatalf("AssignGroupBatch 失败: %v", err)
	}
	if sum.Succeeded != 3 || sum.Failed != 0 {
		t.Errorf("汇总 = 成功 %d 失败 %d, 期望 3/0", sum.Succeeded, sum.Failed)
	}
	// 结果里应带名称，界面才能显示是哪个出了问题。
	for _, r := range sum.Results {
		if r.Name == "" {
			t.Errorf("结果缺少名称: %+v", r)
		}
	}

	got, err := s.QueryProfiles(store.Filter{Group: "电商-美国站"})
	if err != nil {
		t.Fatalf("QueryProfiles 失败: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("分组内数量 = %d, 期望 3", len(got))
	}

	// 空串移出分组。
	if _, err := s.AssignGroupBatch(ids, ""); err != nil {
		t.Fatalf("移出分组失败: %v", err)
	}
	unassigned, err := s.QueryProfiles(store.Filter{Group: store.GroupUnassigned})
	if err != nil {
		t.Fatalf("QueryProfiles 失败: %v", err)
	}
	if len(unassigned) != 3 {
		t.Errorf("未分组数量 = %d, 期望 3", len(unassigned))
	}
}

// 三种标签模式的语义必须分明：只做替换会让"批量加个标签"变成危险操作。
func TestTagBatchModes(t *testing.T) {
	s, _ := newTestService(t)
	ids := makeProfiles(t, s, 2)

	// 先各自打上基础标签。
	if _, err := s.TagBatch(ids, []string{"基础"}, TagAdd); err != nil {
		t.Fatalf("TagAdd 失败: %v", err)
	}

	t.Run("add 保留已有标签", func(t *testing.T) {
		if _, err := s.TagBatch(ids, []string{"已验证"}, TagAdd); err != nil {
			t.Fatalf("TagAdd 失败: %v", err)
		}
		v, err := s.GetProfile(ids[0])
		if err != nil {
			t.Fatal(err)
		}
		if len(v.Tags) != 2 {
			t.Errorf("标签 = %v, 期望保留基础并追加已验证", v.Tags)
		}
	})

	t.Run("remove 只删指定标签", func(t *testing.T) {
		if _, err := s.TagBatch(ids, []string{"基础"}, TagRemove); err != nil {
			t.Fatalf("TagRemove 失败: %v", err)
		}
		v, err := s.GetProfile(ids[0])
		if err != nil {
			t.Fatal(err)
		}
		if len(v.Tags) != 1 || v.Tags[0] != "已验证" {
			t.Errorf("标签 = %v, 期望只剩已验证", v.Tags)
		}
	})

	t.Run("replace 整体替换", func(t *testing.T) {
		if _, err := s.TagBatch(ids, []string{"全新"}, TagReplace); err != nil {
			t.Fatalf("TagReplace 失败: %v", err)
		}
		v, err := s.GetProfile(ids[0])
		if err != nil {
			t.Fatal(err)
		}
		if len(v.Tags) != 1 || v.Tags[0] != "全新" {
			t.Errorf("标签 = %v, 期望只有全新", v.Tags)
		}
	})
}

// remove 应忽略大小写，否则用户看到 "Verified" 却删不掉。
func TestTagBatchRemoveIgnoresCase(t *testing.T) {
	s, _ := newTestService(t)
	v, err := s.CreateProfile(context.Background(), CreateRequest{
		Name: "大小写", Kind: model.KindDaily, Tags: []string{"Verified"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.TagBatch([]string{v.ID}, []string{"verified"}, TagRemove); err != nil {
		t.Fatalf("TagRemove 失败: %v", err)
	}
	got, err := s.GetProfile(v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tags) != 0 {
		t.Errorf("标签 = %v, 期望已删除", got.Tags)
	}
}

func TestTagBatchRejectsInvalidMode(t *testing.T) {
	s, _ := newTestService(t)
	ids := makeProfiles(t, s, 1)
	if _, err := s.TagBatch(ids, []string{"t"}, "bogus"); err == nil {
		t.Error("无效模式应报错")
	}
}

// 单个失败不应影响其余项。
func TestBatchContinuesOnPartialFailure(t *testing.T) {
	s, _ := newTestService(t)
	ids := makeProfiles(t, s, 2)
	mixed := append([]string{"不存在的-id"}, ids...)

	sum, err := s.AssignGroupBatch(mixed, "g")
	if err != nil {
		t.Fatalf("AssignGroupBatch 失败: %v", err)
	}
	if sum.Total != 3 {
		t.Errorf("总数 = %d, 期望 3", sum.Total)
	}
	if sum.Succeeded != 2 || sum.Failed != 1 {
		t.Errorf("汇总 = 成功 %d 失败 %d, 期望 2/1", sum.Succeeded, sum.Failed)
	}
	// 失败项必须给出原因。
	for _, r := range sum.Results {
		if !r.OK && r.Err == "" {
			t.Error("失败项缺少原因")
		}
	}
}

func TestDeleteBatchKeepsBrowsingData(t *testing.T) {
	s, _ := newTestService(t)
	ids := makeProfiles(t, s, 2)

	sum, err := s.DeleteBatch(ids)
	if err != nil {
		t.Fatalf("DeleteBatch 失败: %v", err)
	}
	if sum.Succeeded != 2 {
		t.Errorf("成功数 = %d, 期望 2", sum.Succeeded)
	}
	list, err := s.ListProfiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Errorf("删除后仍有 %d 个 profile", len(list))
	}
}

func TestStopBatchOnNotRunningIsNoop(t *testing.T) {
	s, _ := newTestService(t)
	ids := makeProfiles(t, s, 2)

	// 未运行的 profile 停止应视为成功，不该报错。
	sum, err := s.StopBatch(ids)
	if err != nil {
		t.Fatalf("StopBatch 失败: %v", err)
	}
	if sum.Failed != 0 {
		t.Errorf("停止未运行的 profile 出现失败: %+v", sum.Results)
	}
}

func TestImportProxies(t *testing.T) {
	s, _ := newTestService(t)

	text := `
# 注释行
1.2.3.4:1080:user1:pass1
5.6.7.8:1080
格式错误的行
http://u:p@9.9.9.9:8080
`
	res, err := s.ImportProxies(context.Background(), ImportRequest{
		Text: text, NamePrefix: "美国", Group: "电商", Tags: []string{"待验证"},
	})
	if err != nil {
		t.Fatalf("ImportProxies 失败: %v", err)
	}

	if len(res.Created) != 3 {
		t.Fatalf("创建数 = %d, 期望 3: %+v", len(res.Created), res.CreateFailed)
	}
	if len(res.ParseFailed) != 1 {
		t.Errorf("解析失败数 = %d, 期望 1", len(res.ParseFailed))
	}

	// 命名应补零，保证按名称排序时顺序正确。
	if res.Created[0].Name != "美国-01" {
		t.Errorf("首个名称 = %q, 期望 美国-01", res.Created[0].Name)
	}

	// 分组与标签应应用到全部导入项。
	for _, v := range res.Created {
		if v.Group != "电商" {
			t.Errorf("%s 的分组 = %q", v.Name, v.Group)
		}
		if len(v.Tags) != 1 || v.Tags[0] != "待验证" {
			t.Errorf("%s 的标签 = %v", v.Name, v.Tags)
		}
		if v.Proxy == nil {
			t.Errorf("%s 缺少代理配置", v.Name)
		}
	}

	// 每个 profile 必须有独立种子：共用种子会让 canvas 指纹一致，
	// 多账号一眼被关联。
	seeds := map[int32]bool{}
	for _, v := range res.Created {
		if v.Seed == 0 {
			t.Errorf("%s 的种子为 0", v.Name)
		}
		if seeds[v.Seed] {
			t.Errorf("%s 与其他 profile 共用了种子 %d", v.Name, v.Seed)
		}
		seeds[v.Seed] = true
	}
}

func TestImportProxiesDefaultsToFingerprintKind(t *testing.T) {
	s, _ := newTestService(t)
	res, err := s.ImportProxies(context.Background(), ImportRequest{Text: "1.2.3.4:1080"})
	if err != nil {
		t.Fatalf("ImportProxies 失败: %v", err)
	}
	if len(res.Created) != 1 {
		t.Fatalf("创建数 = %d", len(res.Created))
	}
	if res.Created[0].Kind != model.KindFingerprint {
		t.Errorf("类型 = %q, 期望默认为指纹模式", res.Created[0].Kind)
	}
	// 未指定前缀时用默认前缀。
	if !strings.HasPrefix(res.Created[0].Name, "导入") {
		t.Errorf("名称 = %q, 期望以默认前缀开头", res.Created[0].Name)
	}
}

func TestImportProxiesRejectsEmptyAndAllInvalid(t *testing.T) {
	s, _ := newTestService(t)
	if _, err := s.ImportProxies(context.Background(), ImportRequest{Text: "   "}); err == nil {
		t.Error("空内容应报错")
	}
	res, err := s.ImportProxies(context.Background(), ImportRequest{Text: "全是坏行\n另一坏行"})
	if err == nil {
		t.Error("全部无效时应报错")
	}
	// 即便整体报错，也要把解析失败详情带回去供用户排查。
	if len(res.ParseFailed) != 2 {
		t.Errorf("解析失败详情 = %d 项, 期望 2", len(res.ParseFailed))
	}
}

// 导入的密码不得出现在返回给前端的视图里。
func TestImportProxiesDoesNotLeakPassword(t *testing.T) {
	s, _ := newTestService(t)
	const pw = "IMPORT-SECRET-8f2a"
	res, err := s.ImportProxies(context.Background(), ImportRequest{Text: "1.2.3.4:1080:user:" + pw})
	if err != nil {
		t.Fatalf("ImportProxies 失败: %v", err)
	}
	if strings.Contains(dumpJSON(t, res), pw) {
		t.Error("导入结果中泄漏了代理密码")
	}
	if res.Created[0].Proxy == nil || !res.Created[0].Proxy.HasPassword {
		t.Error("应标记已设置密码")
	}
}

func TestGroupTree(t *testing.T) {
	s, _ := newTestService(t)
	ids := makeProfiles(t, s, 4)
	if _, err := s.AssignGroupBatch(ids[:2], "组A"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AssignGroupBatch(ids[2:3], "组B"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TagBatch(ids[:3], []string{"标签X"}, TagAdd); err != nil {
		t.Fatal(err)
	}

	tree, err := s.GroupTree()
	if err != nil {
		t.Fatalf("GroupTree 失败: %v", err)
	}
	if tree.Total != 4 {
		t.Errorf("总数 = %d, 期望 4", tree.Total)
	}
	if tree.Unassigned != 1 {
		t.Errorf("未分组 = %d, 期望 1", tree.Unassigned)
	}
	if len(tree.Groups) != 2 {
		t.Errorf("分组数 = %d, 期望 2", len(tree.Groups))
	}
	if len(tree.Tags) != 1 || tree.Tags[0].Count != 3 {
		t.Errorf("标签统计 = %+v, 期望标签X 有 3 项", tree.Tags)
	}

	// 分组数量之和加未分组应等于总数，否则侧边栏的数字对不上。
	sum := tree.Unassigned
	for _, g := range tree.Groups {
		sum += g.Count
	}
	if sum != tree.Total {
		t.Errorf("分组数量之和 %d 与总数 %d 不一致", sum, tree.Total)
	}
}

func TestGroupUnassignedKeyIsUsable(t *testing.T) {
	s, _ := newTestService(t)
	makeProfiles(t, s, 1)

	key := s.GroupUnassignedKey()
	got, err := s.QueryProfiles(store.Filter{Group: key})
	if err != nil {
		t.Fatalf("用暴露的哨兵值筛选失败: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("未分组数量 = %d, 期望 1", len(got))
	}
}
