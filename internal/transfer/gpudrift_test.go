package transfer

import (
	"strings"
	"testing"

	"better-web/internal/model"
)

// 跨 GPU 厂商迁移时必须警告。
//
// 场景：在 NVIDIA 机器上筛出同族种子（能过 Cloudflare），导出后在 Intel
// 机器上以 NewSeeds=false 导入（备份恢复的正确语义）。种子原样恢复，
// 派生 GPU 仍是 NVIDIA，而新宿主机是 Intel——又变成跨厂商了。
//
// 这个退化没有任何界面痕迹，只有实测才能发现，所以必须警告。
func TestImportWarnsOnGPUFamilyMismatch(t *testing.T) {
	b := Bundle{
		FormatVersion: FormatVersion,
		CatalogSize:   15,
		HostGPUFamily: model.GPUFamilyNVIDIA,
		Profiles: []Entry{
			{Name: "a", Kind: model.KindFingerprint, Seed: 123},
		},
	}
	idFn, seedFn, dirFn := testDeps()
	res := Prepare(b, Options{
		HostGPUFamily: model.GPUFamilyIntel,
	}, 15, idFn, seedFn, dirFn)

	if !hasWarningAbout(res.Warnings, "NVIDIA", "Intel") {
		t.Fatalf("应警告 GPU 族不同，实际警告: %v", res.Warnings)
	}
	// 不阻断导入：跨厂商只影响能否过风控，profile 本身可用。
	if len(res.Prepared) != 1 {
		t.Errorf("警告不应阻断导入，实际准备了 %d 条", len(res.Prepared))
	}
}

// 同族迁移不该警告——种子在新机器上仍是同族，没有退化。
func TestImportSilentOnSameGPUFamily(t *testing.T) {
	b := Bundle{
		FormatVersion: FormatVersion, CatalogSize: 15,
		HostGPUFamily: model.GPUFamilyNVIDIA,
		Profiles:      []Entry{{Name: "a", Kind: model.KindFingerprint, Seed: 123}},
	}
	idFn, seedFn, dirFn := testDeps()
	res := Prepare(b, Options{
		HostGPUFamily: model.GPUFamilyNVIDIA,
	}, 15, idFn, seedFn, dirFn)

	for _, w := range res.Warnings {
		if strings.Contains(w, "GPU") {
			t.Errorf("同族不应警告 GPU，实际: %q", w)
		}
	}
}

// NewSeeds=true 时不该警告：种子是新生成的，原机器的 GPU 族已无关。
// 这条与档案库那个警告的判断逻辑一致，避免产生噪声。
func TestImportSkipsGPUWarningWhenGeneratingNewSeeds(t *testing.T) {
	b := Bundle{
		FormatVersion: FormatVersion, CatalogSize: 15,
		HostGPUFamily: model.GPUFamilyNVIDIA,
		Profiles:      []Entry{{Name: "a", Kind: model.KindFingerprint, Seed: 123}},
	}
	idFn, seedFn, dirFn := testDeps()
	res := Prepare(b, Options{
		NewSeeds:      true,
		HostGPUFamily: model.GPUFamilyIntel,
	}, 15, idFn, seedFn, dirFn)

	for _, w := range res.Warnings {
		if strings.Contains(w, "GPU") {
			t.Errorf("生成新种子时不应警告 GPU，实际: %q", w)
		}
	}
}

// 任一侧的 GPU 族未知时不警告。
//
// 探测要启动一次浏览器，可能未装内核或超时，此时取不到值。
// 拿 unknown 去比对会对所有导入都报警告，那是纯噪声。
func TestImportSkipsGPUWarningWhenEitherSideUnknown(t *testing.T) {
	cases := map[string]struct {
		exported, local model.GPUFamily
	}{
		"导出侧未知":  {model.GPUFamilyUnknown, model.GPUFamilyIntel},
		"本机侧未知":  {model.GPUFamilyNVIDIA, model.GPUFamilyUnknown},
		"两侧都未知":  {model.GPUFamilyUnknown, model.GPUFamilyUnknown},
		"导出侧为空串": {"", model.GPUFamilyIntel},
		"本机侧为空串": {model.GPUFamilyNVIDIA, ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			b := Bundle{
				FormatVersion: FormatVersion, CatalogSize: 15,
				HostGPUFamily: c.exported,
				Profiles: []Entry{
					{Name: "a", Kind: model.KindFingerprint, Seed: 123},
				},
			}
			idFn, seedFn, dirFn := testDeps()
			res := Prepare(b, Options{HostGPUFamily: c.local},
				15, idFn, seedFn, dirFn)
			for _, w := range res.Warnings {
				if strings.Contains(w, "GPU") {
					t.Errorf("不应警告，实际: %q", w)
				}
			}
		})
	}
}

// 导出时应把本机 GPU 族写进文件，否则导入侧无从比对。
func TestExportRecordsHostGPUFamily(t *testing.T) {
	var buf strings.Builder
	if err := Export(&buf, sampleProfiles(), 15, false,
		model.GPUFamilyNVIDIA); err != nil {
		t.Fatalf("导出失败: %v", err)
	}
	if !strings.Contains(buf.String(), `"hostGPUFamily": "NVIDIA"`) {
		t.Error("导出文件应记录 hostGPUFamily")
	}
}

// 未探测到 GPU 时导出仍应成功，字段省略。
// 探测失败不该阻断导出——那是为了一条提示牺牲主功能。
func TestExportOmitsUnknownHostGPU(t *testing.T) {
	var buf strings.Builder
	if err := Export(&buf, sampleProfiles(), 15, false, ""); err != nil {
		t.Fatalf("导出失败: %v", err)
	}
	if strings.Contains(buf.String(), "hostGPUFamily") {
		t.Error("未探测到 GPU 时该字段应省略")
	}
}

func hasWarningAbout(warnings []string, parts ...string) bool {
	for _, w := range warnings {
		if !strings.Contains(w, "GPU") {
			continue
		}
		all := true
		for _, p := range parts {
			if !strings.Contains(w, p) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}
