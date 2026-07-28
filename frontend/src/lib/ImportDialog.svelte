<script lang="ts">
  import { api, type GroupTreeInfo, type ImportResult } from './api'
  import Button from './ui/Button.svelte'

  let {
    tree,
    onDone,
    onCancel,
  }: {
    tree: GroupTreeInfo | null
    onDone: () => void
    onCancel: () => void
  } = $props()

  let text = $state('')
  let namePrefix = $state('')
  let group = $state('')
  let tagInput = $state('')
  let kind = $state<'fingerprint' | 'daily'>('fingerprint')
  let busy = $state(false)
  let error = $state('')
  let result = $state<ImportResult | null>(null)

  // 有效行数：预先告知会建几个 profile，避免误粘一大段文本后才发现。
  const lineCount = $derived(
    text
      .split('\n')
      .map((l) => l.trim())
      .filter((l) => l !== '' && !l.startsWith('#')).length,
  )

  async function submit() {
    busy = true
    error = ''
    result = null
    try {
      result = await api.importProxies({
        text,
        namePrefix: namePrefix.trim(),
        group: group.trim(),
        tags: tagInput.split(/[,，\s]+/).map((t) => t.trim()).filter(Boolean),
        kind,
      } as never)
      onDone()
    } catch (e) {
      error = (e as Error).message
    } finally {
      busy = false
    }
  }
</script>

<section class="dlg">
  <h2>批量导入代理</h2>

  <p class="hint">
    每行一个代理，支持
    <code>host:port</code>、<code>host:port:user:pass</code>、
    <code>scheme://user:pass@host:port</code>。
    省略协议时按 SOCKS5 处理，<code>#</code> 开头的行会跳过。
  </p>

  <label>
    代理列表{#if lineCount > 0}<span class="cnt">（{lineCount} 行有效）</span>{/if}
    <textarea
      bind:value={text}
      rows="8"
      spellcheck="false"
      placeholder={'1.2.3.4:1080:user:pass\ngate.example.com:7000\nhttp://u:p@5.6.7.8:8080'}
    ></textarea>
  </label>

  <div class="grid">
    <label>
      名称前缀
      <input bind:value={namePrefix} placeholder="留空则用「导入」" />
    </label>
    <label>
      分组
      <input bind:value={group} placeholder="可选" list="bw-import-groups" />
      <datalist id="bw-import-groups">
        {#each tree?.groups ?? [] as g (g.name)}
          <option value={g.name}></option>
        {/each}
      </datalist>
    </label>
    <label>
      标签
      <input bind:value={tagInput} placeholder="逗号或空格分隔" />
    </label>
    <label>
      类型
      <select bind:value={kind}>
        <option value="fingerprint">指纹模式</option>
        <option value="daily">日常模式</option>
      </select>
    </label>
  </div>

  <p class="hint">
    每个 profile 会拿到独立的随机种子。共用种子会让 canvas 指纹完全一致，
    多账号一眼被关联。
  </p>

  {#if error}
    <p class="err" role="alert">{error}</p>
  {/if}

  {#if result}
    <div class="result">
      <p>成功创建 {result.created?.length ?? 0} 个</p>
      {#if (result.parseFailed ?? []).length > 0}
        <p class="err">以下行格式无法识别（密码已隐去）：</p>
        <ul>
          {#each result.parseFailed ?? [] as f (f.line)}
            <li>第 {f.line} 行 <code>{f.raw}</code>：{f.err}</li>
          {/each}
        </ul>
      {/if}
      {#if (result.createFailed ?? []).length > 0}
        <p class="err">以下项创建失败：</p>
        <ul>
          {#each result.createFailed ?? [] as f (f.name)}
            <li>{f.name}：{f.err}</li>
          {/each}
        </ul>
      {/if}
    </div>
  {/if}

  <div class="actions">
    <Button variant="ghost" onclick={onCancel}>关闭</Button>
    <Button disabled={busy || lineCount === 0} onclick={submit}>
      {busy ? '导入中…' : `导入 ${lineCount} 个`}
    </Button>
  </div>
</section>

<style>
  .dlg {
    background: var(--c-raised);
    border: 1px solid var(--c-border);
    border-radius: var(--r-md);
    box-shadow: var(--shadow-card);
    padding: var(--sp-5);
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
  }
  h2 {
    font-size: var(--fs-xl);
  }
  label {
    display: flex;
    flex-direction: column;
    gap: var(--sp-1);
    font-size: var(--fs-base);
    color: var(--c-text-muted);
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
    gap: var(--sp-3);
  }
  .cnt {
    color: var(--c-ok-text);
  }
  .hint {
    font-size: var(--fs-sm);
    color: var(--c-text-faint);
    line-height: var(--lh-loose);
  }
  .err {
    color: var(--c-err-text);
    font-size: var(--fs-sm);
  }
  .result {
    background: var(--c-sunken);
    border: 1px solid var(--c-border);
    border-radius: var(--r-sm);
    padding: var(--sp-2) var(--sp-3);
    font-size: var(--fs-sm);
    color: var(--c-text-muted);
    display: flex;
    flex-direction: column;
    gap: var(--sp-1);
  }
  .result ul {
    margin: 0;
    padding-left: var(--sp-5);
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: var(--sp-2);
    margin-top: var(--sp-1);
  }
</style>
