package probe

import (
	"context"
	"os"
	"testing"

	"better-web/internal/fingerprint"
	"better-web/internal/geo"
	"better-web/internal/launcher"
	"better-web/internal/model"
	"better-web/internal/proxy"
)

// 诊断用：转储 CreepJS 页面在读取时刻的真实结构，用于定位选择器问题。
//
//	BW_TEST_PROXY=socks5://127.0.0.1:10808 BW_RUN_SCORE=1 \
//	  go test -run TestDebugCreepJSDOM -timeout 300s -v ./internal/probe/
func TestDebugCreepJSDOM(t *testing.T) {
	if os.Getenv("BW_RUN_SCORE") != "1" {
		t.Skip("未设置 BW_RUN_SCORE=1，跳过 DOM 诊断")
	}
	k := realKernel(t)
	args := proxiedArgs(t)

	debug := Site{
		Name:     "creepjs-debug",
		URL:      CreepJS.URL,
		SettleMs: 45000,
		// 就绪条件放宽：只要 rating 元素出现即读，用于观察此刻的真实状态。
		ReadyCheck: `return !!document.querySelector('.like-headless-rating')`,
		Extract: `
		  const ratingEls = Array.from(
		    document.querySelectorAll('.like-headless-rating, .headless-rating, .stealth-rating'))
		  const modalContents = Array.from(document.querySelectorAll('.modal-content'))
		  return {
		    ratingCount: ratingEls.length,
		    ratingTexts: ratingEls.map((el) =>
		      (el.textContent || '').replace(/\s+/g, ' ').trim().slice(0, 120)),
		    modalContentCount: modalContents.length,
		    // 找出含 "键: true/false" 明细的 modal，看它长什么样。
		    boolModals: modalContents
		      .map((el) => (el.innerText || el.textContent || '').replace(/\s+/g, ' ').trim())
		      .filter((s) => /:\s*(true|false)/.test(s))
		      .map((s) => s.slice(0, 200)),
		    modalClasses: Array.from(document.querySelectorAll('[class*="modal-creep"]'))
		      .map((el) => el.className).slice(0, 12),
		    // 定位 like-headless 明细：从 rating 元素出发看它的兄弟结构，
		    // 而不是猜 modal 的 class 层级。
		    ratingSiblings: (() => {
		      const el = document.querySelector('.like-headless-rating')
		      if (!el) return null
		      return {
		        ownHTML: (el.outerHTML || '').slice(0, 500),
		        parentHTML: el.parentElement
		          ? (el.parentElement.innerHTML || '').slice(0, 800) : null,
		      }
		    })(),
		    // 全页搜含 "noChrome"/"noPlugins" 等判定项名的元素。
		    flagHosts: Array.from(document.querySelectorAll('*'))
		      .filter((el) => el.children.length === 0 &&
		        /noChrome|noPlugins|hasPermissionsBug|prefersLightColor/.test(el.textContent || ''))
		      .map((el) => el.tagName + '.' + el.className + ' :: ' +
		        (el.textContent || '').replace(/\s+/g, ' ').trim().slice(0, 300))
		      .slice(0, 4),
		    fpIdRow: (() => {
		      const row = Array.from(document.querySelectorAll('.ellipsis-all'))
		        .find((el) => /FP ID:/.test(el.textContent || ''))
		      return row ? row.textContent.replace(/\s+/g, ' ').trim().slice(0, 100) : null
		    })(),
		  }`,
	}

	results, err := (&Scorer{ExecPath: k.ExecPath, Sites: []Site{debug}}).
		Run(context.Background(), args)
	if err != nil {
		t.Fatalf("诊断失败: %v", err)
	}
	r := results[0]
	if r.Err != "" {
		t.Fatalf("采集失败: %s", r.Err)
	}
	for key, val := range r.Metrics {
		t.Logf("%s = %v", key, val)
	}
}

// proxiedArgs 构造一份带代理与自动对齐地理信息的启动参数，供诊断用例复用。
func proxiedArgs(t *testing.T) []string {
	t.Helper()
	up := parseTestProxy(t)
	fwd, err := proxy.New(up)
	if err != nil {
		t.Fatalf("创建转发器失败: %v", err)
	}
	addr, err := fwd.Start()
	if err != nil {
		t.Fatalf("启动转发器失败: %v", err)
	}
	t.Cleanup(func() { _ = fwd.Close() })

	client, err := fwd.HTTPClient()
	if err != nil {
		t.Fatalf("构造代理客户端失败: %v", err)
	}
	resolved, err := geo.NewResolver(client).Lookup(context.Background())
	if err != nil {
		t.Fatalf("查询出口地失败: %v", err)
	}
	fp := fingerprint.Derive(20260727, &resolved)
	p := &model.Profile{
		ID: "dbg", Name: "dbg", Kind: model.KindFingerprint,
		Seed: fp.Seed, ProfileDir: t.TempDir(),
	}
	args, err := launcher.BuildArgs(p, &fp, addr, nil)
	if err != nil {
		t.Fatalf("BuildArgs 失败: %v", err)
	}
	return args
}
