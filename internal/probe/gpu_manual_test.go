package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// GPU 伪造矛盾定位：找出伪造在哪一项上与真实渲染行为对不上。
//
// 启用方式（直连，不需要代理）：
//
//	BW_RUN_GPU=1 go test -run TestGPUSpoofContradiction -timeout 300s -v ./internal/probe/
//
// 背景：TestAntibotControl 已定位到 GPU 伪造是 Cloudflare 拦截的充分必要
// 原因，但没说清是哪一项露馅。GPU 伪造只能改它拦得住的东西（身份字符串），
// 而 WebGL 的能力上限、扩展列表、着色器精度、实际渲染结果都由真实驱动产生。
// 任何一项与声称的型号对不上，就是可判定的矛盾。
//
// 两组对照，唯一变量是 GPU 伪造开关：
//
//	spoofed  = --fingerprint=<seed>
//	honest   = --fingerprint=<seed> --disable-spoofing=gpu
//
// 逐项 diff 后能得出结论：伪造覆盖了哪些字段、漏掉了哪些字段。
// 漏掉的那些就是检测方能用来发现矛盾的依据。
func TestGPUSpoofContradiction(t *testing.T) {
	if os.Getenv("BW_RUN_GPU") != "1" {
		t.Skip("未设置 BW_RUN_GPU=1，跳过 GPU 深度采集")
	}
	k := realKernel(t)
	p := &GPUProbe{ExecPath: k.ExecPath}

	const seed = "770828460"
	groups := []struct {
		name string
		args []string
	}{
		{"spoofed", []string{"--fingerprint=" + seed}},
		{"honest", []string{"--fingerprint=" + seed, "--disable-spoofing=gpu"}},
	}

	reports := map[string]GPUReport{}
	for _, g := range groups {
		rep, err := p.CollectGPU(context.Background(), g.args)
		if err != nil {
			t.Fatalf("[%s] 采集失败: %v", g.name, err)
		}
		reports[g.name] = rep

		out := filepath.Join("testdata", "gpu-"+g.name+"-"+k.Version+".json")
		if err := SaveGPUReport(out, rep); err != nil {
			t.Fatalf("保存失败: %v", err)
		}
		t.Logf("\n===== %s =====", g.name)
		logGL(t, "webgl1", rep.WebGL1)
		logGL(t, "webgl2", rep.WebGL2)
		t.Logf("  webgpu.available = %v", rep.WebGPU.Available)
		if rep.WebGPU.Err != "" {
			t.Logf("  webgpu.err = %s", rep.WebGPU.Err)
		}
		if len(rep.WebGPU.AdapterInfo) > 0 {
			t.Logf("  webgpu.adapterInfo = %v", rep.WebGPU.AdapterInfo)
		}
		if len(rep.WebGPU.Limits) > 0 {
			t.Logf("  webgpu.limits = %v", rep.WebGPU.Limits)
		}
		t.Logf("  uaArch = %v", rep.UAArch)
	}

	sp, ho := reports["spoofed"], reports["honest"]

	t.Log("\n===== 逐项差异（spoofed vs honest）=====")
	diffGL(t, "webgl1", sp.WebGL1, ho.WebGL1)
	diffGL(t, "webgl2", sp.WebGL2, ho.WebGL2)

	// WebGPU 是与 WebGL 独立的实现，若它照实上报真实 GPU，
	// 而 WebGL 声称另一个型号，两者直接矛盾——这是最容易被利用的泄露。
	if !reflect.DeepEqual(sp.WebGPU.AdapterInfo, ho.WebGPU.AdapterInfo) {
		t.Logf("webgpu.adapterInfo 有差异:\n  spoofed = %v\n  honest  = %v",
			sp.WebGPU.AdapterInfo, ho.WebGPU.AdapterInfo)
	} else if len(sp.WebGPU.AdapterInfo) > 0 {
		t.Logf("⚠ webgpu.adapterInfo 两组相同（未被伪造覆盖）: %v",
			sp.WebGPU.AdapterInfo)
	}
	if !reflect.DeepEqual(sp.WebGPU.Limits, ho.WebGPU.Limits) {
		t.Logf("webgpu.limits 有差异")
	} else if len(sp.WebGPU.Limits) > 0 {
		t.Log("⚠ webgpu.limits 两组相同（未被伪造覆盖）")
	}
}

