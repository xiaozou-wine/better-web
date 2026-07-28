<script lang="ts">
  import { api, type BatchSummary, type GroupTreeInfo, type TagMode } from './api'
  import Button from './ui/Button.svelte'

  let {
    selectedIds,
    tree,
    onDone,
    onClearSelection,
  }: {
    selectedIds: string[]
    tree: GroupTreeInfo | null
    onDone: () => void
    onClearSelection: () => void
  } = $props()

  let busy = $state(false)
  let error = $state('')
  let summary = $state<BatchSummary | null>(null)
  let concurrency = $state(0)

  // 分组与标签的操作面板，默认收起以免工具条太高。
  let showGroup = $state(false)
  let showTag = $state(false)
  let groupInput = $state('')
  let tagInput = $state('')
  let tagMode = $state<TagMode>('add')

  $effect(() => {
    api.defaultBatchConcurrency().then((n) => {
      if (concurrency === 0) concurrency = n
    }).catch(() => {})
  })

  async function run(fn: () => Promise<BatchSummary>) {
    busy = true
    error = ''
    summary = null
    try {
      summary = await fn()
    } catch (e) {
      error = (e as Error).message
    } finally {
      busy = false
      onDone()
    }
  }

  const start = () => run(() => api.startBatch(selectedIds, concurrency))
  const stop = () => run(() => api.stopBatch(selectedIds))

  async function remove() {
    if (!confirm(
      `删除选中的 ${selectedIds.length} 个 profile 的配置？\n浏览数据会保留在磁盘上。`,
    )) return
    await run(() => api.deleteBatch(selectedIds))
  }

  async function applyGroup() {
    await run(() => api.assignGroupBatch(selectedIds, groupInput.trim()))
    showGroup = false
  }

  async function applyTags() {
    const tags = tagInput
      .split(/[,，\s]+/)
      .map((t) => t.trim())
      .filter(Boolean)
    if (tags.length === 0) {
      error = '请填写至少一个标签'
      return
    }
    await run(() => api.tagBatch(selectedIds, tags, tagMode))
    showTag = false
    tagInput = ''
  }

  // 失败项要列出来，否则批量操作后用户不知道哪几个没成。
  const failures = $derived(summary?.results?.filter((r) => !r.ok) ?? [])
</script>

<div class="bar">
  <div class="main">
    <span class="n">已选 {selectedIds.length} 个</span>

    <label class="conc">
      并发
      <input type="number" min="1" max="32" bind:value={concurrency} />
    </label>

    <Button size="sm" disabled={busy} onclick={start}>启动</Button>
    <Button variant="ghost" size="sm" disabled={busy} onclick={stop}>停止</Button>
    <Button
      variant="ghost"
      size="sm"
      disabled={busy}
      onclick={() => { showGroup = !showGroup; showTag = false }}>设分组</Button>
    <Button
      variant="ghost"
      size="sm"
      disabled={busy}
      onclick={() => { showTag = !showTag; showGroup = false }}>改标签</Button>
    <Button variant="danger" size="sm" disabled={busy} onclick={remove}>删除</Button>
    <Button variant="quiet" size="sm" onclick={onClearSelection}>取消选择</Button>
  </div>

  {#if showGroup}
    <div class="panel">
      <input
        bind:value={groupInput}
        placeholder="分组名，留空则移出分组"
        list="bw-groups"
      />
      <datalist id="bw-groups">
        {#each tree?.groups ?? [] as g (g.name)}
          <option value={g.name}></option>
        {/each}
      </datalist>
      <Button size="sm" disabled={busy} onclick={applyGroup}>应用</Button>
    </div>
  {/if}

  {#if showTag}
    <div class="panel">
      <select bind:value={tagMode}>
        <option value="add">追加</option>
        <option value="remove">移除</option>
        <option value="replace">替换全部</option>
      </select>
      <input bind:value={tagInput} placeholder="标签，用逗号或空格分隔" />
      <Button size="sm" disabled={busy} onclick={applyTags}>应用</Button>
    </div>
  {/if}

  {#if busy}
    <p class="msg">执行中…</p>
  {/if}

  {#if error}
    <p class="msg err" role="alert">{error}</p>
  {/if}

  {#if summary}
    <p class="msg" class:err={summary.failed > 0}>
      成功 {summary.succeeded} / {summary.total}
      {#if summary.failed > 0}，失败 {summary.failed}{/if}
    </p>
    {#if failures.length > 0}
      <ul class="fails">
        {#each failures as f (f.profileId)}
          <li><strong>{f.name || f.profileId}</strong>：{f.err}</li>
        {/each}
      </ul>
    {/if}
  {/if}
</div>

<style>
  /*
   * 工具条用 accent 底而非普通卡片底：它是随选中出现的临时上下文，
   * 与常驻的 profile 卡片要一眼分得开。
   */
  .bar {
    background: var(--c-accent-soft);
    border: 1px solid var(--c-accent);
    border-radius: var(--r-md);
    padding: var(--sp-2) var(--sp-3);
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
  }
  .main {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    flex-wrap: wrap;
  }
  .n {
    font-size: var(--fs-base);
    font-weight: 500;
    color: var(--c-text);
    margin-right: auto;
  }
  .conc {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    font-size: var(--fs-sm);
    color: var(--c-text-muted);
    white-space: nowrap;
  }
  .conc input {
    width: 56px;
  }
  .panel {
    display: flex;
    gap: var(--sp-2);
    align-items: center;
  }
  .panel select {
    width: auto;
    flex: none;
  }
  .msg {
    font-size: var(--fs-sm);
    color: var(--c-text-muted);
  }
  .msg.err {
    color: var(--c-err-text);
  }
  .fails {
    margin: 0;
    padding-left: var(--sp-5);
    font-size: var(--fs-sm);
    color: var(--c-err-text);
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
</style>
