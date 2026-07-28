/**
 * 交互验证：通过 CDP 驱动预览页，检查本次重构新增的交互是否真的工作。
 *
 * 覆盖三项：详情展开/收起、主题三态循环并持久化、溢出菜单能打开。
 * 用 Node 内置 fetch + WebSocket 直连 CDP，不引入 puppeteer 依赖。
 */
const CDP_PORT = 9333
const URL = 'http://localhost:5199/preview/index.html?theme=dark'

let nextId = 1
const pending = new Map()

function send(ws, method, params = {}) {
  const id = nextId++
  ws.send(JSON.stringify({ id, method, params }))
  return new Promise((resolve, reject) => {
    pending.set(id, { resolve, reject })
    setTimeout(() => reject(new Error(`CDP 超时: ${method}`)), 15000)
  })
}

/** 在页面里求值，返回 JSON 化的结果。 */
async function evaluate(ws, expr) {
  const r = await send(ws, 'Runtime.evaluate', {
    expression: expr,
    awaitPromise: true,
    returnByValue: true,
  })
  if (r.exceptionDetails) {
    throw new Error(`页面异常: ${r.exceptionDetails.text} ${JSON.stringify(r.result?.value ?? '')}`)
  }
  return r.result.value
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

async function main() {
  // 找到页面 target。
  const list = await (await fetch(`http://127.0.0.1:${CDP_PORT}/json/list`)).json()
  const page = list.find((t) => t.type === 'page' && t.url.includes('preview'))
  if (!page) throw new Error('找不到预览页 target；已开的页面: ' + list.map((t) => t.url).join(', '))

  const ws = new WebSocket(page.webSocketDebuggerUrl)
  ws.addEventListener('message', (ev) => {
    const msg = JSON.parse(ev.data)
    if (msg.id && pending.has(msg.id)) {
      const { resolve, reject } = pending.get(msg.id)
      pending.delete(msg.id)
      msg.error ? reject(new Error(JSON.stringify(msg.error))) : resolve(msg.result)
    }
  })
  await new Promise((r) => ws.addEventListener('open', r, { once: true }))
  await send(ws, 'Runtime.enable')

  const results = []
  const check = (name, ok, detail = '') => {
    results.push({ name, ok, detail })
    console.log(`  ${ok ? 'OK  ' : 'FAIL'} ${name}${detail ? ': ' + detail : ''}`)
  }

  console.log('[交互验证]')

  // 前置：确认列表已渲染。
  const cards = await evaluate(ws, `document.querySelectorAll('.card').length`)
  check('列表渲染出 profile 卡片', cards === 4, `${cards} 张`)

  // 1. 详情展开/收起。
  const detailBefore = await evaluate(ws, `document.querySelectorAll('.detail').length`)
  await evaluate(ws, `document.querySelector('.card .disclose').click()`)
  await sleep(150)
  const detailAfter = await evaluate(ws, `document.querySelectorAll('.detail').length`)
  const expandedAttr = await evaluate(
    ws,
    `document.querySelector('.card .disclose').getAttribute('aria-expanded')`,
  )
  check(
    '点击详情展开次要信息',
    detailBefore === 0 && detailAfter === 1 && expandedAttr === 'true',
    `${detailBefore} → ${detailAfter}, aria-expanded=${expandedAttr}`,
  )

  // 展开后应能读到种子等排障字段。
  const detailText = await evaluate(
    ws,
    `document.querySelector('.detail')?.textContent?.replace(/\\s+/g,' ').trim() ?? ''`,
  )
  check('详情含机型与种子', /机型/.test(detailText) && /种子/.test(detailText), detailText.slice(0, 70))

  await evaluate(ws, `document.querySelector('.card .disclose').click()`)
  await sleep(150)
  const detailClosed = await evaluate(ws, `document.querySelectorAll('.detail').length`)
  check('再次点击可收起', detailClosed === 0)

  // 2. 主题三态循环。起始为 dark（URL 指定），依次应到 system → light → dark。
  const seq = []
  for (let i = 0; i < 4; i++) {
    seq.push(
      await evaluate(
        ws,
        `JSON.stringify({
          attr: document.documentElement.getAttribute('data-theme'),
          stored: localStorage.getItem('bw.theme'),
          bg: getComputedStyle(document.body).backgroundColor,
        })`,
      ),
    )
    await evaluate(ws, `document.querySelector('.toggle').click()`)
    await sleep(150)
  }
  const parsed = seq.map((s) => JSON.parse(s))
  const attrs = parsed.map((p) => p.attr)
  const stored = parsed.map((p) => p.stored)
  check(
    '主题三态循环 dark→system→light→dark',
    attrs[0] === 'dark' && attrs[1] === null && attrs[2] === 'light' && attrs[3] === 'dark',
    attrs.map((a) => a ?? 'system').join(' → '),
  )
  check(
    '主题选择写入 localStorage',
    stored[0] === 'dark' && stored[1] === 'system' && stored[2] === 'light',
    stored.join(' → '),
  )
  // 亮暗两态的实际背景色必须不同，否则 token 没接上。
  check(
    '切主题实际改变页面底色',
    parsed[0].bg !== parsed[2].bg,
    `${parsed[0].bg} vs ${parsed[2].bg}`,
  )

  // 3. 溢出菜单能打开，且菜单项齐全。
  await evaluate(ws, `document.querySelector('.card .trigger').click()`)
  await sleep(200)
  const menu = await evaluate(
    ws,
    `(() => {
      const p = [...document.querySelectorAll('[popover]')].find((e) => e.matches(':popover-open'))
      if (!p) return null
      return {
        items: [...p.querySelectorAll('button')].map((b) => b.textContent.trim()),
        visible: p.getBoundingClientRect().width > 0,
      }
    })()`,
  )
  check(
    '卡片溢出菜单可打开',
    menu !== null && menu.visible,
    menu ? menu.items.join(' / ') : '未打开',
  )
  check(
    '菜单含编辑/快捷方式/删除',
    menu !== null && ['编辑', '创建快捷方式', '删除'].every((t) => menu.items.includes(t)),
  )

  ws.close()
  const bad = results.filter((r) => !r.ok).length
  console.log()
  console.log(bad ? `${bad} 项未通过` : '交互验证通过')
  process.exit(bad ? 1 : 0)
}

main().catch((e) => {
  console.error('验证脚本出错:', e.message)
  process.exit(2)
})
