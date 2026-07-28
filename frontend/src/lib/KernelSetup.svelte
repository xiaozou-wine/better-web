<script lang="ts">
  import { api, formatBytes, type InstallProgress, type Release } from './api'
  import Button from './ui/Button.svelte'

  let { kernelsDir, onInstalled }: {
    kernelsDir: string
    onInstalled: () => void
  } = $props()

  let releases = $state<Release[]>([])
  let loading = $state(false)
  let installing = $state<string | null>(null)
  let progress = $state<InstallProgress | null>(null)
  let error = $state('')
  let expanded = $state(false)

  $effect(() => {
    const off = api.onInstallProgress((p) => {
      progress = p
      if (p.done) {
        installing = null
        progress = null
        if (p.err) {
          error = p.err
        } else {
          onInstalled()
        }
      }
    })
    return off
  })

  async function loadReleases() {
    loading = true
    error = ''
    try {
      releases = (await api.listAvailableKernels()) ?? []
      expanded = true
    } catch (e) {
      error = (e as Error).message
    } finally {
      loading = false
    }
  }

  async function install(rel: Release) {
    installing = rel.version
    error = ''
    try {
      await api.installKernel(rel)
    } catch (e) {
      // 失败详情也会经进度事件送来，这里兜住直接抛出的情况。
      error = (e as Error).message
      installing = null
    }
  }

  const percent = $derived(
    progress && progress.total > 0
      ? Math.min(100, Math.round((progress.downloaded / progress.total) * 100))
      : 0,
  )
</script>

<div class="setup">
  <div class="head">
    <div>
      <strong>未检测到浏览器内核</strong>
      <p class="desc">
        指纹能力由打过补丁的 Chromium 内核提供。可以自动下载，
        也可以手动解压到 <code>{kernelsDir}\&lt;版本号&gt;\</code>。
      </p>
    </div>
    {#if !expanded}
      <Button disabled={loading} onclick={loadReleases}>
        {loading ? '查询中…' : '查看可用版本'}
      </Button>
    {/if}
  </div>

  {#if installing && progress}
    <div class="progress">
      <div
        class="bar"
        role="progressbar"
        aria-valuenow={percent}
        aria-valuemin="0"
        aria-valuemax="100"
        aria-label={`安装 ${installing}`}
      >
        <div class="fill" style="width:{percent}%"></div>
      </div>
      <span class="pct">
        正在安装 {installing} · {percent}%
        （{formatBytes(progress.downloaded)} / {formatBytes(progress.total)}）
      </span>
    </div>
  {:else if expanded}
    {#if releases.length === 0}
      <p class="desc">没有查到可用版本。</p>
    {:else}
      <ul>
        {#each releases.slice(0, 5) as rel (rel.version)}
          <li>
            <span class="ver tnum">{rel.version}</span>
            <span class="size tnum">{formatBytes(rel.size)}</span>
            <Button size="sm" disabled={installing !== null} onclick={() => install(rel)}>
              下载并安装
            </Button>
          </li>
        {/each}
      </ul>
    {/if}
  {/if}

  {#if error}
    <p class="err" role="alert">{error}</p>
  {/if}
</div>

<style>
  .setup {
    background: var(--c-accent-soft);
    border: 1px solid var(--c-border);
    border-left: 3px solid var(--c-accent);
    border-radius: var(--r-md);
    padding: var(--sp-4);
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
  }
  .head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--sp-4);
  }
  .desc {
    margin-top: var(--sp-1);
    font-size: var(--fs-base);
    line-height: var(--lh-loose);
    color: var(--c-text-muted);
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--sp-1);
  }
  li {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    font-size: var(--fs-base);
    padding: var(--sp-1) var(--sp-3);
    background: var(--c-raised);
    border: 1px solid var(--c-border);
    border-radius: var(--r-sm);
  }
  .ver {
    font-weight: 600;
  }
  .size {
    color: var(--c-text-faint);
    font-size: var(--fs-sm);
    margin-right: auto;
  }
  .progress {
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
  }
  .bar {
    height: 6px;
    background: var(--c-sunken);
    border: 1px solid var(--c-border);
    border-radius: var(--r-pill);
    overflow: hidden;
  }
  .fill {
    height: 100%;
    background: var(--c-accent);
    transition: width var(--dur-base) var(--ease);
  }
  .pct {
    font-size: var(--fs-sm);
    color: var(--c-text-muted);
    font-variant-numeric: tabular-nums;
  }
  .err {
    color: var(--c-err-text);
    font-size: var(--fs-base);
  }
</style>
