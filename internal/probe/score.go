package probe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Site 是一个检测站点及其结果提取规则。
type Site struct {
	// Name 是站点标识，用于结果归档。
	Name string
	// URL 是检测页地址。
	URL string
	// Extract 是在页面内执行的 JS 语句，需 return 一个对象。
	// 它会被内联进 async 函数体，因此可以 await。
	Extract string
	// ReadyCheck 是判断评分是否算完的 JS 语句，需 return 布尔值。
	// 同样内联进 async 函数体。留空表示不做判断，直接等满 SettleMs。
	//
	// 必要性：这些站点的评分异步计算，耗时随机器负载与网络波动。
	// 固定等待要么读到中间态（如 "Computing..."），要么白等。
	ReadyCheck string
	// SettleMs 是等待评分算完的时间上限。
	SettleMs int
	// MatchPatterns 覆盖内容脚本的注入范围。留空时由 URL 推导出本站一条模式。
	//
	// 需要覆盖的场景：风控站点判定为机器人后会把整页导航到另一个域
	// （DataDome 用 geo.captcha-delivery.com）。只注入原域名会让内容脚本
	// 随原页面一起卸载，采集侧只看到超时，读不出"被拦了"这个结论——
	// 而那正是要测的东西。
	MatchPatterns []string
}

// ScoreResult 是一个站点的跑分结果。
type ScoreResult struct {
	Site string `json:"site"`
	URL  string `json:"url"`
	// Metrics 是从页面提取的指标，键名由各站点的 Extract 决定。
	Metrics map[string]any `json:"metrics"`
	// Err 非空表示该站点采集失败，其余站点不受影响。
	Err string `json:"err,omitempty"`
	// ElapsedMs 是本站点采集耗时。
	ElapsedMs int64 `json:"elapsedMs"`
}

