package transfer

import (
	"fmt"
	"strings"
	"testing"

	"better-web/internal/model"
)

// testDeps 返回确定性的 ID 与种子生成器，便于断言。
func testDeps() (func() string, SeedFunc, func(string) string) {
	var idN, seedN int
	newID := func() string { idN++; return fmt.Sprintf("new-id-%d", idN) }
	newSeed := func() (int32, error) { seedN++; return int32(9000 + seedN), nil }
	dirFor := func(id string) string { return `C:\data\profiles\` + id }
	return newID, newSeed, dirFor
}

func bundleOf(entries ...Entry) Bundle {
	return Bundle{FormatVersion: FormatVersion, CatalogSize: 15, Profiles: entries}
}

// 备份恢复语义：保留原种子，否则恢复出来的是另一台设备。
func TestPrepareKeepsSeedsForRestore(t *testing.T) {
	b := bundleOf(
		Entry{Name: "a", Kind: model.KindFingerprint, Seed: 111},
		Entry{Name: "b", Kind: model.KindFingerprint, Seed: 222},
	)
	id, seed, dir := testDeps()
	res := Prepare(b, Options{NewSeeds: false}, 15, id, seed, dir)

	if len(res.Failed) > 0 {
		t.Fatalf("意外失败: %+v", res.Failed)
	}
	if len(res.Prepared) != 2 {
		t.Fatalf("准备好 %d 条, 期望 2", len(res.Prepared))
	}
	if res.Prepared[0].Seed != 111 || res.Prepared[1].Seed != 222 {
		t.Errorf("种子未保留: %d, %d", res.Prepared[0].Seed, res.Prepared[1].Seed)
	}
	// ID 与目录必须换成本机的。
	if res.Prepared[0].ID != "new-id-1" {
		t.Errorf("ID = %q, 期望重新生成", res.Prepared[0].ID)
	}
	if !strings.HasPrefix(res.Prepared[0].ProfileDir, `C:\data\profiles\`) {
		t.Errorf("ProfileDir = %q, 期望用本机布局", res.Prepared[0].ProfileDir)
	}
}

// 批量建号语义：必须生成新种子，否则所有 profile 共用一套指纹。
func TestPrepareGeneratesNewSeeds(t *testing.T) {
	b := bundleOf(
		Entry{Name: "a", Kind: model.KindFingerprint, Seed: 111},
		Entry{Name: "b", Kind: model.KindFingerprint, Seed: 111},
	)
	id, seed, dir := testDeps()
	res := Prepare(b, Options{NewSeeds: true}, 15, id, seed, dir)

	if len(res.Prepared) != 2 {
		t.Fatalf("准备好 %d 条, 期望 2", len(res.Prepared))
	}
	a, bb := res.Prepared[0].Seed, res.Prepared[1].Seed
	if a == 111 || bb == 111 {
		t.Error("仍在使用文件里的种子")
	}
	if a == bb {
		t.Error("两条 profile 拿到了相同的新种子")
	}
}

// 文件里种子重复且选择保留时必须警告：
// 多个 profile 共用一套 canvas 指纹，平台侧可直接关联。
func TestPrepareWarnsOnDuplicateSeeds(t *testing.T) {
	b := bundleOf(
		Entry{Name: "a", Kind: model.KindFingerprint, Seed: 555},
		Entry{Name: "b", Kind: model.KindFingerprint, Seed: 555},
	)
	id, seed, dir := testDeps()
	res := Prepare(b, Options{NewSeeds: false}, 15, id, seed, dir)

	if !hasWarning(res.Warnings, "共用相同种子") {
		t.Errorf("未警告种子重复: %v", res.Warnings)
	}
}

// 日常模式不需要种子——它不做任何伪造。
func TestPrepareSkipsSeedForDailyProfile(t *testing.T) {
	b := bundleOf(Entry{Name: "日常", Kind: model.KindDaily, Seed: 777})
	id, seed, dir := testDeps()
	res := Prepare(b, Options{}, 15, id, seed, dir)

	if len(res.Prepared) != 1 {
		t.Fatalf("准备好 %d 条", len(res.Prepared))
	}
	if res.Prepared[0].Seed != 0 {
		t.Errorf("日常模式的种子 = %d, 期望 0", res.Prepared[0].Seed)
	}
}

// 缺种子的指纹 profile 必须补一个，不能留 0 —— launcher 会拒绝种子为 0。
func TestPrepareFillsMissingSeed(t *testing.T) {
	b := bundleOf(Entry{Name: "a", Kind: model.KindFingerprint})
	id, seed, dir := testDeps()
	res := Prepare(b, Options{NewSeeds: false}, 15, id, seed, dir)

	if len(res.Prepared) != 1 {
		t.Fatalf("准备好 %d 条: %+v", len(res.Prepared), res.Failed)
	}
	if res.Prepared[0].Seed == 0 {
		t.Error("种子仍为 0，该 profile 将无法启动")
	}
}

// 单条无效不该拖垮整批，且必须报出是哪一条、为什么。
func TestPrepareReportsPerEntryFailures(t *testing.T) {
	b := bundleOf(
		Entry{Name: "好的", Kind: model.KindDaily},
		Entry{Name: "", Kind: model.KindDaily},
		Entry{Name: "类型错", Kind: "bogus"},
		Entry{Name: "代理端口错", Kind: model.KindDaily,
			Proxy: &ProxyEntry{Scheme: model.ProxySOCKS5, Host: "h", Port: 99999}},
		Entry{Name: "代理协议错", Kind: model.KindDaily,
			Proxy: &ProxyEntry{Scheme: "ftp", Host: "h", Port: 1080}},
		Entry{Name: "也是好的", Kind: model.KindDaily},
	)
	id, seed, dir := testDeps()
	res := Prepare(b, Options{}, 15, id, seed, dir)

	if len(res.Prepared) != 2 {
		t.Errorf("准备好 %d 条, 期望 2 条有效的", len(res.Prepared))
	}
	if len(res.Failed) != 4 {
		t.Fatalf("失败 %d 条, 期望 4: %+v", len(res.Failed), res.Failed)
	}
	// 索引要能对照文件定位，从 1 开始。
	wantIdx := []int{2, 3, 4, 5}
	for i, f := range res.Failed {
		if f.Index != wantIdx[i] {
			t.Errorf("第 %d 项失败的索引 = %d, 期望 %d", i, f.Index, wantIdx[i])
		}
		if f.Err == "" {
			t.Errorf("第 %d 项失败未给出原因", i)
		}
	}
}

// 文件内部重名必须在校验阶段拦下。
// 放过去的话第二条写库时才失败，此时第一条已落库，状态不一致。
func TestPrepareRejectsDuplicateNamesInFile(t *testing.T) {
	b := bundleOf(
		Entry{Name: "同名", Kind: model.KindDaily},
		Entry{Name: "同名", Kind: model.KindDaily},
	)
	id, seed, dir := testDeps()
	res := Prepare(b, Options{}, 15, id, seed, dir)

	if len(res.Prepared) != 1 {
		t.Errorf("准备好 %d 条, 期望 1", len(res.Prepared))
	}
	if len(res.Failed) != 1 || !strings.Contains(res.Failed[0].Err, "同名") {
		t.Errorf("未拦下文件内重名: %+v", res.Failed)
	}
}

// 与库中已有名称冲突时跳过而非失败——重名是常见情形，不该当成错误。
func TestPrepareSkipsExistingNames(t *testing.T) {
	b := bundleOf(
		Entry{Name: "已存在", Kind: model.KindDaily},
		Entry{Name: "新的", Kind: model.KindDaily},
	)
	id, seed, dir := testDeps()
	res := Prepare(b, Options{
		SkipExistingNames: map[string]bool{"已存在": true},
	}, 15, id, seed, dir)

	if len(res.Skipped) != 1 || res.Skipped[0] != "已存在" {
		t.Errorf("跳过列表 = %v", res.Skipped)
	}
	if len(res.Prepared) != 1 || res.Prepared[0].Name != "新的" {
		t.Errorf("准备结果不对: %+v", res.Prepared)
	}
}

// 前缀在重名判定之前生效，否则加了前缀反而被误判为重名。
func TestPrepareAppliesNamePrefixBeforeDuplicateCheck(t *testing.T) {
	b := bundleOf(Entry{Name: "01", Kind: model.KindDaily})
	id, seed, dir := testDeps()
	res := Prepare(b, Options{
		NamePrefix:        "批次A-",
		SkipExistingNames: map[string]bool{"01": true}, // 原名冲突，加前缀后不冲突
	}, 15, id, seed, dir)

	if len(res.Skipped) != 0 {
		t.Errorf("加前缀后不该被判为重名: %v", res.Skipped)
	}
	if len(res.Prepared) != 1 || res.Prepared[0].Name != "批次A-01" {
		t.Errorf("名称 = %+v", res.Prepared)
	}
}

func TestPrepareOverridesGroup(t *testing.T) {
	b := bundleOf(Entry{Name: "a", Kind: model.KindDaily, Group: "原分组"})
	id, seed, dir := testDeps()
	res := Prepare(b, Options{Group: "新分组"}, 15, id, seed, dir)

	if res.Prepared[0].Group != "新分组" {
		t.Errorf("分组 = %q, 期望被覆盖", res.Prepared[0].Group)
	}
}

// 档案库条目数不同会让同一种子抽到不同机型，保留原种子时必须警告。
func TestPrepareWarnsOnCatalogSizeMismatch(t *testing.T) {
	b := Bundle{FormatVersion: FormatVersion, CatalogSize: 10,
		Profiles: []Entry{{Name: "a", Kind: model.KindFingerprint, Seed: 1}}}
	id, seed, dir := testDeps()
	res := Prepare(b, Options{NewSeeds: false}, 15, id, seed, dir)

	if !hasWarning(res.Warnings, "档案库") {
		t.Errorf("未警告档案库条目数不同: %v", res.Warnings)
	}
}

// 生成新种子时机型本来就会变，档案库警告是噪声，不该出现。
func TestPrepareOmitsCatalogWarningWhenReseeding(t *testing.T) {
	b := Bundle{FormatVersion: FormatVersion, CatalogSize: 10,
		Profiles: []Entry{{Name: "a", Kind: model.KindFingerprint, Seed: 1}}}
	id, seed, dir := testDeps()
	res := Prepare(b, Options{NewSeeds: true}, 15, id, seed, dir)

	if hasWarning(res.Warnings, "档案库") {
		t.Errorf("生成新种子时不该给档案库警告: %v", res.Warnings)
	}
}

// 缺密码的代理必须提示补填，否则代理会静默认证失败。
func TestPrepareWarnsAboutMissingPasswords(t *testing.T) {
	b := Bundle{FormatVersion: FormatVersion, CatalogSize: 15, WithSecrets: false,
		Profiles: []Entry{{
			Name: "a", Kind: model.KindDaily,
			Proxy: &ProxyEntry{Scheme: model.ProxySOCKS5, Host: "h", Port: 1080,
				Username: "u", HadPassword: true},
		}}}
	id, seed, dir := testDeps()
	res := Prepare(b, Options{}, 15, id, seed, dir)

	if !hasWarning(res.Warnings, "补填") {
		t.Errorf("未提示补填密码: %v", res.Warnings)
	}
}

// 标签要经过与手工创建一致的清洗，否则导入的数据格式与库内不一致。
func TestPrepareNormalizesTags(t *testing.T) {
	b := bundleOf(Entry{Name: "a", Kind: model.KindDaily,
		Tags: []string{" 重复 ", "重复", "", "正常"}})
	id, seed, dir := testDeps()
	res := Prepare(b, Options{}, 15, id, seed, dir)

	tags := res.Prepared[0].Tags
	if len(tags) != 2 {
		t.Errorf("标签 = %v, 期望去重去空后 2 项", tags)
	}
}

func hasWarning(warnings []string, sub string) bool {
	for _, w := range warnings {
		if strings.Contains(w, sub) {
			return true
		}
	}
	return false
}
