package probe

// probePage 是采集页面。它在浏览器内读取各项指纹并 POST 回采集服务。
//
// 注意几处刻意的写法：
//   - Canvas 与 Audio 只取渲染结果的哈希，不回传原始数据：原始 buffer 有几百 KB，
//     而判断噪声是否生效只需要摘要。
//   - WebGL 用 WEBGL_debug_renderer_info 而非 RENDERER：前者才是反爬系统实际读的。
//   - 出错的项留空而不中断整体采集，否则单项不可用会导致什么都拿不到。
const probePage = `<!DOCTYPE html>
<html lang="zh-CN">
<head><meta charset="utf-8"><title>指纹采集</title></head>
<body>
<p id="s">采集中…</p>
<script>
'use strict'

function hash(str) {
  // FNV-1a 32 位。只需稳定摘要，不需要密码学强度。
  let h = 0x811c9dc5
  for (let i = 0; i < str.length; i++) {
    h ^= str.charCodeAt(i)
    h = Math.imul(h, 0x01000193) >>> 0
  }
  return h.toString(16).padStart(8, '0')
}

function safe(fn, fallback) {
  try {
    const v = fn()
    return v === undefined || v === null ? fallback : v
  } catch (e) {
    return fallback
  }
}

function canvasHash() {
  const c = document.createElement('canvas')
  c.width = 300
  c.height = 80
  const ctx = c.getContext('2d')
  if (!ctx) return ''
  // 混合文本与图形：两条路径在内核里的噪声实现不同，一起测才有意义。
  ctx.textBaseline = 'top'
  ctx.font = '16px "Arial"'
  ctx.fillStyle = '#f60'
  ctx.fillRect(0, 0, 120, 40)
  ctx.fillStyle = '#069'
  ctx.fillText('better-web 指纹探针 ABCabc123', 2, 15)
  ctx.fillStyle = 'rgba(102, 204, 0, 0.7)'
  ctx.fillText('better-web 指纹探针 ABCabc123', 4, 25)
  ctx.beginPath()
  ctx.arc(200, 40, 30, 0, Math.PI * 2)
  ctx.fillStyle = 'rgba(255,0,255,0.5)'
  ctx.fill()
  return hash(c.toDataURL())
}

function webgl() {
  const out = { vendor: '', renderer: '' }
  const c = document.createElement('canvas')
  const gl = c.getContext('webgl') || c.getContext('experimental-webgl')
  if (!gl) return out
  // 反爬系统读的是 debug_renderer_info 暴露的 UNMASKED_* 值。
  const ext = gl.getExtension('WEBGL_debug_renderer_info')
  if (ext) {
    out.vendor = String(gl.getParameter(ext.UNMASKED_VENDOR_WEBGL) || '')
    out.renderer = String(gl.getParameter(ext.UNMASKED_RENDERER_WEBGL) || '')
  }
  return out
}

async function audioHash() {
  const Ctx = window.OfflineAudioContext || window.webkitOfflineAudioContext
  if (!Ctx) return ''
  const ctx = new Ctx(1, 5000, 44100)
  const osc = ctx.createOscillator()
  osc.type = 'triangle'
  osc.frequency.value = 10000
  const comp = ctx.createDynamicsCompressor()
  osc.connect(comp)
  comp.connect(ctx.destination)
  osc.start(0)
  const buf = await ctx.startRendering()
  const data = buf.getChannelData(0)
  // 逐样本纳入哈希而非先求和：求和会把不同噪声压成同一个浮点数，
  // 造成不同种子看起来 audio 指纹相同的假象。
  let s = ''
  for (let i = 4500; i < 5000; i++) s += data[i].toString()
  return hash(s)
}

async function collect() {
  const gl = safe(webgl, { vendor: '', renderer: '' })
  return {
    userAgent: safe(() => navigator.userAgent, ''),
    platform: safe(() => navigator.platform, ''),
    hardwareConcurrency: safe(() => navigator.hardwareConcurrency, 0),
    deviceMemory: safe(() => navigator.deviceMemory, 0),
    languages: safe(() => Array.from(navigator.languages || []), []),
    language: safe(() => navigator.language, ''),
    timezone: safe(() => Intl.DateTimeFormat().resolvedOptions().timeZone, ''),
    timezoneOffset: safe(() => new Date().getTimezoneOffset(), 0),
    screenWidth: safe(() => screen.width, 0),
    screenHeight: safe(() => screen.height, 0),
    devicePixelRatio: safe(() => window.devicePixelRatio, 0),
    webglVendor: gl.vendor,
    webglRenderer: gl.renderer,
    canvasHash: safe(canvasHash, ''),
    audioHash: await audioHash().catch(() => ''),
    webdriver: safe(() => navigator.webdriver === true, false),
    uaData: safe(() => {
      const d = navigator.userAgentData
      if (!d) return {}
      return { brands: d.brands, mobile: d.mobile, platform: d.platform }
    }, {}),
    plugins: safe(
      () => Array.from(navigator.plugins || []).map((p) => p.name),
      [],
    ),
  }
}

collect().then((res) => {
  document.getElementById('s').textContent = '采集完成'
  return fetch('/report', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(res),
  })
}).catch((e) => {
  document.getElementById('s').textContent = '采集失败: ' + e
})
</script>
</body>
</html>
`
