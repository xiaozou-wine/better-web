<script lang="ts">
  /** 胶囊标记：类型、状态、分组、标签共用一套形状，靠 tone 区分语义。 */
  import type { Snippet } from 'svelte'

  let {
    tone = 'neutral',
    dot = false,
    title,
    children,
  }: {
    tone?: 'neutral' | 'info' | 'ok' | 'warn' | 'err'
    /** 前置状态圆点，用于运行状态这类需要快速扫读的标记。 */
    dot?: boolean
    title?: string
    children: Snippet
  } = $props()
</script>

<span class="badge {tone}" {title}>
  {#if dot}<span class="dot" aria-hidden="true"></span>{/if}
  {@render children()}
</span>

<style>
  .badge {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    border: 1px solid var(--c-border-strong);
    border-radius: var(--r-pill);
    padding: 1px 8px;
    font-size: var(--fs-xs);
    line-height: 1.7;
    white-space: nowrap;
    color: var(--c-text-muted);
  }
  .dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: currentColor;
    flex: none;
  }
  .neutral {
    color: var(--c-text-faint);
  }
  .info {
    border-color: var(--c-accent);
    color: var(--c-accent-text);
    background: var(--c-accent-soft);
  }
  .ok {
    border-color: var(--c-ok);
    color: var(--c-ok-text);
    background: var(--c-ok-soft);
  }
  .warn {
    border-color: var(--c-warn);
    color: var(--c-warn-text);
    background: var(--c-warn-soft);
  }
  .err {
    border-color: var(--c-err);
    color: var(--c-err-text);
    background: var(--c-err-soft);
  }
</style>