// CreepJS 提取 CreepJS 的核心判定指标。
//
// 三个 rating 的含义（数值越低越好）：
//   - likeHeadless: 有多少项特征"像"无头浏览器
//   - headless: 有多少项特征确证为无头
//   - stealth: 有多少项特征暴露了刻意隐藏的痕迹（伪造做得太糙会拉高它）
//
// lies 是被判定为"撒谎"的指纹项数量：伪造与其他特征矛盾时会被计入。
// 这项对我们最关键——它直接衡量伪造的自洽程度。
var CreepJS = Site{
	Name: "creepjs",
	URL:  "https://abrahamjuliot.github.io/creepjs/",
	// 上限放宽到 60 秒：CreepJS 项目多，慢速代理下 22 秒不够，
	// 实际耗时由 ReadyCheck 决定，不会白等。
	SettleMs: 60000,
	// 就绪条件：rating 已渲染，且页面上再无 "Computing..." 占位符。
	//
	// 不能盯 #creep-fingerprint：那是初始占位元素，算完后 CreepJS 用一个
	// 不带该 id 的 .ellipsis-all div 整体替换它，占位符里的 "Computing..."
	// 永远不会变，盯它会一直判为未就绪。
	ReadyCheck: `
	  if (!document.querySelector('.like-headless-rating')) return false
	  const fpRow = Array.from(document.querySelectorAll('.ellipsis-all'))
	    .find((el) => /FP ID:/.test(el.textContent || ''))
	  if (!fpRow) return false
	  return !/computing/i.test(fpRow.textContent || '')`,
	Extract: `
	  const num = (sel) => {
	    const el = document.querySelector(sel)
	    if (!el) return null
	    const m = (el.textContent || '').match(/(\d+(?:\.\d+)?)\s*%/)
	    return m ? parseFloat(m[1]) : null
	  }
	  const txt = (sel) => {
	    const el = document.querySelector(sel)
	    return el ? (el.textContent || '').replace(/\s+/g, ' ').trim().slice(0, 200) : null
	  }
	  // bold-fail 只可能出现在这 6 个维度上（CreepJS 的 LowerEntropy 对象）。
	  // 与其从 DOM 结构猜维度名（区块布局易变，且整行混有哈希与耗时噪声），
	  // 不如按已知维度反查：找出所在区块提到了哪个维度。
	  const DIMENSIONS = ['audio', 'canvas', 'webgl', 'fonts', 'screen', 'timezone', 'intl']
	  const labels = (sel) =>
	    Array.from(document.querySelectorAll(sel)).map((el) => {
	      let box = el
	      // 向上找到包含维度名的祖先区块。
	      for (let i = 0; i < 6 && box; i++) {
	        const t = (box.textContent || '').toLowerCase()
	        const hit = DIMENSIONS.find((d) => t.includes(d))
	        if (hit) return hit
	        box = box.parentElement
	      }
	      return 'unknown'
	    })
	  return {
	    likeHeadlessPct: num('.like-headless-rating'),
	    headlessPct: num('.headless-rating'),
	    stealthPct: num('.stealth-rating'),
	    liesCount: document.querySelectorAll('.lies').length,
	    lies: labels('.lies'),
	    // bold-fail 标记低熵维度，取值范围为 AUDIO/CANVAS/FONTS/SCREEN/
	    // TIME_ZONE/WEBGL 六项之一。低熵意味着该维度可区分度不足。
	    boldFailCount: document.querySelectorAll('.bold-fail').length,
	    lowEntropy: labels('.bold-fail'),
	    fingerprintId: (() => {
	      const row = Array.from(document.querySelectorAll('.ellipsis-all'))
	        .find((el) => /FP ID:/.test(el.textContent || ''))
	      return row ? row.textContent.replace(/\s+/g, ' ').trim().slice(0, 120) : null
	    })(),
	    // 逐项列出命中的 like-headless / headless / stealth 判定项。
	    //
	    // CreepJS 把每项的布尔值渲染进 modal 内容（"键: true/false" 形式），
	    // 且 modal 靠纯 CSS 显隐，内容始终在 DOM 里，无需点击即可读取。
	    // 百分比只说明命中比例，逐项名称才能定位到具体要改什么。
	    headlessFlags: (() => {
	      const hits = { likeHeadless: [], headless: [], stealth: [] }
	      const sections = [
	        ['likeHeadless', 'Like Headless'],
	        ['headless', 'Headless'],
	        ['stealth', 'Stealth'],
	      ]
	      // 明细在 .modal-content 里，各项之间没有换行分隔，因此不能按行解析，
	      // 只能全局扫 "键: true|false" 模式。标题用于确定归属哪个分区。
	      const boxes = Array.from(document.querySelectorAll('.modal-content'))
	        .map((el) => (el.innerText || el.textContent || '').replace(/\s+/g, ' '))
	        .filter((t) => /:\s*(true|false)/.test(t))
	      for (const [key, title] of sections) {
	        // 标题需精确匹配到分区起始：Headless 是 Like Headless 的子串，
	        // 因此从最长标题优先匹配的分区里挑，用 × 前缀锚定标题位置。
	        const box = boxes.find((t) => t.includes('× ' + title))
	        if (!box) continue
	        const seg = box.slice(box.indexOf('× ' + title))
	        const re = /([A-Za-z][A-Za-z0-9]*)\s*:\s*(true|false)/g
	        let m
	        while ((m = re.exec(seg)) !== null) {
	          if (m[2] === 'true') hits[key].push(m[1])
	        }
	      }
	      return hits
	    })(),
	    fuzzyFingerprint: txt('#fuzzy-fingerprint .fuzzy-fp'),
	    rendered: !!document.querySelector('.like-headless-rating'),
	  }`,
}

