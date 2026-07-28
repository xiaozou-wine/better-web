package probe

// workerProbePage 在主线程与 Worker 中分别采集同一批指纹并比对。
//
// 存在原因：CreepJS 一类检测器不只读主线程的值，还会在 Worker（以及
// OffscreenCanvas）里重读一遍。伪造若只作用于主线程，两处就会不一致，
// 而真实浏览器的这些值在两个作用域里必然相同。这类矛盾是「说谎检测」
// 的主力手段，比任何单项数值都更能暴露伪造。
//
// Worker 中可见的 navigator 子集有限（没有 plugins、languages 等），
// 因此只比对两边都存在的字段。
const workerProbePage = `<!DOCTYPE html>
<html lang="zh-CN">
<head><meta charset="utf-8"><title>Worker 一致性采集</title></head>
<body>
<p id="s">采集中…</p>
<script>
'use strict'

function hash(str) {
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

// mainScope 采集主线程的值。
function mainScope() {
  return {
    userAgent: safe(() => navigator.userAgent, ''),
    platform: safe(() => navigator.platform, ''),
    hardwareConcurrency: safe(() => navigator.hardwareConcurrency, 0),
    deviceMemory: safe(() => navigator.deviceMemory, 0),
    language: safe(() => navigator.language, ''),
    timezone: safe(() => Intl.DateTimeFormat().resolvedOptions().timeZone, ''),
    timezoneOffset: safe(() => new Date().getTimezoneOffset(), 0),
    uaDataPlatform: safe(() => (navigator.userAgentData || {}).platform || '', ''),
    webglRenderer: safe(() => {
      const gl = document.createElement('canvas').getContext('webgl')
      if (!gl) return ''
      const ext = gl.getExtension('WEBGL_debug_renderer_info')
      return ext ? String(gl.getParameter(ext.UNMASKED_RENDERER_WEBGL) || '') : ''
    }, ''),
  }
}

// workerSource 是 Worker 内执行的采集代码。
// 通过 Blob 构造以避免额外的网络请求。
const workerSource = ` + "`" + `
self.onmessage = function () {
  function safe(fn, fallback) {
    try {
      var v = fn()
      return v === undefined || v === null ? fallback : v
    } catch (e) {
      return fallback
    }
  }
  var out = {
    userAgent: safe(function () { return navigator.userAgent }, ''),
    platform: safe(function () { return navigator.platform }, ''),
    hardwareConcurrency: safe(function () { return navigator.hardwareConcurrency }, 0),
    deviceMemory: safe(function () { return navigator.deviceMemory }, 0),
    language: safe(function () { return navigator.language }, ''),
    timezone: safe(function () {
      return Intl.DateTimeFormat().resolvedOptions().timeZone
    }, ''),
    timezoneOffset: safe(function () { return new Date().getTimezoneOffset() }, 0),
    uaDataPlatform: safe(function () {
      return (navigator.userAgentData || {}).platform || ''
    }, ''),
    webglRenderer: '',
  }
  // OffscreenCanvas 让 Worker 也能读 WebGL，是检测器常用的第二条路径。
  try {
    if (typeof OffscreenCanvas !== 'undefined') {
      var gl = new OffscreenCanvas(1, 1).getContext('webgl')
      if (gl) {
        var ext = gl.getExtension('WEBGL_debug_renderer_info')
        if (ext) {
          out.webglRenderer = String(gl.getParameter(ext.UNMASKED_RENDERER_WEBGL) || '')
        }
      }
    }
  } catch (e) {}
  self.postMessage(out)
}
` + "`" + `

function workerScope() {
  return new Promise((resolve) => {
    let done = false
    const finish = (v) => {
      if (!done) {
        done = true
        resolve(v)
      }
    }
    // Worker 不可用时返回 null，由 Go 侧判定为"无法比对"而非"不一致"。
    try {
      const url = URL.createObjectURL(
        new Blob([workerSource], { type: 'application/javascript' }),
      )
      const w = new Worker(url)
      w.onmessage = (e) => {
        w.terminate()
        URL.revokeObjectURL(url)
        finish(e.data)
      }
      w.onerror = () => {
        w.terminate()
        URL.revokeObjectURL(url)
        finish(null)
      }
      w.postMessage('go')
      setTimeout(() => finish(null), 8000)
    } catch (e) {
      finish(null)
    }
  })
}

// prototypeLies 检查关键 API 是否留下被改写的痕迹。
// 源码层伪造不应留痕；若这里报出问题，说明有 JS 层注入在改属性。
function prototypeLies() {
  const lies = []
  const checkNative = (obj, name, label) => {
    try {
      const d = Object.getOwnPropertyDescriptor(obj, name)
      if (!d) return
      const fn = d.get || d.value
      if (typeof fn !== 'function') return
      const s = Function.prototype.toString.call(fn)
      // 原生实现必定形如 "function get platform() { [native code] }"。
      if (!s.includes('[native code]')) lies.push(label + ': toString 非原生')
    } catch (e) {
      lies.push(label + ': 描述符不可读')
    }
  }
  checkNative(Navigator.prototype, 'platform', 'navigator.platform')
  checkNative(Navigator.prototype, 'userAgent', 'navigator.userAgent')
  checkNative(Navigator.prototype, 'hardwareConcurrency', 'navigator.hardwareConcurrency')
  checkNative(Navigator.prototype, 'languages', 'navigator.languages')
  checkNative(Screen.prototype, 'width', 'screen.width')
  checkNative(HTMLCanvasElement.prototype, 'toDataURL', 'canvas.toDataURL')
  checkNative(CanvasRenderingContext2D.prototype, 'getImageData', 'ctx.getImageData')
  checkNative(WebGLRenderingContext.prototype, 'getParameter', 'gl.getParameter')

  // Function.prototype.toString 自身被改写是更彻底的伪装手段，也更容易被查出。
  try {
    const s = Function.prototype.toString.call(Function.prototype.toString)
    if (!s.includes('[native code]')) lies.push('Function.prototype.toString 被改写')
  } catch (e) {
    lies.push('Function.prototype.toString 不可调用')
  }
  return lies
}

async function collect() {
  const main = mainScope()
  const worker = await workerScope()
  return {
    main: main,
    worker: worker,
    workerAvailable: worker !== null,
    prototypeLies: safe(prototypeLies, ['检查自身失败']),
    canvasHash: safe(() => {
      const c = document.createElement('canvas')
      c.width = 200
      c.height = 50
      const ctx = c.getContext('2d')
      if (!ctx) return ''
      ctx.textBaseline = 'top'
      ctx.font = '14px Arial'
      ctx.fillStyle = '#069'
      ctx.fillText('worker-consistency', 2, 10)
      return hash(c.toDataURL())
    }, ''),
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
