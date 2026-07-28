package probe

// gpuPage 是 GPU 深度采集页。
//
// 采集的三类信息各有用途：
//   - 身份字符串（vendor/renderer/version）：伪造声称的型号
//   - 能力参数（limits/extensions/precision）：真实驱动决定，用于交叉验证
//   - 渲染产物（pixelHash）：实际光栅化行为
//
// 只取有型号区分度的 limits，不遍历全部参数：全表有上百项且多数在所有 GPU 上
// 相同，回传只增加噪声。
const gpuPage = `<!DOCTYPE html>
<html lang="zh-CN">
<head><meta charset="utf-8"><title>GPU 深度采集</title></head>
<body>
<p id="s">采集中…</p>
<canvas id="cv" width="256" height="256"></canvas>
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

// 有型号区分度的能力上限。集成显卡与独立显卡在这些项上差异明显，
// 因此它们能用来核对声称的型号是否合理。
const LIMIT_NAMES = [
  'MAX_TEXTURE_SIZE', 'MAX_RENDERBUFFER_SIZE', 'MAX_VIEWPORT_DIMS',
  'MAX_CUBE_MAP_TEXTURE_SIZE', 'MAX_TEXTURE_IMAGE_UNITS',
  'MAX_VERTEX_ATTRIBS', 'MAX_VERTEX_UNIFORM_VECTORS',
  'MAX_FRAGMENT_UNIFORM_VECTORS', 'MAX_VARYING_VECTORS',
  'MAX_COMBINED_TEXTURE_IMAGE_UNITS', 'ALIASED_LINE_WIDTH_RANGE',
  'ALIASED_POINT_SIZE_RANGE', 'MAX_SAMPLES',
  'MAX_3D_TEXTURE_SIZE', 'MAX_ARRAY_TEXTURE_LAYERS',
  'MAX_DRAW_BUFFERS', 'MAX_COLOR_ATTACHMENTS',
]

function limits(gl) {
  const out = {}
  for (const name of LIMIT_NAMES) {
    if (gl[name] === undefined) continue
    const v = safe(() => gl.getParameter(gl[name]), null)
    if (v === null) continue
    // 数组型参数（如 MAX_VIEWPORT_DIMS）转成普通数组，否则 JSON 里是对象。
    out[name] = (v && typeof v.length === 'number') ? Array.from(v) : v
  }
  return out
}

// 着色器精度。顶点与片元阶段各三种精度，共 6 组，
// 每组回传 rangeMin/rangeMax/precision 三个值。
function precision(gl) {
  const out = {}
  const stages = [['VERTEX', gl.VERTEX_SHADER], ['FRAGMENT', gl.FRAGMENT_SHADER]]
  const kinds = ['LOW_FLOAT', 'MEDIUM_FLOAT', 'HIGH_FLOAT',
                 'LOW_INT', 'MEDIUM_INT', 'HIGH_INT']
  for (const [sname, stage] of stages) {
    for (const kind of kinds) {
      if (gl[kind] === undefined) continue
      const p = safe(() => gl.getShaderPrecisionFormat(stage, gl[kind]), null)
      if (!p) continue
      out[sname + '_' + kind] = [p.rangeMin, p.rangeMax, p.precision]
    }
  }
  return out
}

// 固定场景渲染。用渐变三角形而非纯色：纯色在任何 GPU 上结果都一样，
// 测不出光栅化差异；渐变会经过插值与抖动，能反映真实驱动行为。
function renderHash(gl, canvas) {
  const vs = gl.createShader(gl.VERTEX_SHADER)
  gl.shaderSource(vs, 'attribute vec2 p;varying vec2 v;' +
    'void main(){v=p;gl_Position=vec4(p,0.0,1.0);}')
  gl.compileShader(vs)
  const fs = gl.createShader(gl.FRAGMENT_SHADER)
  gl.shaderSource(fs, 'precision mediump float;varying vec2 v;' +
    'void main(){gl_FragColor=vec4(v.x*0.5+0.5,v.y*0.5+0.5,' +
    'sin(v.x*8.0)*0.5+0.5,1.0);}')
  gl.compileShader(fs)
  const prog = gl.createProgram()
  gl.attachShader(prog, vs)
  gl.attachShader(prog, fs)
  gl.linkProgram(prog)
  if (!gl.getProgramParameter(prog, gl.LINK_STATUS)) return ''
  gl.useProgram(prog)

  const buf = gl.createBuffer()
  gl.bindBuffer(gl.ARRAY_BUFFER, buf)
  gl.bufferData(gl.ARRAY_BUFFER,
    new Float32Array([-0.8, -0.8, 0.8, -0.7, 0.1, 0.9]), gl.STATIC_DRAW)
  const loc = gl.getAttribLocation(prog, 'p')
  gl.enableVertexAttribArray(loc)
  gl.vertexAttribPointer(loc, 2, gl.FLOAT, false, 0, 0)
  gl.clearColor(0, 0, 0, 1)
  gl.clear(gl.COLOR_BUFFER_BIT)
  gl.drawArrays(gl.TRIANGLES, 0, 3)

  const px = new Uint8Array(canvas.width * canvas.height * 4)
  gl.readPixels(0, 0, canvas.width, canvas.height, gl.RGBA, gl.UNSIGNED_BYTE, px)
  // 逐字节拼串再哈希：求和会把不同像素分布压成同一个数。
  let s = ''
  for (let i = 0; i < px.length; i += 97) s += px[i].toString(16)
  return hash(s)
}

function glContext(type) {
  const out = {
    available: false, vendor: '', renderer: '', glVersion: '',
    shadingLang: '', limits: {}, extensions: [], precision: {}, pixelHash: '',
  }
  const c = document.createElement('canvas')
  c.width = 256
  c.height = 256
  const gl = safe(() => c.getContext(type, { preserveDrawingBuffer: true }), null)
  if (!gl) return out
  out.available = true
  const ext = safe(() => gl.getExtension('WEBGL_debug_renderer_info'), null)
  if (ext) {
    out.vendor = String(safe(() => gl.getParameter(ext.UNMASKED_VENDOR_WEBGL), ''))
    out.renderer = String(safe(() => gl.getParameter(ext.UNMASKED_RENDERER_WEBGL), ''))
  }
  out.glVersion = String(safe(() => gl.getParameter(gl.VERSION), ''))
  out.shadingLang = String(
    safe(() => gl.getParameter(gl.SHADING_LANGUAGE_VERSION), ''))
  out.limits = safe(() => limits(gl), {})
  out.extensions = safe(() => (gl.getSupportedExtensions() || []).slice(), [])
  out.precision = safe(() => precision(gl), {})
  out.pixelHash = safe(() => renderHash(gl, c), '')
  return out
}

// WebGPU 与 WebGL 是两套独立实现，伪造可能只覆盖后者。
// 真实 GPU 型号常出现在 adapterInfo 的 description 或 architecture 中。
async function webgpu() {
  const out = { available: false, adapterInfo: {}, limits: {}, features: [] }
  if (!navigator.gpu) return out
  try {
    const adapter = await navigator.gpu.requestAdapter()
    if (!adapter) {
      out.err = 'requestAdapter 返回 null'
      return out
    }
    out.available = true
    // info 在新版是同步属性，旧版是 requestAdapterInfo()，两种都试。
    let info = adapter.info
    if (!info && adapter.requestAdapterInfo) {
      info = await adapter.requestAdapterInfo()
    }
    if (info) {
      out.adapterInfo = {
        vendor: String(info.vendor || ''),
        architecture: String(info.architecture || ''),
        device: String(info.device || ''),
        description: String(info.description || ''),
      }
    }
    if (adapter.limits) {
      // limits 是带原型的接口对象，for...in 取不到，需按已知键名读。
      for (const k of ['maxTextureDimension2D', 'maxBufferSize',
        'maxComputeWorkgroupStorageSize', 'maxStorageBufferBindingSize',
        'maxUniformBufferBindingSize', 'maxComputeInvocationsPerWorkgroup']) {
        const v = adapter.limits[k]
        if (v !== undefined) out.limits[k] = v
      }
    }
    out.features = adapter.features ? Array.from(adapter.features) : []
  } catch (e) {
    out.err = String((e && e.message) || e)
  }
  return out
}

async function collect() {
  return {
    webgl1: safe(() => glContext('webgl'), {}),
    webgl2: safe(() => glContext('webgl2'), {}),
    webgpu: await webgpu(),
    uaArch: await (async () => {
      try {
        const d = navigator.userAgentData
        if (!d || !d.getHighEntropyValues) return {}
        return await d.getHighEntropyValues(
          ['architecture', 'bitness', 'model', 'platformVersion'])
      } catch (e) {
        return {}
      }
    })(),
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
  return fetch('/report', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ err: String((e && e.message) || e) }),
  })
})
</script>
</body>
</html>
`