// BrowserScan 提取 browserscan.net 的机器人检测结论。
// 该站把结论渲染成中文或英文文本，因此只取整块文本让人工判读，
// 避免依赖易变的内部结构。
var BrowserScan = Site{
	Name:     "browserscan",
	URL:      "https://www.browserscan.net/bot-detection",
	SettleMs: 45000,
	// 就绪条件：结论容器里已填入非空文本。
	// 静态 HTML 中该容器只有标签和一个占位 svg，结论由 JS 填入。
	ReadyCheck: `
	  const label = Array.from(document.querySelectorAll('strong'))
	    .find((el) => /Test Results:/i.test(el.textContent || ''))
	  if (!label || !label.parentElement) return false
	  const v = (label.parentElement.textContent || '')
	    .replace(/Test Results:/i, '').trim()
	  return v.length > 0`,
	// 只解析 "Test Results:" 后面紧跟的结论词。
	//
	// 不能对整页正文做关键词匹配：该页的说明文字本身就含 "robot"
	// （"whether the browser environment is controlled by a robot"），
	// 整页匹配会让结论词和说明文字同时命中，判定失去意义。
	Extract: `
	  // 结论渲染在 "Test Results:" 标签所在的那个容器里，紧随其后的
	  // <ul> 是分区列表（Webdriver / User-Agent / CDP / Navigator），
	  // 不是结论。对扁平化正文用正则会把分区名误当结论。
	  const label = Array.from(document.querySelectorAll('strong'))
	    .find((el) => /Test Results:/i.test(el.textContent || ''))
	  let verdict = null
	  if (label && label.parentElement) {
	    verdict = (label.parentElement.textContent || '')
	      .replace(/Test Results:/i, '')
	      .replace(/\s+/g, ' ')
	      .trim()
	      .slice(0, 60) || null
	  }
	  const flat = (document.body ? document.body.innerText : '')
	    .replace(/\s+/g, ' ').trim()
	  return {
	    // 该站用 Normal / 正常 表示未被识别为自动化环境。
	    verdict: verdict,
	    passed: !!verdict && /^(normal|正常)$/i.test(verdict),
	    excerpt: flat.slice(0, 400),
	  }`,
}

// 商业风控实测：与 CreepJS/BrowserScan 的区别在于判据不同。
//
// 那两个站输出的是"环境自洽性评分"，属于自查工具，判定逻辑公开且不带商业
// 后果。Cloudflare 与 DataDome 输出的是"放行或拦截"这个二元结果，判定逻辑
// 不公开、按 IP 信誉与行为持续调整。因此这里唯一可靠的信号是**页面到底加载
// 出来了没有**，而不是页面上写了什么分数。
//
// 提取策略：优先认目标页自己的正向成功标记，其次记录挑战页仍在的证据。
// 不用"没命中挑战特征就算通过"——那个方向实测会误判，原因见 cfSuccessMarker。
//
// 结果的边界要认清：这些系统对同一浏览器的判定会随出口 IP 信誉与累计访问
// 频次浮动。单次通过不代表指纹过关，可能只是这个 IP 当时干净；单次拦截也
// 不代表指纹有问题，可能只是该 IP 已被限流。因此本文件的站点必须与
// TestAntibotControl 的裸跑对照组一起用：只有"裸跑通过而伪造被拦"才能把
// 责任归到指纹上。

// cfSuccessMarker 是 scrapingcourse 挑战页通过后注入的成功标记。
//
// 用正向标记判定，而不是枚举挑战页特征取反。第一版用的是后者，实测被误判：
// Accept-Language 按代理出口设为 de-DE 后，Cloudflare 返回德语挑战页
// （title "Nur einen Moment…"、正文 "Sicherheitsüberprüfung wird
// durchgeführt"），英文文案全部落空；同时该页也不再用 #challenge-running
// 那套 id，元素判据一并落空，于是"没命中任何挑战特征"被当成了通过。
//
// 教训是判据的方向问题：挑战页的形态由对方控制、会本地化也会改版，穷举必然
// 漏；而成功标记是目标页自己的内容，只在真正通过后才存在。宁可漏报通过，
// 不可误报通过——后者会让人以为能过 Cloudflare 而实际没过。
const cfSuccessMarker = "You bypassed the Cloudflare challenge"