func logGL(t *testing.T, label string, c GLContext) {
	t.Helper()
	if !c.Available {
		t.Logf("  %s: 不可用", label)
		return
	}
	t.Logf("  %s.vendor      = %s", label, c.Vendor)
	t.Logf("  %s.renderer    = %s", label, c.Renderer)
	t.Logf("  %s.glVersion   = %s", label, c.GLVersion)
	t.Logf("  %s.shadingLang = %s", label, c.ShadingLang)
	t.Logf("  %s.pixelHash   = %s", label, c.PixelHash)
	t.Logf("  %s.extCount    = %d", label, len(c.Extensions))
	t.Logf("  %s.limits      = %s", label, compact(c.Limits))
}

// diffGL 逐项比对两组 GLContext，把差异与相同项分别列出。
//
// 关注点在"相同项"：伪造声称了另一个型号，而这些项仍是真实驱动的值，
// 它们就是矛盾的来源。
func diffGL(t *testing.T, label string, sp, ho GLContext) {
	t.Helper()
	if !sp.Available || !ho.Available {
		t.Logf("%s: 有一组不可用（spoofed=%v honest=%v）",
			label, sp.Available, ho.Available)
		return
	}

	strFields := []struct {
		name string
		a, b string
	}{
		{"vendor", sp.Vendor, ho.Vendor},
		{"renderer", sp.Renderer, ho.Renderer},
		{"glVersion", sp.GLVersion, ho.GLVersion},
		{"shadingLang", sp.ShadingLang, ho.ShadingLang},
		{"pixelHash", sp.PixelHash, ho.PixelHash},
	}
	for _, f := range strFields {
		if f.a == f.b {
			t.Logf("  [同] %s.%s = %s", label, f.name, f.a)
			continue
		}
		t.Logf("  [异] %s.%s\n        spoofed = %s\n        honest  = %s",
			label, f.name, f.a, f.b)
	}

	// limits 逐键比对：任何一项相同都意味着该能力参数未被伪造，
	// 可用于反推真实 GPU 档次。
	var sameLimits, diffLimits []string
	for k, v := range ho.Limits {
		sv, ok := sp.Limits[k]
		if !ok {
			diffLimits = append(diffLimits, k+"(spoofed 缺失)")
			continue
		}
		if reflect.DeepEqual(fmt.Sprint(sv), fmt.Sprint(v)) {
			sameLimits = append(sameLimits, fmt.Sprintf("%s=%v", k, v))
		} else {
			diffLimits = append(diffLimits,
				fmt.Sprintf("%s: %v→%v", k, v, sv))
		}
	}
	sort.Strings(sameLimits)
	sort.Strings(diffLimits)
	t.Logf("  limits 未被伪造(%d 项): %v", len(sameLimits), sameLimits)
	t.Logf("  limits 被改写(%d 项): %v", len(diffLimits), diffLimits)

	// 扩展列表差异。集成显卡与独显的扩展集合不同，
	// 列表未变而型号声称变了同样是矛盾。
	if reflect.DeepEqual(sorted(sp.Extensions), sorted(ho.Extensions)) {
		t.Logf("  ⚠ %s.extensions 两组完全相同（%d 项，未被伪造覆盖）",
			label, len(sp.Extensions))
	} else {
		t.Logf("  %s.extensions 有差异: spoofed %d 项 / honest %d 项",
			label, len(sp.Extensions), len(ho.Extensions))
		for _, d := range symDiff(sp.Extensions, ho.Extensions) {
			t.Logf("      %s", d)
		}
	}

	if reflect.DeepEqual(sp.Precision, ho.Precision) {
		t.Logf("  ⚠ %s.precision 两组完全相同（未被伪造覆盖）", label)
	} else {
		t.Logf("  %s.precision 有差异", label)
	}
}

func sorted(in []string) []string {
	out := append([]string{}, in...)
	sort.Strings(out)
	return out
}

// symDiff 返回两个列表的对称差，标注各项属于哪一组。
func symDiff(a, b []string) []string {
	inA := map[string]bool{}
	for _, s := range a {
		inA[s] = true
	}
	inB := map[string]bool{}
	for _, s := range b {
		inB[s] = true
	}
	var out []string
	for _, s := range a {
		if !inB[s] {
			out = append(out, "仅 spoofed 有: "+s)
		}
	}
	for _, s := range b {
		if !inA[s] {
			out = append(out, "仅 honest 有: "+s)
		}
	}
	sort.Strings(out)
	return out
}

func compact(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}
