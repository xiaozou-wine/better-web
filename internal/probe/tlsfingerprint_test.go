package probe

import (
	"context"
	"os"
	"strings"
	"testing"

	"better-web/internal/model"
)

// tlsProbeSite 查询本机 TLS 指纹的公开服务。
//
// 选 tls.browserleaks.com：返回 JA3、JA4 与 Akamai HTTP/2 指纹的 JSON，
// 无需注册。（tls.peet.ws 也提供同类数据，但其证书已过期，浏览器会停在
// 证书警告页而拿不到内容。）
//
// 用它回答一个具体问题：声称不同操作系统的 profile，TLS 指纹是否相同。
var tlsProbeSite = Site{
	Name: "tls.browserleaks.com",
	URL:  "https://tls.browserleaks.com/json",
	// 该接口直接返回 JSON，页面里就是一个 <pre>。
	ReadyCheck: `return document.body && document.body.innerText.includes('ja3_hash')`,
	Extract: `
		const raw = document.body.innerText
		let d = {}
		try { d = JSON.parse(raw) } catch (e) { return { err: '解析失败: ' + e.message } }
		return {
			ja3Hash: d.ja3_hash || '',
			ja3Text: d.ja3_text || '',
			ja4: d.ja4 || '',
			ja4Raw: d.ja4_r || '',
			akamaiHash: d.akamai_hash || '',
			userAgent: d.user_agent || '',
		}
	`,
	SettleMs: 45000,
}

// 声称不同操作系统的 profile，TLS 指纹是否相同？
//
// 这是架构层面的已知限制：转发器是纯 TCP 隧道，不碰 TLS，ClientHello 由
// Chromium 自带的 BoringSSL 生成。因此同一个内核二进制产出的 TLS 指纹
// 与 --fingerprint-platform 声称的系统无关。
//
// 本测试把该事实变成可核对的数据：如果三种声称系统给出同一个 JA3，
// 说明 TLS 层确实不随 profile 变化，跨维度矛盾客观存在。
//
// 需要联网且不经代理（走本机直连），因此默认跳过。
// 用 BW_PROBE_TLS=1 启用。
func TestReportTLSFingerprintAcrossPlatforms(t *testing.T) {
	requireEnv(t, "BW_PROBE_TLS")
	k := realKernel(t)
	geo := &model.Geo{CountryCode: "US", Timezone: "America/Los_Angeles", Locale: "en-US"}

	type sample struct {
		platform model.Platform
		seed     int32
		ja3Hash  string
		ja4      string
		akamai   string
		ua       string
	}
	var samples []sample

	for platform, seeds := range seedsByPlatform(t) {
		seed := seeds[0]
		fp, args := probeProfile(t, seed, geo)
		res, err := (&Scorer{ExecPath: k.ExecPath, Sites: []Site{tlsProbeSite}}).
			Run(context.Background(), args)
		if err != nil {
			t.Fatalf("种子 %d 查询失败: %v", seed, err)
		}
		if len(res) == 0 || res[0].Err != "" {
			t.Fatalf("种子 %d 采集失败: %v", seed, res)
		}
		m := res[0].Metrics
		s := sample{
			platform: platform, seed: seed,
			ja3Hash: str(m["ja3Hash"]), ja4: str(m["ja4"]),
			akamai: str(m["akamaiHash"]), ua: str(m["userAgent"]),
		}
		samples = append(samples, s)
		_ = fp

		t.Logf("声称 %s（种子 %d）:", platform, seed)
		t.Logf("  UA        %s", s.ua)
		t.Logf("  JA3 hash  %s", s.ja3Hash)
		t.Logf("  JA4       %s", s.ja4)
		t.Logf("  Akamai H2 %s", s.akamai)
	}

	if len(samples) < 2 {
		t.Skip("可用的声称系统不足 2 种，无法比较")
	}

	// 汇总：相同的 TLS 指纹意味着 TLS 层无法区分 profile。
	ja3Set := map[string][]string{}
	ja4Set := map[string][]string{}
	for _, s := range samples {
		ja3Set[s.ja3Hash] = append(ja3Set[s.ja3Hash], string(s.platform))
		ja4Set[s.ja4] = append(ja4Set[s.ja4], string(s.platform))
	}

	t.Logf("\n结论:")
	t.Logf("  %d 种声称系统产出 %d 个不同的 JA3 hash", len(samples), len(ja3Set))
	t.Logf("  %d 种声称系统产出 %d 个不同的 JA4", len(samples), len(ja4Set))
	if len(ja3Set) == 1 {
		for h, platforms := range ja3Set {
			t.Logf("  全部相同: %s ← %v", h, platforms)
		}
		t.Log("  含义: TLS 指纹不随 --fingerprint-platform 变化。声称 macOS 的 profile" +
			"在 TLS 层与声称 Windows 的完全一致，做 TLS+JS 交叉验证的平台能看到该矛盾。")
	} else {
		t.Log("  含义: TLS 指纹随声称系统变化，与预期不符——" +
			"内核可能已开始按平台调整 ClientHello，值得进一步核实。")
	}

	// UA 里的系统声明应当与 profile 一致，据此确认样本没搞混。
	for _, s := range samples {
		want := map[model.Platform]string{
			model.PlatformWindows: "Windows",
			model.PlatformMacOS:   "Macintosh",
			model.PlatformLinux:   "Linux",
		}[s.platform]
		if want != "" && !strings.Contains(s.ua, want) {
			t.Errorf("声称 %s 的样本 UA 中不含 %q: %s", s.platform, want, s.ua)
		}
	}
}