// CloudflareChallenge 用 scrapingcourse 的托管挑战页测 Cloudflare。
//
// 选它而不是随便找个 Cloudflare 站点：它是专门为测试搭的，挑战常开，
// 通过后有明确的成功文案，且测它不给真实业务站带去无关流量。
// 实测裸 curl 请求该页返回 403 且带 Cf-Mitigated: challenge，
// 说明挑战确实生效，具备判别力。
var CloudflareChallenge = Site{
	Name:     "cloudflare-challenge",
	URL:      "https://www.scrapingcourse.com/cloudflare-challenge",
	SettleMs: 45000,
	// 就绪条件：出现成功标记（通过），或出现硬拦截文案（失败）。
	// 都没出现就一直等到超时——停在挑战页本身即为未通过，
	// 由 Extract 记录当时的页面状态。
	ReadyCheck: `
	  const flat = (document.body ? (document.body.innerText || '') : '')
	  if (flat.includes('` + cfSuccessMarker + `')) return true
	  return /Sorry, you have been blocked|Error 1020/.test(flat)`,
	// 判据是成功标记在不在。同时记录挑战页痕迹，用于区分
	// "停在挑战页"（未通过）与"页面没加载出来"（链路问题）。
	Extract: `
	  const flat = (document.body ? (document.body.innerText || '') : '')
	    .replace(/\s+/g, ' ').trim()
	  const passed = flat.includes('` + cfSuccessMarker + `')
	  // Ray ID 与 Turnstile iframe 是挑战页仍在的证据，与语言无关。
	  const stuck = []
	  if (/Ray ID:/.test(flat)) stuck.push('rayId')
	  if (document.querySelector('iframe[src*="challenges.cloudflare.com"]'))
	    stuck.push('turnstileFrame')
	  if (document.querySelector('#challenge-form, #challenge-stage'))
	    stuck.push('challengeForm')
	  const blocked = ['Sorry, you have been blocked', 'Error 1020']
	    .filter((t) => flat.includes(t))
	  return {
	    finalURL: location.href,
	    title: (document.title || '').slice(0, 120),
	    passed: passed,
	    challengeSelectors: stuck,
	    blockedTexts: blocked,
	    bodyLen: flat.length,
	    excerpt: flat.slice(0, 300),
	  }`,
}

// DataDome 用 DataDome 自己的站点测 DataDome。
//
// 为什么用它而不是 leboncoin/hermes 一类真实商业站：那些站的拦截同时受
// 业务风控策略影响，测出来分不清是指纹问题还是该站对中国/机房 IP 的地域
// 策略。用厂商自己的官网，命中的是其通用规则。
// 实测裸 curl 返回 403 且带 x-datadome: protected 与 x-dd-b: 1，
// 说明防护在线且会拦非浏览器客户端。
//
// 该站的结论不可单独作为指纹判据。实测（2026-07-27，直连同一出口）：
// 内核裸跑组连续三轮通过，此后同一组转为被拦到 captcha 域并保持——
// 期间浏览器参数完全没变，变化的只是这个 IP 的累计访问次数。
// DataDome 按 IP 维度累积请求频次判定，短时间连续跑分本身就会触发。
//
// 因此解读规则是：只有在同批次内"裸跑组通过、伪造组被拦"时，才能归因到
// 指纹；两组都被拦时只能得出"该 IP 已被限流"，需换出口或隔开时间重测。
// 这也是必须保留裸跑对照组的原因（见 TestAntibotControl）。
//
// 关键实现点：DataDome 判定为机器人时会整页跳转到 geo.captcha-delivery.com，
// 因此 MatchPatterns 必须带上该域，否则内容脚本随原页卸载，只能观察到超时。
var DataDome = Site{
	Name:     "datadome",
	URL:      "https://datadome.co/",
	SettleMs: 45000,
	MatchPatterns: []string{
		"https://datadome.co/*",
		"https://*.datadome.co/*",
		"https://geo.captcha-delivery.com/*",
		"https://*.captcha-delivery.com/*",
	},
	// 就绪条件：已跳到 captcha 域（终态失败），或本域页面有实质内容（终态通过）。
	ReadyCheck: `
	  if (/captcha-delivery\.com/.test(location.host)) return true
	  const flat = (document.body ? (document.body.innerText || '') : '')
	    .replace(/\s+/g, ' ').trim()
	  return flat.length > 300`,
	Extract: `
	  const flat = (document.body ? (document.body.innerText || '') : '')
	    .replace(/\s+/g, ' ').trim()
	  // 落在 captcha 域即为被判定成机器人，这是最硬的失败信号。
	  const onCaptcha = /captcha-delivery\.com/.test(location.host)
	  const CHALLENGE_TEXT = [
	    'Please enable JS and disable any ad blocker',
	    'verify you are a human', 'Vous devez activer',
	  ]
	  const hitText = CHALLENGE_TEXT.filter((t) => flat.includes(t))
	  // DataDome 的 challenge 页会挂 captcha iframe。
	  const hitSel = ['iframe[src*="captcha-delivery.com"]', '#captcha__frame']
	    .filter((s) => document.querySelector(s))
	  return {
	    finalURL: location.href,
	    finalHost: location.host,
	    title: (document.title || '').slice(0, 120),
	    onCaptchaDomain: onCaptcha,
	    challengeTexts: hitText,
	    challengeSelectors: hitSel,
	    passed: !onCaptcha && hitText.length === 0 && hitSel.length === 0 &&
	      flat.length > 300,
	    bodyLen: flat.length,
	    excerpt: flat.slice(0, 300),
	  }`,
}

