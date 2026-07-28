<script lang="ts">
  /**
   * 统一按钮。取代此前散在 5 个组件里的各写一份 button 样式
   * （padding 有 6/13、8/15、8/16、9/18 四种，同一屏能看出高度不齐）。
   */
  import type { Snippet } from 'svelte'

  let {
    variant = 'primary',
    size = 'md',
    type = 'button',
    disabled = false,
    title,
    ariaLabel,
    onclick,
    children,
    ...rest
  }: {
    /** primary 用于主操作，ghost 用于次要操作，danger 用于不可逆操作，quiet 无边框。 */
    variant?: 'primary' | 'ghost' | 'danger' | 'quiet'
    size?: 'sm' | 'md'
    type?: 'button' | 'submit'
    disabled?: boolean
    title?: string
    ariaLabel?: string
    onclick?: (e: MouseEvent) => void
    children: Snippet
    [key: string]: unknown
  } = $props()
</script>

<button
  {type}
  {disabled}
  {title}
  aria-label={ariaLabel}
  class="btn {variant} {size}"
  {onclick}
  {...rest}
>
  {@render children()}
</button>

<style>
  .btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: var(--sp-2);
    border: 1px solid transparent;
    border-radius: var(--r-sm);
    font-family: inherit;
    font-weight: 500;
    white-space: nowrap;
    cursor: pointer;
    transition:
      background var(--dur-fast) var(--ease),
      border-color var(--dur-fast) var(--ease),
      color var(--dur-fast) var(--ease);
  }
  .md {
    padding: 7px 14px;
    font-size: var(--fs-base);
  }
  .sm {
    padding: 5px 10px;
    font-size: var(--fs-sm);
  }

  .primary {
    background: var(--c-accent);
    color: var(--c-accent-fg);
  }
  .primary:hover:not(:disabled) {
    background: var(--c-accent-hover);
  }

  .ghost {
    background: transparent;
    border-color: var(--c-border-strong);
    color: var(--c-text-muted);
  }
  .ghost:hover:not(:disabled) {
    background: var(--c-raised-hover);
    border-color: var(--c-accent);
    color: var(--c-text);
  }

  /* 不可逆操作平时保持中性，hover 时才转红——避免一排红字造成警戒疲劳。 */
  .danger {
    background: transparent;
    border-color: var(--c-border-strong);
    color: var(--c-err-text);
  }
  .danger:hover:not(:disabled) {
    background: var(--c-err-soft);
    border-color: var(--c-err);
  }

  .quiet {
    background: transparent;
    color: var(--c-text-muted);
  }
  .quiet:hover:not(:disabled) {
    background: var(--c-raised-hover);
    color: var(--c-text);
  }

  .btn:disabled {
    opacity: 0.45;
    cursor: not-allowed;
  }
</style>