// 同一 profile 重复采集，判断 JA3 差异来自随机化还是按平台区分。
//
// 必要性：跨平台采集显示 JA3 不同而 JA4 相同，无法据此断定内核按声称的
// 系统调整了 ClientHello——GREASE（RFC 8701）会在密码套件与扩展列表里插入
// 随机值，本身就会让 JA3 每次连接都变，而 JA4 对此做了归一化。
//
// 判据：同一 profile 三次采集若 JA3 各不相同，差异源于随机化，
// 跨平台的"不同"是假象；若稳定不变，才说明 TLS 指纹真的随平台变化。
func TestReportTLSFingerprintStability(t *testing.T) {
	requireEnv(t, "BW_PROBE_TLS")
	k := realKernel(t)
	geo := &model.Geo{CountryCode: "US", Timezone: "America/Los_Angeles", Locale: "en-US"}

	// 固定种子，固定声称系统，只重复采集。
	const seed int32 = 1000
	var ja3s, ja4s, akamais []string

	for i := 0; i < 3; i++ {
		_, args := probeProfile(t, seed, geo)
		res, err := (&Scorer{ExecPath: k.ExecPath, Sites: []Site{tlsProbeSite}}).
			Run(context.Background(), args)
		if err != nil {
			t.Fatalf("第 %d 次采集失败: %v", i+1, err)
		}
		if len(res) == 0 || res[0].Err != "" {
			t.Fatalf("第 %d 次采集失败: %v", i+1, res)
		}
		m := res[0].Metrics
		ja3, ja4, ak := str(m["ja3Hash"]), str(m["ja4"]), str(m["akamaiHash"])
		ja3s = append(ja3s, ja3)
		ja4s = append(ja4s, ja4)
		akamais = append(akamais, ak)
		t.Logf("第 %d 次: JA3=%s", i+1, ja3)
		t.Logf("        JA4=%s", ja4)
		t.Logf("        Akamai=%s", ak)
	}

	uniq := func(vals []string) int {
		set := map[string]bool{}
		for _, v := range vals {
			set[v] = true
		}
		return len(set)
	}
	nJA3, nJA4, nAk := uniq(ja3s), uniq(ja4s), uniq(akamais)

	t.Logf("\n同一 profile 三次采集:")
	t.Logf("  JA3    %d 个不同值", nJA3)
	t.Logf("  JA4    %d 个不同值", nJA4)
	t.Logf("  Akamai %d 个不同值", nAk)

	switch {
	case nJA3 > 1:
		t.Log("\n结论: JA3 在同一 profile 内就会变化，说明它受 GREASE 随机化影响。")
		t.Log("      跨平台采集看到的 JA3 差异是随机化造成的假象，")
		t.Log("      不能据此认为 TLS 指纹随声称的系统变化。")
		if nJA4 == 1 {
			t.Log("      JA4 保持稳定，符合其对随机化做归一化的设计——")
			t.Log("      因此判断多账号是否会被 TLS 层关联，应当看 JA4 而非 JA3。")
		}
	case nJA4 == 1 && nJA3 == 1:
		t.Log("\n结论: JA3 与 JA4 在同一 profile 内都稳定。")
		t.Log("      结合跨平台采集中 JA3 不同的观测，说明内核确实按声称的系统")
		t.Log("      调整了 ClientHello——这与既有判断相反，值得进一步核实。")
	}
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// requireEnv 在指定环境变量未设为 1 时跳过测试。
// 用于需要联网或会开关浏览器窗口的探测型测试。
func requireEnv(t *testing.T, key string) {
	t.Helper()
	if os.Getenv(key) != "1" {
		t.Skipf("未设置 %s=1，跳过", key)
	}
}