// DefaultSites 是默认跑分的站点集合。
var DefaultSites = []Site{CreepJS, BrowserScan}

// AntibotSites 是商业风控站点集合，与 DefaultSites 分开。
//
// 不并入 DefaultSites 的理由：这两项的结果受出口 IP 信誉影响，
// 会让本该稳定的内核回归门禁变得抖动。它们测的是"这条链路当前能不能过"，
// 属于按需验证，不是内核升级回归项。
var AntibotSites = []Site{CloudflareChallenge, DataDome}

// Scorer 用真实内核在检测站点上跑分。
type Scorer struct {
	// ExecPath 是内核可执行文件路径。
	ExecPath string
	// Sites 是要跑的站点。留空时用 DefaultSites。
	Sites []Site
}

// scoreTimeout 是单站点的总超时，须大于其 SettleMs 加上页面加载时间。
const scoreTimeout = 90 * time.Second

// Run 依次在各站点跑分。
//
// extraArgs 通常来自 launcher.BuildArgs，须包含代理参数——这些站点在墙内
// 直连不通，且检测结果本身也应当在目标出口环境下测量。
//
// 每个站点单独启动一次内核：共用实例会让前一站点的状态影响后续判定。
// 单站点失败不影响其余站点，失败原因记录在对应 ScoreResult.Err 中。
func (s *Scorer) Run(ctx context.Context, extraArgs []string) ([]ScoreResult, error) {
	if s.ExecPath == "" {
		return nil, errors.New("未指定内核路径")
	}
	sites := s.Sites
	if len(sites) == 0 {
		sites = DefaultSites
	}

	out := make([]ScoreResult, 0, len(sites))
	for _, site := range sites {
		started := time.Now()
		metrics, err := s.runSite(ctx, site, extraArgs)
		res := ScoreResult{
			Site: site.Name, URL: site.URL, Metrics: metrics,
			ElapsedMs: time.Since(started).Milliseconds(),
		}
		if err != nil {
			res.Err = err.Error()
		}
		out = append(out, res)
	}
	return out, nil
}

