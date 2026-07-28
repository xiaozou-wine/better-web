package probe

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// 跑分采集为什么用扩展而不是 CDP 或跨窗口读 DOM：
//
//   - CDP：启用即被检测。CreepJS 明确检测 CDP 痕迹，用它跑分等于在测量前
//     先污染样本。
//   - 跨窗口读 DOM：目标站与本地页不同源，同源策略会拦住 document 访问。
//   - 本地代理目标站使其同源：会把 https 降级为 http 并改变 origin，
//     多个检测项的结果会随之改变，测出来的分数没有参考价值。
//
// 内容脚本共享页面 DOM 且不需要 CDP，是这几个选项里对测量干扰最小的。
// 它默认运行在隔离世界（isolated world），只共享 DOM 不共享 JS 变量，
// 因此提取表达式只能读 DOM——这对我们够用，各站的评分都渲染进了 DOM。

// extensionFiles 生成采集扩展的全部文件内容。
//
// reportURL 是本地收集服务的上报地址。扩展需要在 host_permissions 中
// 声明它，否则内容脚本的 fetch 会被 CORS 拦掉。
func extensionFiles(site Site, reportURL string) map[string]string {
	urlJSON, _ := json.Marshal(site.URL)
	exprJSON, _ := json.Marshal(site.Extract)
	nameJSON, _ := json.Marshal(site.Name)
	reportJSON, _ := json.Marshal(reportURL)

	patterns := site.MatchPatterns
	if len(patterns) == 0 {
		patterns = []string{matchPattern(site.URL)}
	}
	matchJSON, _ := json.Marshal(patterns)

	manifest := strings.NewReplacer(
		"__MATCHES__", string(matchJSON),
	).Replace(manifestTemplate)

	// 提取逻辑必须作为真实代码内联，不能用 new Function 求值字符串：
	// MV3 的内容安全策略不允许 unsafe-eval，运行时会直接抛错。
	ready := site.ReadyCheck
	if ready == "" {
		// 未提供就绪判断时立即视为就绪，退化成固定等待。
		ready = "return true"
	}
	content := strings.NewReplacer(
		"__URL__", string(urlJSON),
		"__EXTRACT_BODY__", site.Extract,
		"__READY_BODY__", ready,
		"__NAME__", string(nameJSON),
		"__REPORT__", string(reportJSON),
		"__SETTLE__", fmt.Sprint(site.SettleMs),
	).Replace(contentScriptTemplate)
	_ = exprJSON // 表达式以代码形式内联，不再需要其 JSON 编码

	return map[string]string{
		"manifest.json": manifest,
		"content.js":    content,
	}
}

// writeExtension 把扩展写到 dir 下，返回该目录。
func writeExtension(dir string, site Site, reportURL string) (string, error) {
	extDir := filepath.Join(dir, "collector-ext")
	if err := os.MkdirAll(extDir, 0o700); err != nil {
		return "", fmt.Errorf("创建扩展目录失败: %w", err)
	}
	for name, content := range extensionFiles(site, reportURL) {
		path := filepath.Join(extDir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			return "", fmt.Errorf("写入扩展文件 %s 失败: %w", name, err)
		}
	}
	return extDir, nil
}

// matchPattern 把站点 URL 转成内容脚本的匹配模式。
// 只匹配该站点，避免内容脚本注入到无关页面干扰测量。
// 站点需要跨域采集时用 Site.MatchPatterns 显式指定，不走这里。
func matchPattern(rawURL string) string {
	// 取到主机名结束的部分，后接 /* 覆盖其全部路径。
	s := rawURL
	for _, prefix := range []string{"https://", "http://"} {
		if strings.HasPrefix(s, prefix) {
			rest := s[len(prefix):]
			if i := strings.IndexByte(rest, '/'); i >= 0 {
				rest = rest[:i]
			}
			return prefix + rest + "/*"
		}
	}
	return "<all_urls>"
}

// manifestTemplate 是采集扩展的 manifest。
//
// 用 MV3：Chromium 148 已不再加载 MV2 扩展。
// run_at 用 document_idle，随后由脚本自行等待评分算完。
const manifestTemplate = `{
  "manifest_version": 3,
  "name": "better-web score collector",
  "version": "1.0",
  "description": "采集检测站点的评分结果，仅供本地跑分使用。",
  "host_permissions": ["http://127.0.0.1/*"],
  "content_scripts": [
    {
      "matches": __MATCHES__,
      "js": ["content.js"],
      "run_at": "document_start",
      "all_frames": false
    }
  ]
}
`

// contentScriptTemplate 是注入到检测页的内容脚本。
//
// 提取逻辑以真实代码内联（__EXTRACT_BODY__），不走 new Function：
// MV3 禁用 unsafe-eval，求值字符串会被 CSP 拦掉。
//
// 表达式只能读 DOM：内容脚本运行在隔离世界，读不到页面的 JS 变量。
// 各检测站的评分都渲染进了 DOM，这个限制不影响采集。
const contentScriptTemplate = `'use strict'
;(async () => {
  const TARGET = __URL__
  const NAME = __NAME__
  const REPORT = __REPORT__
  const SETTLE = __SETTLE__

  function report(body) {
    return fetch(REPORT, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    }).catch(() => {})
  }

  // 捕获页面自身的脚本错误。评分算不完时，多数情况是页面里抛了异常
  // 而非"还没算完"，没有这个信息只能靠猜。
  const pageErrors = []
  window.addEventListener('error', (e) => {
    pageErrors.push(String((e && e.message) || e).slice(0, 200))
  })
  window.addEventListener('unhandledrejection', (e) => {
    const r = e && e.reason
    pageErrors.push('unhandled: ' + String((r && r.message) || r).slice(0, 200))
  })

  async function extract() {
    __EXTRACT_BODY__
  }

  // isReady 判断评分是否已算完。各站点的就绪信号不同，由 Site.ReadyCheck 提供。
  async function isReady() {
    __READY_BODY__
  }

  // 评分是异步算出来的，过早读取会拿到中间态（如 "FP ID: Computing..."）。
  // 固定等待不可靠：耗时随机器负载与网络波动，实测 22 秒处于临界。
  // 改为轮询就绪信号，出现后再给一段缓冲让剩余项收敛。
  const startedAt = Date.now()
  const deadline = startedAt + SETTLE
  let settled = false
  let polls = 0
  while (Date.now() < deadline) {
    polls++
    let ok = false
    try {
      ok = await isReady()
    } catch (e) {
      ok = false
    }
    if (ok) {
      settled = true
      break
    }
    await new Promise((r) => setTimeout(r, 500))
  }
  const settleMs = Date.now() - startedAt
  if (settled) {
    await new Promise((r) => setTimeout(r, 3000))
  }

  try {
    const metrics = await extract()
    if (metrics && typeof metrics === 'object') {
      metrics.__settled = settled
      // 记录等待耗时与轮询次数：判断超时是"确实需要更久"还是"永远不会就绪"。
      metrics.__settleMs = settleMs
      metrics.__polls = polls
      metrics.__readyState = document.readyState
      metrics.__pageErrors = pageErrors.slice(0, 5)
    }
    await report({ metrics: metrics })
  } catch (e) {
    await report({ err: NAME + ' 提取失败: ' + (e && e.message ? e.message : String(e)) })
  }
})()
`
