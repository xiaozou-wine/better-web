<script lang="ts">
  /**
   * 溢出操作菜单。卡片上只留主操作（启动/停止），其余收进这里，
   * 让列表行不被四个等重按钮撑满。
   *
   * 用 popover 属性而非自绘遮罩：浏览器负责置顶层、点击外部关闭与 Esc
   * 关闭，省掉一套容易出错的手写事件管理。Chromium 114+ 支持，
   * 而本应用运行在打过补丁的 Chromium 上，可用性有保证。
   */
  import type { Snippet } from 'svelte'

  let { label = '更多操作', children }: { label?: string; children: Snippet } =
    $props()

  // popover 的 target 与锚点名都需要全局唯一：同一列表里会有几十个实例。
  const uid = crypto.randomUUID().slice(0, 8)
  const id = `menu-${uid}`
  const anchor = `--anchor-${uid}`
</script>

<button
  class="trigger"
  popovertarget={id}
  title={label}
  aria-label={label}
  style="anchor-name: {anchor}"
>
  <svg viewBox="0 0 24 24" aria-hidden="true">
    <circle cx="5" cy="12" r="1.6" />
    <circle cx="12" cy="12" r="1.6" />
    <circle cx="19" cy="12" r="1.6" />
  </svg>
</button>

<div {id} popover="auto" class="pop" style="position-anchor: {anchor}">
  {@render children()}
</div>

<style>
  .trigger {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 30px;
    height: 30px;
    flex: none;
    background: transparent;
    border: 1px solid var(--c-border-strong);
    border-radius: var(--r-sm);
    color: var(--c-text-muted);
    cursor: pointer;
    transition:
      background var(--dur-fast) var(--ease),
      border-color var(--dur-fast) var(--ease);
  }
  .trigger:hover {
    background: var(--c-raised-hover);
    border-color: var(--c-accent);
    color: var(--c-text);
  }
  svg {
    width: 16px;
    height: 16px;
    fill: currentColor;
  }

  .pop {
    position: fixed;
    margin: 0;
    min-width: 168px;
    padding: var(--sp-1);
    background: var(--c-overlay);
    border: 1px solid var(--c-border-strong);
    border-radius: var(--r-sm);
    box-shadow: var(--shadow-pop);
    color: var(--c-text);
  }
  .pop:popover-open {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

  /*
   * 锚定到触发按钮下方右对齐。CSS anchor positioning 需要 Chromium 125+；
   * 不支持时 @supports 内整段不生效，popover 退回浏览器默认的居中摆放，
   * 功能不受影响。
   */
  @supports (position-area: bottom span-left) {
    .pop {
      position-area: bottom span-left;
      margin-top: var(--sp-1);
      /* 贴边时自动翻转到上方，避免菜单被视口截断。 */
      position-try-fallbacks: flip-block;
    }
  }
  /* 菜单项由调用方以普通 button 传入，样式在此统一。 */
  .pop :global(button) {
    display: flex;
    align-items: center;
    width: 100%;
    gap: var(--sp-2);
    background: transparent;
    border: 0;
    border-radius: var(--r-sm);
    padding: 7px 10px;
    font-family: inherit;
    font-size: var(--fs-base);
    color: var(--c-text-muted);
    text-align: left;
    cursor: pointer;
  }
  .pop :global(button:hover:not(:disabled)) {
    background: var(--c-raised-hover);
    color: var(--c-text);
  }
  .pop :global(button:disabled) {
    opacity: 0.45;
    cursor: not-allowed;
  }
  .pop :global(button.danger) {
    color: var(--c-err-text);
  }
  .pop :global(button.danger:hover:not(:disabled)) {
    background: var(--c-err-soft);
  }
  .pop :global(hr) {
    margin: var(--sp-1) 0;
    border: 0;
    border-top: 1px solid var(--c-border);
  }
</style>