// runSite 在单个站点上采集指标。
//
// 实现方式：起一个本地收集页，它在新标签打开目标站点，等评分算完后跨
// 窗口读取 DOM 并回传。不用 CDP，因为启用 CDP 本身就是可被检测的信号，
// 会污染跑分结果。
func (s *Scorer) runSite(ctx context.Context, site Site, extraArgs []string) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, scoreTimeout)
	defer cancel()

	// 容量给到能容下跨域跳转带来的多次上报：跳转前后各一次，
	// 再留余量。容量不足时 handler 的 default 分支会丢弃后到的上报，
	// 而后到的那份往往才是真实结局（见 confirm）。
	results := make(chan map[string]any, 8)
	failures := make(chan error, 1)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("启动收集服务失败: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/report", func(w http.ResponseWriter, r *http.Request) {
		// 内容脚本跨源上报，需放行 CORS 预检与实际请求。
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		var payload struct {
			Metrics map[string]any `json:"metrics"`
			Err     string         `json:"err"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&payload); err != nil {
			failures <- fmt.Errorf("解析跑分结果失败: %w", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		if payload.Err != "" {
			failures <- errors.New(payload.Err)
			return
		}
		select {
		case results <- payload.Metrics:
		default:
		}
	})

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			failures <- err
		}
	}()
	defer func() {
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutCancel()
		_ = srv.Shutdown(shutCtx)
	}()

	dataDir, err := os.MkdirTemp("", "bw-score-*")
	if err != nil {
		return nil, fmt.Errorf("创建临时 profile 目录失败: %w", err)
	}
	defer func() { _ = os.RemoveAll(dataDir) }()

	reportURL := "http://" + ln.Addr().String() + "/report"
	extDir, err := writeExtension(dataDir, site, reportURL)
	if err != nil {
		return nil, err
	}

	args := append([]string{}, extraArgs...)
	if !hasUserDataDir(args) {
		args = append(args, "--user-data-dir="+dataDir)
	}
	args = append(args,
		"--no-first-run",
		"--no-default-browser-check",
		// 加载采集扩展。内容脚本共享页面 DOM 且无需 CDP，
		// 是对测量干扰最小的注入方式。
		"--disable-extensions-except="+extDir,
		"--load-extension="+extDir,
		"--window-size=1400,900",
		// 窗口被其他窗口遮挡或最小化时，Chromium 会节流后台标签的渲染与
		// 定时器，导致评分长时间算不完（实测约 40% 的运行会卡住）。
		// 这几个开关关掉遮挡检测与后台节流，让评分稳定跑完。
		//
		// 仅用于采集，不要加进 launcher 的生产启动路径：禁用后台节流本身
		// 就是偏离 Chrome 默认行为的可检测差异。
		"--disable-backgrounding-occluded-windows",
		"--disable-renderer-backgrounding",
		"--disable-background-timer-throttling",
		site.URL,
	)

	cmd := exec.CommandContext(ctx, s.ExecPath, args...)
	setProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动内核失败: %w", err)
	}
	defer func() { reapKernel(cmd) }()

	select {
	case m := <-results:
		return s.confirm(ctx, site, m, results), nil
	case err := <-failures:
		return nil, err
	case <-ctx.Done():
		return nil, fmt.Errorf("跑分超时: %w", ctx.Err())
	}
}

// redirectGrace 是收到"通过"结论后，继续等待反驳性上报的时间。
const redirectGrace = 5 * time.Second

// confirm 处理跨域跳转站点的多次上报。
//
// 必要性：站点声明多个匹配域时（如 DataDome 的原域 + captcha 域），
// 跳转前后两个页面各自注入内容脚本、各自上报。若原页面在跳转发生前
// 恰好满足了就绪条件，它会先上报一个"通过"，而真实结局是被拦到 captcha 域。
// 采信先到的那份就会得出与事实相反的结论——这在本测试里是最坏的错误：
// 它会让人以为指纹能过 DataDome，而实际上没过。
//
// 因此收到"通过"后再等一段时间：若期间有页面报回"未通过"，采信后者。
// 单域站点（CreepJS 等）不会有第二次上报，直接返回，不付这份等待成本。
func (s *Scorer) confirm(ctx context.Context, site Site,
	first map[string]any, more <-chan map[string]any) map[string]any {
	if len(site.MatchPatterns) <= 1 {
		return first
	}
	if passed, ok := first["passed"].(bool); !ok || !passed {
		return first // 已是"未通过"，不会再被更坏的结论推翻
	}
	timer := time.NewTimer(redirectGrace)
	defer timer.Stop()
	for {
		select {
		case next := <-more:
			if passed, ok := next["passed"].(bool); ok && !passed {
				next["__supersededPass"] = true
				return next
			}
		case <-timer.C:
			return first
		case <-ctx.Done():
			return first
		}
	}
}

// SaveScores 把跑分结果写成缩进 JSON，便于纳入版本管理做趋势比对。
func SaveScores(path string, results []ScoreResult) error {
	b, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

// Summary 返回一行可读摘要，供日志输出。
func (r ScoreResult) Summary() string {
	if r.Err != "" {
		return fmt.Sprintf("%s: 失败（%s）", r.Site, r.Err)
	}
	parts := make([]string, 0, len(r.Metrics))
	for _, k := range []string{
		"likeHeadlessPct", "headlessPct", "stealthPct", "liesCount",
		"boldFailCount", "verdict", "passed",
		// 商业风控站的判定字段。
		"onCaptchaDomain", "challengeSelectors", "challengeTexts",
		"blockedTexts", "finalHost",
	} {
		if v, ok := r.Metrics[k]; ok && v != nil {
			parts = append(parts, fmt.Sprintf("%s=%v", k, v))
		}
	}
	if len(parts) == 0 {
		return fmt.Sprintf("%s: 无可读指标", r.Site)
	}
	return r.Site + ": " + strings.Join(parts, " ")
}
