<script lang="ts">
  /**
   * 主题切换。三态循环：跟随系统 → 亮 → 暗 → 跟随系统。
   *
   * 做成循环而非二态开关，是为了让"跟随系统"可达——只给亮暗两态时，
   * 用户点过一次就再也回不到跟随系统了。
   */
  import { theme, type ThemePref } from '../theme.svelte'

  const order: ThemePref[] = ['system', 'light', 'dark']
  const labels: Record<ThemePref, string> = {
    system: '跟随系统',
    light: '亮色',
    dark: '暗色',
  }

  function next() {
    const i = order.indexOf(theme.pref)
    theme.set(order[(i + 1) % order.length])
  }
</script>

<button
  class="toggle"
  onclick={next}
  title={`主题：${labels[theme.pref]}（点击切换）`}
  aria-label={`主题：${labels[theme.pref]}，点击切换`}
>
  {#if theme.pref === 'system'}
    <!-- 显示器：跟随系统 -->
    <svg viewBox="0 0 24 24" aria-hidden="true">
      <rect x="2.5" y="4" width="19" height="12.5" rx="1.8" />
      <path d="M8.5 20.5h7M12 16.5v4" />
    </svg>
  {:else if theme.pref === 'light'}
    <!-- 太阳：强制亮色 -->
    <svg viewBox="0 0 24 24" aria-hidden="true">
      <circle cx="12" cy="12" r="4.2" />
      <path
        d="M12 2.5v2.2M12 19.3v2.2M2.5 12h2.2M19.3 12h2.2M5.3 5.3l1.6 1.6M17.1 17.1l1.6 1.6M18.7 5.3l-1.6 1.6M6.9 17.1l-1.6 1.6"
      />
    </svg>
  {:else}
    <!-- 月亮：强制暗色 -->
    <svg viewBox="0 0 24 24" aria-hidden="true">
      <path d="M20.5 14.3A8.8 8.8 0 1 1 9.7 3.5a7 7 0 0 0 10.8 10.8Z" />
    </svg>
  {/if}
</button>

<style>
  .toggle {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 32px;
    height: 32px;
    flex: none;
    background: transparent;
    border: 1px solid var(--c-border-strong);
    border-radius: var(--r-sm);
    color: var(--c-text-muted);
    cursor: pointer;
    transition:
      color var(--dur-fast) var(--ease),
      border-color var(--dur-fast) var(--ease),
      background var(--dur-fast) var(--ease);
  }
  .toggle:hover {
    background: var(--c-raised-hover);
    border-color: var(--c-accent);
    color: var(--c-text);
  }
  svg {
    width: 16px;
    height: 16px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.7;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
</style>
