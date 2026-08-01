<script lang="ts">
  import {
    api,
    geoSourceLabels,
    kindLabels,
    stateLabels,
    type GroupTreeInfo,
    type Kernel,
    type ProfileView,
  } from './lib/api'
  import BatchBar from './lib/BatchBar.svelte'
  import ImportDialog from './lib/ImportDialog.svelte'
  import KernelSetup from './lib/KernelSetup.svelte'
  import ProfileForm from './lib/ProfileForm.svelte'
  import Sidebar from './lib/Sidebar.svelte'
  import URLHandlerSetup from './lib/URLHandlerSetup.svelte'
  import { applyTheme, theme } from './lib/theme.svelte'
  import Badge from './lib/ui/Badge.svelte'
  import Button from './lib/ui/Button.svelte'
  import Menu from './lib/ui/Menu.svelte'
  import ThemeToggle from './lib/ui/ThemeToggle.svelte'

  let profiles = $state<ProfileView[]>([])
  let kernels = $state<Kernel[]>([])
  let kernelsDir = $state('')
  let error = $state('')
  let busy = $state<Record<string, boolean>>({})
  let showForm = $state(false)
  let editing = $state<ProfileView | null>(null)
  let showImport = $state(false)
  let showURLHandler = $state(false)

  // 筛选状态。activeGroup 为空表示不限分组，等于 unassignedKey 表示只看未分组。
  let tree = $state<GroupTreeInfo | null>(null)
  let unassignedKey = $state('')
  let activeGroup = $state('')
  let activeTags = $state<string[]>([])
  let keyword = $state('')

  // 选中的 profile。用 Set 而非数组：勾选/取消是高频操作，
  // 数组的 includes + filter 在几百项时会有可感的卡顿。
  let selected = $state<Set<string>>(new Set())

  /** 轮询间隔。够快以便关掉浏览器窗口后列表及时收敛，又不至于压满 IPC。 */
  const POLL_MS = 2000

  // kernelsDir 与未分组哨兵值都是启动时确定的常量，只取一次，不参与轮询。
  $effect(() => {
    api.kernelsDir().then((d) => (kernelsDir = d)).catch(() => {})
    api.groupUnassignedKey().then((k) => (unassignedKey = k)).catch(() => {})
  })

  // 主题偏好写入 <html data-theme>。首帧已在 main.ts 里落过一次，
  // 这里负责后续切换。
  $effect(() => {
    applyTheme(theme.pref)
  })

  // 展开查看次要信息的 profile。默认收起：列表主要用于扫读状态与出口，
  // 种子、语言、备注这些排障时才看的字段常驻会把行高撑成四倍。
  let expanded = $state<Set<string>>(new Set())

  function toggleExpand(id: string) {
    const next = new Set(expanded)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    expanded = next
  }

  /** 运行状态到 Badge 语义色的映射。 */
  const stateTone = (s: string): 'ok' | 'warn' | 'err' | 'neutral' =>
    s === 'running' ? 'ok' : s === 'starting' ? 'warn' : s === 'failed' ? 'err' : 'neutral'

  /**
   * 拉取列表与内核。
   *
   * silent 为 true 时不改动 error：轮询失败通常是瞬时的，把它显示出来会
   * 盖掉用户操作的真实报错；反之轮询成功也不该清掉那条报错——用户点了启动
   * 失败，两秒后错误自己消失会让人以为没出问题。
   */
  async function refresh(silent = false) {
    try {
      const [ps, ks, tr] = await Promise.all([
        api.queryProfiles({
          group: activeGroup,
          tags: activeTags,
          keyword: keyword.trim(),
        } as never),
        api.listKernels(),
        api.groupTree(),
      ])
      profiles = ps ?? []
      kernels = ks ?? []
      tree = tr
      // 剔除已不存在的选中项，否则批量操作会带上被删掉的 ID。
      pruneSelection(profiles)
      if (!silent) error = ''
    } catch (e) {
      if (!silent) error = (e as Error).message
    }
  }

  function pruneSelection(list: ProfileView[]) {
    if (selected.size === 0) return
    const alive = new Set(list.map((p) => p.id))
    let changed = false
    const next = new Set<string>()
    for (const id of selected) {
      if (alive.has(id)) next.add(id)
      else changed = true
    }
    if (changed) selected = next
  }

  // 筛选条件变化时立刻重查，不等下个轮询周期——否则点了分组要等两秒才响应。
  $effect(() => {
    // 读取这几个值以建立依赖。
    void activeGroup
    void activeTags
    void keyword
    refresh(true)
  })

  function selectGroup(group: string) {
    activeGroup = group
  }

  function toggleTag(tag: string) {
    activeTags = activeTags.includes(tag)
      ? activeTags.filter((t) => t !== tag)
      : [...activeTags, tag]
  }

  function toggleSelect(id: string) {
    const next = new Set(selected)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    selected = next
  }

  function toggleSelectAll() {
    selected = allSelected ? new Set() : new Set(profiles.map((p) => p.id))
  }

  async function makeShortcut(p: ProfileView) {
    error = ''
    dismissNotice()
    try {
      const path = await api.createShortcut(p.id)
      showNotice(`已创建快捷方式: ${path}`)
    } catch (e) {
      error = (e as Error).message
    }
  }

  // 配置导出导入。与「批量导入」的区别：那个从代理文本建新号，
  // 这两个是把现有配置搬到别处（备份、换机器）。
  let bundleBusy = $state(false)
  // notice 是操作成功的反馈，与 error 分开：导入可能同时成功一部分、
  // 报一部分警告，两者需要同时呈现。
  let notice = $state('')

  // notice 自动消失的定时器。成功提示不该需要用户手动关：它只是确认操作
  // 完成了，读完就没用了，留在页面上会一直占位。
  //
  // error 不自动消失——那是需要用户处理的信息，自动清掉会让他错过。
  let noticeTimer: ReturnType<typeof setTimeout> | undefined

  // showNotice 设置成功提示并安排它自动消失。
  //
  // 每次调用都要先清掉上一个定时器：连续两次操作时，前一个定时器会在后一条
  // 提示还没读完时就把它清掉。
  function showNotice(msg: string) {
    clearTimeout(noticeTimer)
    notice = msg
    // 8 秒：导出提示里有完整文件路径，太短读不完。
    noticeTimer = setTimeout(() => (notice = ''), 8000)
  }

  // dismissNotice 手动关闭，同时取消定时器避免它稍后清掉新的提示。
  function dismissNotice() {
    clearTimeout(noticeTimer)
    notice = ''
  }

  // 组件销毁时清掉悬空的定时器，避免它在已卸载的组件上写状态。
  $effect(() => () => clearTimeout(noticeTimer))

  async function exportBundle() {
    error = ''
    dismissNotice()
    const path = await api.pickExportPath().catch(() => '')
    if (!path) return // 用户取消

    // 是否带凭据必须显式选择：导出文件会被复制、转发、甚至提交到仓库。
    const withSecrets = confirm(
      '导出文件中包含代理密码明文？\n\n' +
        '确定 = 包含（文件即凭据，切勿转发或提交到仓库）\n' +
        '取消 = 不包含（导入后需逐个补填密码）',
    )
    bundleBusy = true
    try {
      const n = await api.exportBundle(path, withSecrets)
      showNotice(
        `已导出 ${n} 个 profile 到 ${path}` +
          (withSecrets ? '（含密码明文，请妥善保管）' : '（不含密码）'),
      )
    } catch (e) {
      error = (e as Error).message
    } finally {
      bundleBusy = false
    }
  }

  async function importBundle() {
    error = ''
    dismissNotice()
    const path = await api.pickImportPath().catch(() => '')
    if (!path) return

    // 种子语义没有安全的默认值，必须由用户判断这批配置的用途。
    const newSeeds = confirm(
      '这批配置的用途是？\n\n' +
        '确定 = 批量建新号（生成新指纹，每个 profile 是不同设备）\n' +
        '取消 = 恢复备份或迁移机器（保留原指纹，还原为同一批设备）',
    )
    bundleBusy = true
    try {
      const r = await api.importBundle(path, { newSeeds } as never)
      const parts = [`导入 ${r.imported} 个`]
      if (r.skippedNames?.length) parts.push(`跳过 ${r.skippedNames.length} 个重名`)
      if (r.failures?.length) parts.push(`失败 ${r.failures.length} 个`)
      showNotice(parts.join('，'))
      // 警告与逐条失败都要显示出来——只报计数的话用户不知道该怎么修。
      const detail = [
        ...(r.warnings ?? []),
        ...(r.failures ?? []).map((f) => `第 ${f.index} 条「${f.name}」: ${f.err}`),
      ]
      if (detail.length) error = detail.join('\n')
      await refresh()
    } catch (e) {
      error = (e as Error).message
    } finally {
      bundleBusy = false
    }
  }

  // 会话状态由 Go 侧持有，轮询保证用户直接关掉浏览器窗口后列表能收敛。
  //
  // 两处暂停：表单打开时避免列表在用户填写期间反复重建；窗口不可见时
  // 没人在看，继续轮询只是白耗电。恢复可见时立刻补一次，不等下个周期。
  $effect(() => {
    if (showForm) return

    let timer = 0
    const tick = () => {
      if (!document.hidden) refresh(true)
    }
    const onVisible = () => {
      if (!document.hidden) refresh(true)
    }

    refresh(true)
    timer = setInterval(tick, POLL_MS)
    document.addEventListener('visibilitychange', onVisible)
    return () => {
      clearInterval(timer)
      document.removeEventListener('visibilitychange', onVisible)
    }
  })

  async function withBusy(id: string, fn: () => Promise<unknown>) {
    busy = { ...busy, [id]: true }
    try {
      await fn()
      error = ''
    } catch (e) {
      error = (e as Error).message
    } finally {
      busy = { ...busy, [id]: false }
      // 用 silent 刷新：这里已经设好了操作结果，不能让它被覆盖。
      await refresh(true)
    }
  }

  const start = (p: ProfileView) => withBusy(p.id, () => api.start(p.id))
  const stop = (p: ProfileView) => withBusy(p.id, () => api.stop(p.id))

  async function remove(p: ProfileView) {
    // 只删配置记录，浏览数据保留在磁盘上，所以这里说明清楚范围即可。
    if (!confirm(`删除 profile「${p.name}」的配置？\n浏览数据会保留在磁盘上。`)) return
    await withBusy(p.id, () => api.deleteProfile(p.id))
  }

  function onFormDone() {
    showForm = false
    editing = null
    // 非 silent：这是用户操作后的刷新，失败要让用户看到。
    refresh()
  }

  const noKernel = $derived(kernels.length === 0)
  const selectedIds = $derived([...selected])
  const allSelected = $derived(
    profiles.length > 0 && profiles.every((p) => selected.has(p.id)),
  )
  const filtering = $derived(
    activeGroup !== '' || activeTags.length > 0 || keyword.trim() !== '',
  )
