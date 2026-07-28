package probe

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// 站点声明 MatchPatterns 时，manifest 应原样使用它而非从 URL 推导。
//
// 这是 DataDome 能测出结论的前提：判定为机器人时会整页跳到
// geo.captcha-delivery.com，只注入原域名会让内容脚本随原页卸载，
// 采集侧只看到超时，读不出"被拦了"这个结论。
func TestMatchPatternsOverrideURLDerivation(t *testing.T) {
	files := extensionFiles(DataDome, "http://127.0.0.1:1234/report")
	var manifest struct {
		ContentScripts []struct {
			Matches []string `json:"matches"`
		} `json:"content_scripts"`
	}
	if err := json.Unmarshal([]byte(files["manifest.json"]), &manifest); err != nil {
		t.Fatalf("manifest 不是合法 JSON: %v\n%s", err, files["manifest.json"])
	}
	if len(manifest.ContentScripts) != 1 {
		t.Fatalf("content_scripts 应有 1 项，实际 %d", len(manifest.ContentScripts))
	}
	got := manifest.ContentScripts[0].Matches
	if len(got) != len(DataDome.MatchPatterns) {
		t.Fatalf("匹配模式数量不符，期望 %d 实际 %d: %v",
			len(DataDome.MatchPatterns), len(got), got)
	}
	var hasCaptcha bool
	for _, m := range got {
		if strings.Contains(m, "captcha-delivery.com") {
			hasCaptcha = true
		}
	}
	if !hasCaptcha {
		t.Errorf("匹配模式缺少 captcha 域，跳转后将无法采集: %v", got)
	}
}

// 未声明 MatchPatterns 的站点仍应从 URL 推导出单条模式。
func TestMatchPatternsFallBackToURL(t *testing.T) {
	files := extensionFiles(CreepJS, "http://127.0.0.1:1234/report")
	var manifest struct {
		ContentScripts []struct {
			Matches []string `json:"matches"`
		} `json:"content_scripts"`
	}
	if err := json.Unmarshal([]byte(files["manifest.json"]), &manifest); err != nil {
		t.Fatalf("manifest 不是合法 JSON: %v", err)
	}
	got := manifest.ContentScripts[0].Matches
	want := "https://abrahamjuliot.github.io/*"
	if len(got) != 1 || got[0] != want {
		t.Errorf("期望单条 %q，实际 %v", want, got)
	}
}

// 跨域站点先报"通过"、后报"未通过"时，必须采信后者。
//
// 这是本测试套件里最关键的一条：DataDome 跳转前的原页面可能短暂满足就绪
// 条件而先上报通过，但真实结局是被拦。采信先到的那份会得出与事实相反的
// 结论——让人以为指纹能过 DataDome，而实际没过。
func TestConfirmPrefersLaterFailure(t *testing.T) {
	s := &Scorer{ExecPath: "dummy"}
	more := make(chan map[string]any, 2)
	more <- map[string]any{"passed": false, "finalHost": "geo.captcha-delivery.com"}

	got := s.confirm(context.Background(), DataDome,
		map[string]any{"passed": true, "finalHost": "datadome.co"}, more)

	if passed, _ := got["passed"].(bool); passed {
		t.Error("先到的通过未被后到的失败推翻")
	}
	if host, _ := got["finalHost"].(string); host != "geo.captcha-delivery.com" {
		t.Errorf("应采信跳转后的上报，实际 finalHost=%q", host)
	}
	if superseded, _ := got["__supersededPass"].(bool); !superseded {
		t.Error("推翻时应标记 __supersededPass，否则排障时看不出发生过推翻")
	}
}

// 单域站点不应为等待第二次上报付出时间成本。
//
// CreepJS 等站只注入一个域，不会有第二次上报，等待纯属浪费。
func TestConfirmSkipsWaitForSingleDomainSite(t *testing.T) {
	s := &Scorer{ExecPath: "dummy"}
	first := map[string]any{"passed": true}
	started := time.Now()

	got := s.confirm(context.Background(), CreepJS, first, make(chan map[string]any))

	if elapsed := time.Since(started); elapsed > time.Second {
		t.Errorf("单域站点不应等待，实际耗时 %v", elapsed)
	}
	if passed, _ := got["passed"].(bool); !passed {
		t.Error("单域站点的结论应原样返回")
	}
}

// 首份上报即为"未通过"时应立即返回，不必等待。
// 它不可能被更坏的结论推翻，等待没有意义。
func TestConfirmReturnsFailureImmediately(t *testing.T) {
	s := &Scorer{ExecPath: "dummy"}
	started := time.Now()

	got := s.confirm(context.Background(), DataDome,
		map[string]any{"passed": false}, make(chan map[string]any))

	if elapsed := time.Since(started); elapsed > time.Second {
		t.Errorf("已是未通过不应等待，实际耗时 %v", elapsed)
	}
	if passed, _ := got["passed"].(bool); passed {
		t.Error("结论不应被改写")
	}
}

// Cloudflare 判据必须依赖正向成功标记，不能是"未命中挑战特征即通过"。
//
// 回归锁定：第一版用反向判据，被德语挑战页误判成通过（英文文案与
// #challenge-running 系列 id 全部落空）。
func TestCloudflareUsesPositiveSuccessMarker(t *testing.T) {
	if !strings.Contains(CloudflareChallenge.Extract, cfSuccessMarker) {
		t.Error("Extract 未使用正向成功标记，可能退回反向判据")
	}
	if !strings.Contains(CloudflareChallenge.ReadyCheck, cfSuccessMarker) {
		t.Error("ReadyCheck 未使用正向成功标记")
	}
	// passed 必须由成功标记决定。反向判据的典型写法是对挑战选择器计数取零，
	// 这里断言不存在那种写法。
	if strings.Contains(CloudflareChallenge.Extract, "hitSel.length === 0") {
		t.Error("Extract 仍含反向判据写法，德语挑战页会被误判为通过")
	}
}

// 商业风控站不得进入默认站点集与门禁。
//
// 它们的结果随出口 IP 信誉与访问频次浮动，进门禁会让内核回归检查抖动，
// 把环境问题误报成内核退化。
func TestAntibotSitesStayOutOfDefaultSites(t *testing.T) {
	for _, a := range AntibotSites {
		for _, d := range DefaultSites {
			if a.Name == d.Name {
				t.Errorf("%s 同时在 AntibotSites 与 DefaultSites 中", a.Name)
			}
		}
	}
	// 门禁只认 creepjs 与 browserscan 两个站名，风控站落进去会被当成
	// "未知站点"静默跳过；这里断言门禁确实不对它们下判断。
	results := []ScoreResult{
		{Site: CloudflareChallenge.Name, Metrics: map[string]any{"passed": false}},
		{Site: DataDome.Name, Metrics: map[string]any{"passed": false}},
	}
	if v := DefaultGate.Check(results); len(v) != 0 {
		t.Errorf("门禁不应对商业风控站下判断，实际产生 %d 条违规: %v", len(v), v)
	}
}
