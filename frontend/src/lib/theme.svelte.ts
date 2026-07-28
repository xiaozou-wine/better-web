/**
 * 主题状态：跟随系统 / 强制亮色 / 强制暗色。
 *
 * 存 'system' 而非解析后的 light|dark：用户选了跟随系统，就应该在系统
 * 切换时跟着变，把解析结果存下来会把这层意图丢掉。
 */

export type ThemePref = 'system' | 'light' | 'dark'

const STORAGE_KEY = 'bw.theme'

function readStored(): ThemePref {
  try {
    const v = localStorage.getItem(STORAGE_KEY)
    if (v === 'light' || v === 'dark' || v === 'system') return v
  } catch {
    // 隐私模式下 localStorage 可能抛异常，此时退回跟随系统即可。
  }
  return 'system'
}

const media =
  typeof window !== 'undefined' && window.matchMedia
    ? window.matchMedia('(prefers-color-scheme: light)')
    : null

class Theme {
  /** 用户的选择，三态。 */
  pref = $state<ThemePref>(readStored())
  /** 系统当前偏好，仅在 pref 为 system 时参与解析。 */
  #systemLight = $state(media?.matches ?? false)

  /** 实际生效的主题，供 UI 显示当前处于亮还是暗。 */
  resolved = $derived<'light' | 'dark'>(
    this.pref === 'system' ? (this.#systemLight ? 'light' : 'dark') : this.pref,
  )

  constructor() {
    // 系统偏好变化要实时反映。整个应用生命周期都需要，不解绑。
    media?.addEventListener('change', (e) => {
      this.#systemLight = e.matches
    })
  }

  set(pref: ThemePref) {
    this.pref = pref
    try {
      localStorage.setItem(STORAGE_KEY, pref)
    } catch {
      // 存不下就只在本次会话生效，不影响使用。
    }
  }

  /** 在亮暗之间切换。当前跟随系统时，切到与系统相反的那个。 */
  toggle() {
    this.set(this.resolved === 'dark' ? 'light' : 'dark')
  }
}

export const theme = new Theme()

/**
 * 把生效主题写到 <html data-theme>。
 *
 * pref 为 system 时移除该属性，交回 CSS 的 prefers-color-scheme 媒体查询，
 * 而不是写入解析值——这样切系统主题时不依赖 JS 也能立刻生效。
 */
export function applyTheme(pref: ThemePref) {
  const root = document.documentElement
  if (pref === 'system') root.removeAttribute('data-theme')
  else root.setAttribute('data-theme', pref)
}