</script>

<header class="topbar">
  <!-- 背景与底边线通栏，内容与 main 同宽同边距，两者左右才对得齐。 -->
  <div class="topinner">
    <div class="brand">
      <span class="mark" aria-hidden="true"></span>
      <div>
        <h1>better-web</h1>
        <p class="sub">Profile 管理面板</p>
      </div>
    </div>

    <div class="search">
      <svg viewBox="0 0 24 24" aria-hidden="true">
        <circle cx="11" cy="11" r="6.5" />
        <path d="m16 16 4.5 4.5" />
      </svg>
      <input bind:value={keyword} placeholder="搜索名称或备注" spellcheck="false" />
      {#if keyword}
        <button class="clear" title="清空搜索" onclick={() => (keyword = '')}>×</button>
      {/if}
    </div>

    <!-- 导入导出三项语义相近容易混，收进菜单并各带 title 说明：
         批量导入代理 = 粘贴代理文本从零建号
         导出/导入配置 = 把现有 profile 搬到别处（备份、换机器） -->
    <Menu label="导入导出">
      <button
        title="粘贴多行代理，批量创建新 profile"
        onclick={() => (showImport = !showImport)}>批量导入代理</button>
      <hr />
      <button
        title="把现有 profile 配置存成文件，用于备份或迁移到另一台机器"
        disabled={bundleBusy}
        onclick={exportBundle}>{bundleBusy ? '处理中…' : '导出配置'}</button>
      <button
        title="从配置文件还原 profile（备份恢复或以模板批量建号）"
        disabled={bundleBusy}
        onclick={importBundle}>导入配置</button>
      <hr />
      <button
        title="让其他应用点开的链接走指定的 Profile 实例"
        onclick={() => (showURLHandler = !showURLHandler)}>系统链接接管</button>
    </Menu>

    <ThemeToggle />

    <Button onclick={() => { editing = null; showForm = true }}>新建 Profile</Button>
  </div>
</header>

<main>
  {#if noKernel}
    <KernelSetup {kernelsDir} onInstalled={() => refresh()} />
  {/if}

  {#if notice}
    <!-- 与 error 并列而非互斥：导入可能同时成功一部分、报一部分警告。 -->
    <div class="banner ok" role="status">
      <span class="msg">{notice}</span>
      <button class="close" title="关闭" onclick={dismissNotice}>×</button>
    </div>
  {/if}

  {#if error}
    <!-- 错误不再随轮询自动消失，因此必须给用户一个关掉它的方式。 -->
    <div class="banner err" role="alert">
      <span class="msg">{error}</span>
      <button class="close" title="关闭" onclick={() => (error = '')}>×</button>
    </div>
  {/if}

  {#if showImport}
    <ImportDialog
      {tree}
      onDone={() => { showImport = false; refresh() }}
      onCancel={() => (showImport = false)}
    />
  {/if}

  {#if showURLHandler}
    <URLHandlerSetup {profiles} onClose={() => (showURLHandler = false)} />
  {/if}

  {#if showForm}
    <section class="panel">
      <ProfileForm
        {editing}
        onDone={onFormDone}
        onCancel={() => { showForm = false; editing = null }}
      />
    </section>
  {/if}

  <div class="body">
    <Sidebar
      {tree}
      {activeGroup}
      {activeTags}
      {unassignedKey}
      onSelectGroup={selectGroup}
      onToggleTag={toggleTag}
    />

    <div class="content">
      {#if selectedIds.length > 0}
        <BatchBar
          {selectedIds}
          {tree}
          onDone={() => refresh(true)}
          onClearSelection={() => (selected = new Set())}
        />
      {/if}

      {#if profiles.length === 0}
        <div class="empty">
          <p class="etitle">
            {filtering ? '没有符合条件的 profile' : '还没有 profile'}
          </p>
          <p class="ehint">
            {#if filtering}
              试试清空搜索词或取消筛选条件。
            {:else}
              点击右上角新建一个，或从菜单批量导入代理。
            {/if}
          </p>
        </div>
      {:else}
        <div class="listhead">
          <label class="selall">
            <input
              type="checkbox"
              checked={allSelected}
              onchange={toggleSelectAll}
            />
            全选
          </label>
          <span class="total tnum">{profiles.length} 个</span>
        </div>
        <ul class="list">
          {#each profiles as p (p.id)}
            <li class="card" class:picked={selected.has(p.id)}>
              <input
                class="pick"
                type="checkbox"
                checked={selected.has(p.id)}
                onchange={() => toggleSelect(p.id)}
                aria-label={`选择 ${p.name}`}
              />

              <div class="info">
                <div class="title">
                  <!-- 状态圆点与文字标记并存：圆点供扫读，文字保证不靠颜色传达信息。 -->
                  <span class="name">{p.name}</span>
                  <Badge tone={stateTone(p.state)} dot>
                    {stateLabels[p.state] ?? p.state}
                  </Badge>
                  <Badge tone={p.kind === 'fingerprint' ? 'info' : 'neutral'}>
                    {kindLabels[p.kind] ?? p.kind}
                  </Badge>
                  {#if p.group}
                    <Badge tone="info">{p.group}</Badge>
                  {/if}
                  {#each p.tags ?? [] as t}
                    <Badge>{t}</Badge>
                  {/each}
                  {#if p.startup?.newTabUrl || (p.startup?.urls ?? []).length > 0}
                    <Badge title="已配置打开的页面">页面</Badge>
                  {/if}
                </div>

                <!-- 常驻这一行只放扫读时真正要看的：走哪条线路、出口在哪。 -->
                <div class="meta">
                  {#if p.proxy}
                    <span class="mono">
                      {p.proxy.scheme}://{p.proxy.host}:{p.proxy.port}
                    </span>
                    {#if p.proxy.hasPassword}
                      <span class="ok-text" title="代理需要认证，密码已保存">认证</span>
                    {/if}
                  {:else}
                    <span class="warn-text">直连，未配置代理</span>
                  {/if}

                  {#if p.exit?.ip}
                    <!-- 运行中才有值：这是本次会话的实际出口，不是配置预览。 -->
                    <span class="sep" aria-hidden="true">·</span>
                    <span class="mono">{p.exit.ip}</span>
                    {#if p.exit.org}
                      <span class="faint">
                        {p.exit.org}{p.exit.asn ? ` (AS${p.exit.asn})` : ''}
                      </span>
                    {/if}
                  {/if}
                </div>

                {#each p.warnings ?? [] as w}
                  <p class="runtime-warn">{w}</p>
                {/each}

                <button
                  class="disclose"
                  aria-expanded={expanded.has(p.id)}
                  onclick={() => toggleExpand(p.id)}
                >
                  <span class="caret" class:open={expanded.has(p.id)} aria-hidden="true">▸</span>
                  {expanded.has(p.id) ? '收起详情' : '详情'}
                </button>

                {#if expanded.has(p.id)}
                  <dl class="detail">
                    {#if p.kind === 'fingerprint' && p.fingerprint}
                      <dt>机型</dt>
                      <dd>{p.fingerprint.device.label}</dd>
                      <!-- 时区与语言合成一行并标注来源：这两个值在停止态是
                           上次启动的实测缓存，不标注的话用户会当成当前事实。
                           default 表示从未启动过，那时它们只是内核兜底值。 -->
                      <dt>时区 / 语言</dt>
                      <dd>
                        {p.fingerprint.timezone} / {p.fingerprint.locale}
                        {#if p.geoSource}
                          <span
                            class="src"
                            class:stale={p.geoSource === 'lastRun'}
                            class:unknown={p.geoSource === 'default'}
                          >
                            {geoSourceLabels[p.geoSource] ?? p.geoSource}
                          </span>
                        {/if}
                      </dd>
                      {#if p.geoSource === 'default'}
                        <dt></dt>
                        <dd class="srchint">
                          启动时会按代理出口 IP 自动对齐，实际值可能与此不同。
                        </dd>
                      {/if}
                      <dt>种子</dt>
                      <dd class="tnum">{p.seed}</dd>
                    {:else}
                      <dt>环境</dt>
                      <dd>真实环境，不伪造指纹</dd>
                    {/if}
                    {#if p.notes}
                      <dt>备注</dt>
                      <dd>{p.notes}</dd>
                    {/if}
                  </dl>
                {/if}
              </div>

              <div class="ops">
                {#if p.state === 'running' || p.state === 'starting'}
                  <Button variant="ghost" size="sm" disabled={busy[p.id]} onclick={() => stop(p)}>
                    停止
                  </Button>
                {:else}
                  <Button
                    size="sm"
                    disabled={busy[p.id] || noKernel}
                    title={noKernel ? '需要先安装浏览器内核' : undefined}
                    onclick={() => start(p)}>启动</Button>
                {/if}
                <Menu label={`${p.name} 的操作`}>
                  <button
                    disabled={busy[p.id]}
                    onclick={() => { editing = p; showForm = true }}>编辑</button>
                  <button
                    title="在桌面创建快捷方式，双击直接启动此 profile"
                    disabled={busy[p.id]}
                    onclick={() => makeShortcut(p)}>创建快捷方式</button>
                  <hr />
                  <button class="danger" disabled={busy[p.id]} onclick={() => remove(p)}>
                    删除
                  </button>
                </Menu>
              </div>
            </li>
          {/each}
        </ul>
      {/if}
    </div>
  </div>
</main>

<style>
  /* ---------- 顶栏 ---------- */

  /*
   * 顶栏贴顶并常驻：列表长了之后搜索框与新建按钮不该随页面滚走。
   * 半透明加模糊让下方内容滚过时有层次，同时保留底边线作为边界。
   */
  .topbar {
    position: sticky;
    top: 0;
    z-index: 10;
    background: color-mix(in srgb, var(--c-canvas) 85%, transparent);
    backdrop-filter: blur(12px);
    border-bottom: 1px solid var(--c-border);
  }
  .topinner {
    max-width: var(--w-content);
    margin: 0 auto;
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    padding: var(--sp-3) var(--sp-5);
  }
  .brand {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    margin-right: auto;
  }
  .mark {
    width: 28px;
    height: 28px;
    flex: none;
    border-radius: 8px;
    background: linear-gradient(140deg, var(--c-accent), var(--c-accent-text));
  }
  h1 {
    font-size: var(--fs-lg);
    letter-spacing: 0.01em;
  }
  .sub {
    font-size: var(--fs-sm);
    color: var(--c-text-faint);
  }

  .search {
    position: relative;
    display: flex;
    align-items: center;
    width: 240px;
    flex: none;
  }
  .search svg {
    position: absolute;
    left: 9px;
    width: 15px;
    height: 15px;
    fill: none;
    stroke: var(--c-text-faint);
    stroke-width: 1.8;
    stroke-linecap: round;
    pointer-events: none;
  }
  .search input {
    padding-left: 30px;
    padding-right: 26px;
  }
  .clear {
    position: absolute;
    right: 6px;
    display: flex;
    align-items: center;
    justify-content: center;
    width: 18px;
    height: 18px;
    background: none;
    border: 0;
    border-radius: 50%;
    color: var(--c-text-faint);
    font-size: 15px;
    line-height: 1;
    cursor: pointer;
  }
  .clear:hover {
    background: var(--c-border-strong);
    color: var(--c-text);
  }

  /* ---------- 主体骨架 ---------- */

  main {
    max-width: var(--w-content);
    margin: 0 auto;
    padding: var(--sp-5) var(--sp-5) var(--sp-8);
    display: flex;
    flex-direction: column;
    gap: var(--sp-4);
  }
  .body {
    display: flex;
    gap: var(--sp-6);
    align-items: flex-start;
  }
  .content {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
  }

  .panel {
    background: var(--c-raised);
    border: 1px solid var(--c-border);
    border-radius: var(--r-md);
    box-shadow: var(--shadow-card);
    padding: var(--sp-5);
  }

  /* ---------- 提示条 ---------- */

  .banner {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--sp-3);
    border: 1px solid var(--c-border);
    border-left-width: 3px;
    border-radius: var(--r-sm);
    padding: var(--sp-3) var(--sp-4);
    font-size: var(--fs-base);
    line-height: var(--lh-loose);
  }
  /* 导入失败会逐条列出，用 pre-wrap 保留换行，否则全挤成一行读不了。
     长错误信息（如带堆栈的代理报错）还要能换行而不撑破容器。 */
  .msg {
    min-width: 0;
    white-space: pre-wrap;
    word-break: break-word;
  }
  .banner.ok {
    background: var(--c-ok-soft);
    border-left-color: var(--c-ok);
    color: var(--c-ok-text);
  }
  .banner.err {
    background: var(--c-err-soft);
    border-left-color: var(--c-err);
    color: var(--c-err-text);
  }
  .close {
    flex: none;
    background: none;
    border: 0;
    color: inherit;
    opacity: 0.7;
    font-size: 17px;
    line-height: 1;
    padding: 0 2px;
    cursor: pointer;
  }
  .close:hover {
    opacity: 1;
  }

  /* ---------- 列表 ---------- */

  .listhead {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0 var(--sp-1);
  }
  .selall {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    font-size: var(--fs-sm);
    color: var(--c-text-faint);
    cursor: pointer;
  }
  .selall:hover {
    color: var(--c-text-muted);
  }
  .total {
    font-size: var(--fs-sm);
    color: var(--c-text-faint);
  }

  .empty {
    text-align: center;
    padding: var(--sp-8) 0;
  }
  .etitle {
    font-size: var(--fs-md);
    color: var(--c-text-muted);
  }
  .ehint {
    margin-top: var(--sp-2);
    font-size: var(--fs-base);
    color: var(--c-text-faint);
  }

  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
  }

  /*
   * 卡片用 grid 而非 flex：勾选框、信息区、操作区三列，
   * 信息区展开详情时另两列保持顶部对齐，不会被拉到垂直居中。
   */
  .card {
    display: grid;
    grid-template-columns: auto 1fr auto;
    align-items: start;
    gap: var(--sp-3);
    background: var(--c-raised);
    border: 1px solid var(--c-border);
    border-radius: var(--r-md);
    box-shadow: var(--shadow-card);
    padding: var(--sp-3) var(--sp-4);
    transition:
      border-color var(--dur-fast) var(--ease),
      background var(--dur-fast) var(--ease);
  }
  .card:hover {
    border-color: var(--c-border-strong);
  }
  .card.picked {
    border-color: var(--c-accent);
    background: var(--c-accent-soft);
  }
  .pick {
    margin-top: 5px;
  }

  .info {
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
    min-width: 0;
  }
  .title {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    flex-wrap: wrap;
  }
  .name {
    font-size: var(--fs-md);
    font-weight: 600;
    color: var(--c-text);
  }

  .meta {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    flex-wrap: wrap;
    font-size: var(--fs-sm);
    color: var(--c-text-muted);
  }
  .mono {
    font-family: var(--font-mono);
    font-size: var(--fs-sm);
  }
  .faint {
    color: var(--c-text-faint);
  }
  .sep {
    color: var(--c-border-strong);
  }
  .ok-text {
    color: var(--c-ok-text);
  }
  .warn-text {
    color: var(--c-warn-text);
  }

  /* 运行时警告（如出口为机房 IP）不阻断使用，用醒目但非报错的配色，
     并给左边框以便在密集的 meta 行中被注意到。 */
  .runtime-warn {
    padding: var(--sp-1) var(--sp-3);
    border-left: 2px solid var(--c-warn);
    background: var(--c-warn-soft);
    border-radius: var(--r-sm);
    font-size: var(--fs-sm);
    line-height: var(--lh-base);
    color: var(--c-warn-text);
  }

  .disclose {
    align-self: flex-start;
    display: flex;
    align-items: center;
    gap: 5px;
    background: none;
    border: 0;
    padding: 0;
    font-family: inherit;
    font-size: var(--fs-sm);
    color: var(--c-text-faint);
    cursor: pointer;
  }
  .disclose:hover {
    color: var(--c-accent-text);
  }
  .caret {
    display: inline-block;
    transition: transform var(--dur-fast) var(--ease);
  }
  .caret.open {
    transform: rotate(90deg);
  }

  .detail {
    margin: 0;
    display: grid;
    grid-template-columns: auto 1fr;
    gap: var(--sp-1) var(--sp-3);
    font-size: var(--fs-sm);
    padding: var(--sp-2) var(--sp-3);
    background: var(--c-sunken);
    border-radius: var(--r-sm);
  }
  .detail dt {
    color: var(--c-text-faint);
  }
  .detail dd {
    margin: 0;
    color: var(--c-text-muted);
    min-width: 0;
    word-break: break-word;
  }

  /* 来源标记。默认中性——运行中和手动指定都是可信的当前值。 */
  .src {
    display: inline-block;
    margin-left: var(--sp-2);
    padding: 0 6px;
    border: 1px solid var(--c-border-strong);
    border-radius: var(--r-pill);
    font-size: var(--fs-xs);
    color: var(--c-text-faint);
    white-space: nowrap;
  }
  /* 上次实测：值是真的，但可能已过期，用警示色提醒代理出口会变。 */
  .src.stale {
    border-color: var(--c-warn);
    background: var(--c-warn-soft);
    color: var(--c-warn-text);
  }
  /* 从未启动：显示的是兜底值，与真实出口无关，比过期更需要警示。 */
  .src.unknown {
    border-color: var(--c-warn);
    background: var(--c-warn-soft);
    color: var(--c-warn-text);
  }
  .srchint {
    font-size: var(--fs-xs);
    color: var(--c-text-faint);
    line-height: var(--lh-base);
  }

  .ops {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    flex-shrink: 0;
  }

  /* 窄窗口下侧栏改为横向排布在列表上方，避免主列表被挤到不足 400px。 */
  @media (max-width: 860px) {
    .body {
      flex-direction: column;
    }
    .search {
      width: 160px;
    }
  }
</style>
