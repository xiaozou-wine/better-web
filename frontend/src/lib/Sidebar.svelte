<script lang="ts">
  import type { GroupTreeInfo } from './api'

  let {
    tree,
    activeGroup,
    activeTags,
    unassignedKey,
    onSelectGroup,
    onToggleTag,
  }: {
    tree: GroupTreeInfo | null
    activeGroup: string
    activeTags: string[]
    unassignedKey: string
    onSelectGroup: (group: string) => void
    onToggleTag: (tag: string) => void
  } = $props()
</script>

<aside>
  <nav>
    <p class="head">分组</p>
    <button
      class="row"
      class:active={activeGroup === ''}
      onclick={() => onSelectGroup('')}
    >
      <span class="label">全部</span>
      <span class="count">{tree?.total ?? 0}</span>
    </button>

    {#each tree?.groups ?? [] as g (g.name)}
      <button
        class="row"
        class:active={activeGroup === g.name}
        onclick={() => onSelectGroup(g.name)}
      >
        <span class="label">{g.name}</span>
        <span class="count">{g.count}</span>
      </button>
    {/each}

    {#if (tree?.unassigned ?? 0) > 0}
      <button
        class="row"
        class:active={activeGroup === unassignedKey}
        onclick={() => onSelectGroup(unassignedKey)}
      >
        <span class="label muted">未分组</span>
        <span class="count">{tree?.unassigned}</span>
      </button>
    {/if}
  </nav>

  {#if (tree?.tags ?? []).length > 0}
    <div class="tags">
      <p class="head">
        标签
        {#if activeTags.length > 1}<span class="hint">取交集</span>{/if}
      </p>
      <div class="taglist">
        {#each tree?.tags ?? [] as t (t.name)}
          <button
            class="tag"
            class:on={activeTags.includes(t.name)}
            aria-pressed={activeTags.includes(t.name)}
            onclick={() => onToggleTag(t.name)}
          >
            {t.name}
            <span class="count">{t.count}</span>
          </button>
        {/each}
      </div>
    </div>
  {/if}
</aside>

<style>
  aside {
    width: var(--w-sidebar);
    flex: none;
    display: flex;
    flex-direction: column;
    gap: var(--sp-5);
    /* 列表滚动时筛选条件保持可见——切分组不必先滚回顶部。 */
    position: sticky;
    top: calc(var(--h-topbar) + var(--sp-5));
  }
  nav {
    display: flex;
    flex-direction: column;
    gap: 1px;
  }
  .head {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    padding: 0 var(--sp-3) var(--sp-2);
    font-size: var(--fs-xs);
    font-weight: 600;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--c-text-faint);
  }
  .hint {
    font-weight: 400;
    letter-spacing: 0;
    text-transform: none;
    opacity: 0.8;
  }

  .row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--sp-2);
    width: 100%;
    background: transparent;
    border: 0;
    border-radius: var(--r-sm);
    padding: 6px var(--sp-3);
    font-family: inherit;
    font-size: var(--fs-base);
    color: var(--c-text-muted);
    cursor: pointer;
    text-align: left;
    transition:
      background var(--dur-fast) var(--ease),
      color var(--dur-fast) var(--ease);
  }
  .row:hover {
    background: var(--c-raised-hover);
    color: var(--c-text);
  }
  /*
   * 选中项除了底色，还在左侧压一道 accent 竖条。只靠底色时，
   * 亮色主题下那点明度差在浅底上几乎读不出来。
   */
  .row.active {
    background: var(--c-accent-soft);
    color: var(--c-accent-text);
    font-weight: 500;
    box-shadow: inset 2px 0 0 var(--c-accent);
  }
  .label {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .label.muted {
    color: var(--c-text-faint);
  }
  .count {
    flex: none;
    font-size: var(--fs-xs);
    font-variant-numeric: tabular-nums;
    color: var(--c-text-faint);
  }
  .row.active .count {
    color: var(--c-accent-text);
  }

  .taglist {
    display: flex;
    flex-wrap: wrap;
    gap: var(--sp-1);
  }
  .tag {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    background: transparent;
    border: 1px solid var(--c-border-strong);
    border-radius: var(--r-pill);
    padding: 2px var(--sp-2);
    font-family: inherit;
    font-size: var(--fs-xs);
    color: var(--c-text-muted);
    cursor: pointer;
    transition:
      background var(--dur-fast) var(--ease),
      border-color var(--dur-fast) var(--ease),
      color var(--dur-fast) var(--ease);
  }
  .tag:hover {
    border-color: var(--c-accent);
    color: var(--c-text);
  }
  .tag.on {
    background: var(--c-accent);
    border-color: var(--c-accent);
    color: var(--c-accent-fg);
  }
  .tag.on .count {
    color: var(--c-accent-fg);
    opacity: 0.75;
  }

  /* 窄窗口下侧栏横排到列表上方，分组与标签各占一行。 */
  @media (max-width: 860px) {
    aside {
      position: static;
      width: 100%;
      flex-direction: row;
      flex-wrap: wrap;
      gap: var(--sp-3);
    }
    nav {
      flex-direction: row;
      flex-wrap: wrap;
      gap: var(--sp-1);
    }
    nav .head,
    .tags .head {
      width: 100%;
      padding-bottom: 0;
    }
    .row {
      width: auto;
      border: 1px solid var(--c-border-strong);
    }
  }
</style>
