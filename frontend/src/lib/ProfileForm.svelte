<script lang="ts">
  import {
    api,
    type CreateRequest,
    type CredentialProtectionInfo,
    type DeviceProfile,
    type GroupStat,
    type StartupMode,
    type Kernel,
    type ProfileView,
    type ProxyCheck,
  } from './api'
  import Button from './ui/Button.svelte'

  type Kind = 'daily' | 'fingerprint'

  // editing 为 null 表示新建，否则为编辑该 profile。
  let { editing = null, onDone, onCancel }: {
    editing?: ProfileView | null
    onDone: () => void
    onCancel: () => void
  } = $props()

  // 表单字段只取 editing 的初始值作为默认值，之后由用户输入接管，
  // 因此这里刻意只读一次，不做响应式跟随。切换编辑对象时父组件会重建本组件。
  // svelte-ignore state_referenced_locally
  const initial: ProfileView | null = editing
  let name = $state(initial?.name ?? '')
  let kind = $state<Kind>((initial?.kind as Kind) ?? 'fingerprint')
  let useProxy = $state(initial?.proxy != null)
  let scheme = $state(initial?.proxy?.scheme ?? 'socks5')
  let host = $state(initial?.proxy?.host ?? '')
  let port = $state(initial?.proxy?.port ? String(initial.proxy.port) : '')
  let username = $state(initial?.proxy?.username ?? '')
  let password = $state('')
  let notes = $state(initial?.notes ?? '')
  let error = $state('')
  let saving = $state(false)

  // 分组与标签。标签在界面上用逗号或空格分隔，提交前拆成数组。
  let group = $state(initial?.group ?? '')
  let tagInput = $state((initial?.tags ?? []).join(', '))
  let groups = $state<GroupStat[]>([])

  // 启动页与新标签页。ungoogled-chromium 的默认新标签页是空白的
  // （Google 的组件被移除），所以 newTabUrl 在实际使用中比启动页更常用。
  let startupMode = $state<StartupMode>(
    (initial?.startup?.mode as StartupMode) ?? 'newtab',
  )
  let startupURLText = $state((initial?.startup?.urls ?? []).join('\n'))
  let newTabURL = $state(initial?.startup?.newTabUrl ?? '')

  // 机型锁定。空串表示由种子随机抽取，这是默认与推荐做法。
  let deviceLabel = $state(initial?.deviceLabel ?? '')
  let devices = $state<DeviceProfile[]>([])
  // 给已用过的 profile 换机型需显式确认，与换内核大版本同理。
  let confirmDeviceChange = $state(false)
  const deviceChanged = $derived(deviceLabel !== (initial?.deviceLabel ?? ''))

  // 代理检测状态。checking 期间禁用按钮，避免重复发起。
  let checking = $state(false)
  let check = $state<ProxyCheck | null>(null)

  // 一行粘贴代理。独立于下面五个字段，解析成功后回填它们并清空自身：
  // 留着原文没有用处，而它含密码明文，界面上多留一份就多一个泄漏面。
  let pasteLine = $state('')
  let pasteError = $state('')

  // 密码的存储保护级别。非 Windows 平台会退化为明文，必须告知用户，
  // 否则他们会以为密码已受保护而放松对数据目录的警惕。
  let credProtection = $state<CredentialProtectionInfo | null>(null)

  // 内核版本。空串表示跟随最新内核，不锁定。
  let kernelVersion = $state(initial?.kernelVersion ?? '')
  let kernels = $state<Kernel[]>([])
  // 已用过的指纹 profile 换内核大版本会导致指纹漂移，后端要求显式确认。
  let confirmKernelChange = $state(false)

  const isUsed = $derived(Boolean(initial?.lastUseAt))
  const majorOf = (v: string) => v.split('.')[0] ?? ''
  // 与后端 app.crossesMajor 的判定保持一致：两端都非空且主版本不同才算跨版本。
  const crossesMajor = $derived(
    initial != null &&
      initial.kind === 'fingerprint' &&
      isUsed &&
      Boolean(initial.kernelVersion) &&
      Boolean(kernelVersion) &&
      majorOf(initial.kernelVersion ?? '') !== majorOf(kernelVersion),
  )

  // 排障开关：关闭指定的伪造子系统以定位是哪一项触发了检测。
  // 正常使用应全部保持开启，每关一项就少一层伪装。
  let spoofTargets = $state<string[]>([])
  let disabledSpoofing = $state<string[]>([...(initial?.disableSpoofing ?? [])])

  // 日常模式用系统已装的官方 Chrome，而非指纹内核。
  // 指纹模式禁用（官方 Chrome 不认识 --fingerprint，会静默失去全部伪装）。
  let useSystemBrowser = $state(initial?.useSystemBrowser ?? false)

  // 提交时的实际取值：切到指纹模式一律置 false。
  //
  // 不靠界面隐藏就够了——隐藏只是不显示，勾选状态还留在变量里，
  // 用户先勾上再切成指纹模式就会提交非法组合，被 Go 侧 fail-closed 拒绝。
  // 与其让他撞一次错误，不如在这里归零。
  const effectiveSystemBrowser = $derived(
    kind === 'daily' ? useSystemBrowser : false,
  )

  // 匹配本机 GPU 厂商：靠筛选种子实现，只对新建生效。
  let matchHostGPU = $state(false)

  // 与 effectiveSystemBrowser 同理：切到日常模式一律置 false。
  //
  // 日常模式不伪造指纹，没有种子可筛，Go 侧会拒绝这个组合。而勾选状态
  // 在切换类型后仍留在变量里，只隐藏控件不够——用户先在指纹模式勾上、
  // 再切成日常模式，提交时就会被拒。
  const effectiveMatchHostGPU = $derived(
    kind === 'fingerprint' ? matchHostGPU : false,
  )

  let hostGPU = $state<{ family: string; renderer: string } | null>(null)
  let detectingGPU = $state(false)
  let gpuError = $state('')

  // 先探测让用户知道本机是哪一族，以及大概能不能筛到。
  // 不自动探测：那会在每次打开表单时启动一次浏览器。
  async function detectGPU() {
    detectingGPU = true
    gpuError = ''
    try {
      hostGPU = await api.detectHostGPU(kernelVersion)
    } catch (e) {
      hostGPU = null
      gpuError = (e as Error).message
    } finally {
      detectingGPU = false
    }
  }
  let showAdvanced = $state(
    (initial?.disableSpoofing?.length ?? 0) > 0 || initial?.geoOverride != null,
  )

  // 地理覆盖：填了就跳过出口 IP 反查，直接用指定值。
  // 留空是推荐做法——自动对齐才能保证与真实出口一致。
  let overrideGeo = $state(initial?.geoOverride != null)
  let geoCountry = $state(initial?.geoOverride?.countryCode ?? '')
  let geoTimezone = $state(initial?.geoOverride?.timezone ?? '')
  let geoLocale = $state(initial?.geoOverride?.locale ?? '')

  $effect(() => {
    api.listKernels().then((v) => (kernels = v ?? [])).catch(() => {})
    api.listSpoofTargets().then((v) => (spoofTargets = v ?? [])).catch(() => {})
    api.credentialProtection().then((v) => (credProtection = v)).catch(() => {})
    api.groupTree().then((t) => (groups = t.groups ?? [])).catch(() => {})
    api.listDeviceProfiles().then((d) => (devices = d ?? [])).catch(() => {})
  })

  /** 把界面上的标签串拆成数组。逗号、中文逗号、空白都作分隔符。 */
  function parseTags(): string[] {
    return tagInput.split(/[,，\s]+/).map((t) => t.trim()).filter(Boolean)
  }

  /**
   * 组装启动页配置。两项都为默认值时返回 undefined，
   * 让后端存 NULL 而非一个内容为空的配置对象。
   */
  function buildStartup() {
    const urls = startupURLText
      .split('\n')
      .map((u) => u.trim())
      .filter(Boolean)
    const ntp = newTabURL.trim()

    if (startupMode === 'newtab' && urls.length === 0 && ntp === '') {
      return undefined
    }
    return {
      mode: startupMode,
      // 非 URLs 模式不带 URL，避免切换模式后残留配置意外打开页面。
      urls: startupMode === 'urls' ? urls : [],
      newTabUrl: ntp,
    }
  }

  /**
   * 清除该 profile 的浏览数据。
   *
   * 不可恢复：会一并清掉 Cookie、登录态与浏览历史，等于账号要重新登录。
   * 因此要求二次确认并说明后果，而非只弹一句"确定吗"。
   */
  async function clearData() {
    if (!initial) return
    if (!confirm(
      `清除「${initial.name}」的浏览数据？\n\n` +
        'Cookie、登录态与浏览历史会被一并删除，不可恢复，' +
        '该 profile 下的账号需要重新登录。\n\nprofile 配置本身会保留。',
    )) return

    saving = true
    error = ''
    try {
      await api.deleteProfileData(initial.id)
      error = ''
      onDone()
    } catch (e) {
      error = (e as Error).message
    } finally {
      saving = false
    }
  }

  function toggleSpoof(t: string) {
    disabledSpoofing = disabledSpoofing.includes(t)
      ? disabledSpoofing.filter((x) => x !== t)
      : [...disabledSpoofing, t]
  }

  function buildGeoOverride() {
    if (!overrideGeo) return undefined
    const tz = geoTimezone.trim()
    const locale = geoLocale.trim()
    // 时区与语言是内核参数的必填项，缺一个就会启动失败，在此拦掉。
    if (!tz || !locale) throw new Error('指定地理信息时，时区与语言都必须填写')
    return {
      countryCode: geoCountry.trim().toUpperCase(),
      timezone: tz,
      locale,
      latitude: 0,
      longitude: 0,
    } as CreateRequest['geoOverride']
  }

  // 编辑已有代理时密码不回填（后端不下发原文），留空即保留原密码。
  const passwordPlaceholder = initial?.proxy?.hasPassword ? '留空则保留原密码' : ''

  function buildProxy() {
    if (!useProxy) return undefined
    const p = Number(port)
    if (!host.trim()) throw new Error('请填写代理主机')
    if (!Number.isInteger(p) || p < 1 || p > 65535) {
      throw new Error('代理端口需为 1-65535 的整数')
    }
    return {
      scheme,
      host: host.trim(),
      port: p,
      username: username.trim(),
      password,
    } as CreateRequest['proxy']
  }

  // 编辑已有代理且未重填密码时无法检测：前端不持有密码原文，
  // 送空密码去试会得到认证失败，那是误报而非代理有问题。
  const needsPasswordToCheck = $derived(
    useProxy && !password && (initial?.proxy?.hasPassword ?? false),
  )

  async function runCheck() {
    error = ''
    check = null
    checking = true
    try {
      const proxy = buildProxy()
      if (!proxy) throw new Error('请先启用并填写代理')
      check = await api.checkProxy(proxy as never)
    } catch (e) {
      error = (e as Error).message
    } finally {
      checking = false
    }
  }

  // 代理字段一改，先前的检测结果就不再对应当前配置，必须失效，
  // 否则用户会拿着旧结果误以为新配置已验证过。
  function invalidateCheck() {
    check = null
  }

  // 解析粘贴的一行并回填五个字段。
  //
  // 解析走后端 model.ParseProxy 而非在此重写：密码含 @ 或 : 时的切法
  // 有讲究，两份实现漂移的后果是表单显示的凭据与实际存的不一致。
  //
  // 只在解析成功时才动字段——失败时保留用户已填的内容，避免一次误粘
  // 就清空了手工填好的配置。
  async function applyPasteLine() {
    pasteError = ''
    const line = pasteLine.trim()
    if (!line) {
      pasteError = '请先粘贴一行代理配置'
      return
    }
    try {
      const p = await api.parseProxyLine(line)
      scheme = p.scheme as typeof scheme
      host = p.host
      port = String(p.port)
      username = p.username ?? ''
      password = p.password ?? ''
      pasteLine = ''
      invalidateCheck()
    } catch (e) {
      pasteError = (e as Error).message
    }
  }

  async function save() {
    error = ''
    saving = true
    try {
      const proxy = buildProxy()
      const geoOverride = buildGeoOverride()
      if (editing) {
        await api.updateProfile({
          id: editing.id,
          name,
          proxy,
          clearProxy: !useProxy,
          geoOverride,
          kernelVersion,
          extraArgs: editing.extraArgs ?? [],
          notes,
          disableSpoofing: disabledSpoofing,
          confirmKernelChange,
          group: group.trim(),
          tags: parseTags(),
          deviceLabel,
          confirmDeviceChange,
          startup: buildStartup(),
          useSystemBrowser: effectiveSystemBrowser,
        } as never)
      } else {
        await api.createProfile({
          name,
          kind,
          proxy,
          geoOverride,
          notes,
          kernelVersion,
          disableSpoofing: disabledSpoofing,
          group: group.trim(),
          tags: parseTags(),
          deviceLabel,
          matchHostGPU: effectiveMatchHostGPU,
          startup: buildStartup(),
          useSystemBrowser: effectiveSystemBrowser,
        } as never)
      }
      onDone()
    } catch (e) {
      error = (e as Error).message
    } finally {
      saving = false
    }
  }
</script>

<form class="form" onsubmit={(e) => { e.preventDefault(); save() }}>
  <h2>{editing ? '编辑 Profile' : '新建 Profile'}</h2>

  <label>
    名称
    <input bind:value={name} placeholder="例如 美国-洛杉矶-01" required />
  </label>

  <div class="pair">
    <label>
      分组
      <input bind:value={group} placeholder="可选" list="bw-form-groups" />
      <datalist id="bw-form-groups">
        {#each groups as g (g.name)}
          <option value={g.name}></option>
        {/each}
      </datalist>
    </label>
    <label>
      标签
      <input bind:value={tagInput} placeholder="逗号或空格分隔" />
    </label>
  </div>

  {#if !editing}
    <fieldset class="kind">
      <legend>类型</legend>
      <label class="radio">
        <input type="radio" bind:group={kind} value="fingerprint" />
        <span>
          <strong>指纹模式</strong>
          <small>独立环境与隔离身份，用于多账号</small>
        </span>
      </label>
      <label class="radio">
        <input type="radio" bind:group={kind} value="daily" />
        <span>
          <strong>日常模式</strong>
          <small>使用真实环境，不伪造任何指纹</small>
        </span>
      </label>
    </fieldset>
  {/if}

  <label class="check">
    <input type="checkbox" bind:checked={useProxy} />
    使用代理
  </label>

  {#if useProxy}
    <div class="proxy">
      <!-- 一行粘贴。放在各字段之前：代理商给的就是一行文本，
           先粘再核对比逐字段手抄少出错。 -->
      <label>
        粘贴一行代理
        <span class="pasteinput">
          <input
            bind:value={pasteLine}
            oninput={() => (pasteError = '')}
            onkeydown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault()
                applyPasteLine()
              }
            }}
            autocomplete="off"
            spellcheck="false"
            placeholder="198.51.100.10:31280:user:pass"
          />
          <Button variant="ghost" size="sm" onclick={applyPasteLine}>填入</Button>
        </span>
      </label>
      <p class="hint">
        支持 <code>host:port</code>、<code>host:port:user:pass</code>、
        <code>scheme://user:pass@host:port</code>。省略协议按 SOCKS5 处理。
      </p>
      {#if pasteError}
        <p class="hint danger-hint">{pasteError}</p>
      {/if}

      <label>
        协议
        <select bind:value={scheme} onchange={invalidateCheck}>
          <option value="socks5">SOCKS5</option>
          <option value="http">HTTP</option>
          <option value="https">HTTPS</option>
        </select>
      </label>
      <label>
        主机
        <input bind:value={host} oninput={invalidateCheck} placeholder="gate.example.com" />
      </label>
      <label>
        端口
        <input
          bind:value={port}
          oninput={invalidateCheck}
          inputmode="numeric"
          placeholder="7000"
        />
      </label>
      <label>
        用户名
        <input bind:value={username} oninput={invalidateCheck} autocomplete="off" />
      </label>
      <label>
        密码
        <input
          type="password"
          bind:value={password}
          oninput={invalidateCheck}
          autocomplete="new-password"
          placeholder={passwordPlaceholder}
        />
      </label>
      <p class="hint">
        带认证的代理会经本地转发接入，时区与语言按出口 IP 自动对齐。
      </p>
      {#if credProtection}
        <p class="hint" class:danger-hint={!credProtection.encrypted}>
          密码存储：{credProtection.detail}
        </p>
      {/if}

      <div class="checkrow">
        <Button
          variant="ghost"
          size="sm"
          onclick={runCheck}
          disabled={checking || needsPasswordToCheck}
        >
          {checking ? '检测中…' : '测试代理'}
        </Button>
        {#if needsPasswordToCheck}
          <small class="hint">重新填写密码后才能检测</small>
        {:else if !check}
          <small class="hint">保存前可先验证连通性与出口质量</small>
        {/if}
      </div>

      {#if check}
        <div class="result" class:ok={check.ok} class:bad={!check.ok}>
          {#if check.ok}
            <strong>连通正常</strong>
            <dl>
              {#if check.exit?.ip}
                <dt>出口 IP</dt>
                <dd>{check.exit.ip}</dd>
              {/if}
              {#if check.exit?.org}
                <dt>归属</dt>
                <dd>
                  {check.exit.org}{check.exit.asn ? ` (AS${check.exit.asn})` : ''}
                </dd>
              {/if}
              {#if check.aligned}
                <dt>将生效</dt>
                <dd>{check.aligned.timezone} / {check.aligned.locale}</dd>
              {/if}
              <dt>耗时</dt>
              <dd>{check.elapsedMs} ms</dd>
            </dl>
            {#each check.warnings ?? [] as w}
              <p class="warn">{w}</p>
            {/each}
          {:else}
            <strong>连接失败</strong>
            <p class="why">{check.err}</p>
          {/if}
        </div>
      {/if}
    </div>
  {/if}

  {#if kind === 'fingerprint' && devices.length > 0}
    <label>
      机型
      <select bind:value={deviceLabel}>
        <option value="">由种子随机抽取（推荐）</option>
        {#each devices as d (d.label)}
          <option value={d.label}>{d.label}</option>
        {/each}
      </select>
    </label>
    {#if isUsed && deviceChanged}
      <div class="result bad">
        <strong>更换机型会改变该 Profile 的设备特征</strong>
        <p class="why">
          机型决定操作系统、CPU 核数、GPU 与屏幕等一整组特征。该 Profile 已使用过，
          换掉机型后平台侧会视为同一账号更换了设备。
        </p>
        <label class="check">
          <input type="checkbox" bind:checked={confirmDeviceChange} />
          我已了解风险，仍要更换
        </label>
      </div>
    {:else}
      <p class="hint">
        默认由种子抽取，保证机型与其余指纹维度自洽。手动锁定适用于需要指定
        特定系统或显卡的场景。
      </p>
    {/if}
  {/if}

  <!-- 日常模式可换成系统 Chrome。不给指纹模式提供：官方 Chrome 不认识
       --fingerprint，会静默忽略全部伪造。 -->
  {#if kind === 'daily'}
    <label class="check">
      <input type="checkbox" bind:checked={useSystemBrowser} />
      使用系统已装的 Google Chrome
    </label>
    {#if useSystemBrowser}
      <div class="result">
        <p class="why">
          日常模式本就不做指纹伪造，用官方 Chrome 能拿回指纹内核缺少的能力：
          可登录 Google 账号、书签密码同步、自动更新、流媒体 DRM。
        </p>
        <p class="why">
          <strong>代理与目录隔离照旧生效</strong>，那两件事在启动器里完成，
          与用哪个浏览器无关。
        </p>
        <p class="why">
          切换浏览器后该 Profile 已有的登录态和扩展未必被完整识别，
          两者的配置格式同源但版本不同，可能需要重新登录。
        </p>
      </div>
    {:else}
      <p class="hint">
        默认使用指纹内核。它基于 ungoogled-chromium，登不了 Google 账号、
        没有同步，且版本更新滞后于官方 Chrome。
      </p>
    {/if}
  {/if}

  <!-- 只在新建时提供：匹配 GPU 靠筛选种子实现，而种子是已有 Profile 的
       身份根，改动等同于换设备。 -->
  {#if kind === 'fingerprint' && !editing}
    <label class="check">
      <input type="checkbox" bind:checked={matchHostGPU} />
      筛选种子以匹配本机 GPU 厂商（可通过 Cloudflare）
    </label>
    {#if matchHostGPU}
      <div class="result">
        <p class="why">
          实测 Cloudflare 拦截的判据是伪造 GPU 与本机跨厂商，而非存在伪造。
          GPU 由内核从种子派生、没有参数可指定，因此只能反复生成种子并实测，
          直到派生出与本机同厂商的型号。
        </p>
        <p class="why">
          <strong>创建会变慢：</strong>每个候选种子要启动一次浏览器（约 2 秒），
          最多试 24 个。筛不出来会直接报错，不会静默给一个未筛选的种子。
        </p>
        {#if hostGPU}
          <p class="why">本机 GPU：{hostGPU.family}（{hostGPU.renderer}）</p>
        {/if}
        <Button variant="ghost" size="sm" onclick={detectGPU} disabled={detectingGPU}>
          {detectingGPU ? '探测中…' : '先探测本机 GPU'}
        </Button>
        {#if gpuError}
          <p class="why"><strong>探测失败：</strong>{gpuError}</p>
        {/if}
      </div>
    {/if}
  {/if}

  <!-- 用系统 Chrome 时不显示内核选择：这个 Profile 不会用到指纹内核，
       留着一个无效的下拉只会让人误以为它生效了。 -->
  {#if kernels.length > 0 && !effectiveSystemBrowser}
    <label>
      内核版本
      <select bind:value={kernelVersion}>
        <option value="">跟随最新内核</option>
        {#each kernels as k}
          <option value={k.version}>{k.version}</option>
        {/each}
      </select>
    </label>
    {#if crossesMajor}
      <div class="result bad">
        <strong>更换内核大版本会改变该 Profile 的指纹</strong>
        <p class="why">
          该 Profile 已使用过内核 {initial?.kernelVersion}。不同大版本的指纹算法不同，
          切换后平台侧会视为同一账号更换了设备。
        </p>
        <label class="check">
          <input type="checkbox" bind:checked={confirmKernelChange} />
          我已了解风险，仍要切换
        </label>
      </div>
    {:else}
      <p class="hint">
        锁定版本可避免内核升级后指纹漂移；未使用过的 Profile 可随时更换。
      </p>
    {/if}
  {/if}

  <fieldset class="startup">
    <legend>打开的页面</legend>

    <label>
      新标签页
      <input
        bind:value={newTabURL}
        placeholder="留空则用内核默认（空白页）"
        spellcheck="false"
      />
    </label>
    <p class="hint">
      每次打开新标签页都会到这个地址。ungoogled-chromium 移除了 Google 的
      新标签页组件，默认是空白页，填一个常用页面会方便得多。
    </p>

    <label class="check">
      <input
        type="radio"
        bind:group={startupMode}
        value="newtab"
      />
      启动时打开新标签页
    </label>
    <label class="check">
      <input type="radio" bind:group={startupMode} value="urls" />
      启动时打开指定页面
    </label>

    {#if startupMode === 'urls'}
      <label>
        启动页（每行一个）
        <textarea
          bind:value={startupURLText}
          rows="3"
          spellcheck="false"
          placeholder={'https://example.com\nhttps://example.org'}
        ></textarea>
      </label>
      <p class="hint">
        只在启动那一次打开。多个地址会各占一个标签页。
      </p>
    {/if}
  </fieldset>

  <label>
    备注
    <input bind:value={notes} placeholder="可选" />
  </label>

  {#if kind === 'fingerprint'}
    <div class="advanced">
      <button
        type="button"
        class="disclose"
        aria-expanded={showAdvanced}
        onclick={() => (showAdvanced = !showAdvanced)}
      >
        {showAdvanced ? '▾' : '▸'} 高级选项
        {#if disabledSpoofing.length > 0 || overrideGeo}
          <span class="badge">已修改</span>
        {/if}
      </button>

      {#if showAdvanced}
        <div class="advbody">
          <label class="check">
            <input type="checkbox" bind:checked={overrideGeo} />
            手动指定地理信息（跳过出口 IP 反查）
          </label>
          {#if overrideGeo}
            <div class="geo">
              <label>
                国家码
                <input bind:value={geoCountry} placeholder="US" maxlength="2" />
              </label>
              <label>
                时区
                <input bind:value={geoTimezone} placeholder="America/Los_Angeles" />
              </label>
              <label>
                语言
                <input bind:value={geoLocale} placeholder="en-US" />
              </label>
            </div>
            <p class="hint danger-hint">
              指定的值与代理实际出口不一致会构成明显矛盾。除非确知出口位置，
              否则留空让其按出口 IP 自动对齐。
            </p>
          {/if}

          {#if spoofTargets.length > 0}
            <fieldset class="spoof">
              <legend>关闭伪造（仅排障用）</legend>
              {#each spoofTargets as t}
                <label class="check">
                  <input
                    type="checkbox"
                    checked={disabledSpoofing.includes(t)}
                    onchange={() => toggleSpoof(t)}
                  />
                  {t}
                </label>
              {/each}
              <p class="hint danger-hint">
                逐项关闭可定位是哪一项触发了检测。每关一项就少一层伪装，
                排查完请全部恢复。
              </p>
            </fieldset>
          {/if}
        </div>
      {/if}
    </div>
  {/if}

  {#if error}
    <p class="error" role="alert">{error}</p>
  {/if}

  <div class="actions">
    {#if editing}
      <!-- 放在左侧并与保存拉开距离：这是不可恢复操作，不该紧邻常用按钮。 -->
      <Button variant="danger" disabled={saving} onclick={clearData}>
        清除浏览数据
      </Button>
    {/if}
    <span class="spacer"></span>
    <Button variant="ghost" onclick={onCancel}>取消</Button>
    <Button type="submit" disabled={saving}>{saving ? '保存中…' : '保存'}</Button>
  </div>
</form>

<style>
  .form {
    display: flex;
    flex-direction: column;
    gap: var(--sp-4);
  }
  h2 {
    font-size: var(--fs-xl);
  }

  /* 表单项统一为「标签在上、控件在下」。控件本身的外观由 base.css 提供。 */
  label {
    display: flex;
    flex-direction: column;
    gap: var(--sp-1);
    font-size: var(--fs-base);
    color: var(--c-text-muted);
  }
  .pair {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--sp-3);
  }

  fieldset.kind,
  fieldset.startup,
  fieldset.spoof {
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
  }

  /* 单选/复选改为横向：勾选框与文字同行，文字不该被压到下一行。 */
  .radio,
  .check {
    flex-direction: row;
    align-items: center;
    gap: var(--sp-2);
    cursor: pointer;
  }
  .radio span {
    display: flex;
    flex-direction: column;
  }
  .radio strong {
    color: var(--c-text);
    font-weight: 500;
  }
  .radio small {
    color: var(--c-text-faint);
    font-size: var(--fs-sm);
  }

  .proxy {
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
    padding: var(--sp-3);
    background: var(--c-sunken);
    border: 1px solid var(--c-border-strong);
    border-radius: var(--r-sm);
  }
  /* 代理块自身已是 sunken 底，里面的输入框需要反过来提亮才分得出层次。 */
  .proxy :global(input),
  .proxy :global(select) {
    background: var(--c-raised);
  }
  /* code 的全局底色也是 sunken，在代理块里会糊成一片，同样提亮。 */
  .proxy code {
    background: var(--c-raised);
  }
  /* 粘贴框与「填入」按钮同行：label 默认纵向排列，这里需要横向。 */
  .pasteinput {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
  }
  .pasteinput input {
    flex: 1;
    min-width: 0;
  }
  .checkrow {
    display: flex;
    align-items: center;
    gap: var(--sp-3);
    flex-wrap: wrap;
  }

  .advanced {
    border-top: 1px solid var(--c-border);
    padding-top: var(--sp-3);
  }
  .disclose {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    background: none;
    border: 0;
    padding: 0;
    font-family: inherit;
    font-size: var(--fs-base);
    color: var(--c-text-muted);
    cursor: pointer;
  }
  .disclose:hover {
    color: var(--c-text);
  }
  /* 折叠状态下也要能看出里面有非默认设置，否则用户会忘记自己改过。 */
  .badge {
    background: var(--c-warn-soft);
    border: 1px solid var(--c-warn);
    color: var(--c-warn-text);
    border-radius: var(--r-pill);
    padding: 0 var(--sp-2);
    font-size: var(--fs-xs);
  }
  .advbody {
    display: flex;
    flex-direction: column;
    gap: var(--sp-3);
    margin-top: var(--sp-3);
  }
  .geo {
    display: grid;
    grid-template-columns: 80px 1fr 1fr;
    gap: var(--sp-2);
  }

  /* 检测结果与风险确认块共用：左边框颜色表达结论，正文保持中性色。 */
  .result {
    border: 1px solid var(--c-border-strong);
    border-left-width: 3px;
    border-radius: var(--r-sm);
    padding: var(--sp-2) var(--sp-3);
    background: var(--c-sunken);
    font-size: var(--fs-base);
    display: flex;
    flex-direction: column;
    gap: var(--sp-2);
  }
  .result.ok {
    border-left-color: var(--c-ok);
  }
  .result.bad {
    border-left-color: var(--c-err);
  }
  .result strong {
    color: var(--c-text);
    font-weight: 600;
  }
  .result dl {
    margin: 0;
    display: grid;
    grid-template-columns: auto 1fr;
    gap: var(--sp-1) var(--sp-3);
    font-size: var(--fs-sm);
  }
  .result dt {
    color: var(--c-text-faint);
  }
  .result dd {
    margin: 0;
    color: var(--c-text-muted);
    word-break: break-all;
  }

  /* 机房 IP 一类的警告不阻断保存，用醒目但非报错的配色。 */
  .warn {
    font-size: var(--fs-sm);
    color: var(--c-warn-text);
    line-height: var(--lh-base);
  }
  .why {
    font-size: var(--fs-sm);
    color: var(--c-text-muted);
    line-height: var(--lh-base);
    word-break: break-word;
  }
  .hint {
    font-size: var(--fs-sm);
    color: var(--c-text-faint);
    line-height: var(--lh-base);
  }
  .danger-hint {
    color: var(--c-warn-text);
  }
  .error {
    color: var(--c-err-text);
    font-size: var(--fs-base);
  }

  .actions {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    padding-top: var(--sp-2);
    border-top: 1px solid var(--c-border);
  }
  .spacer {
    flex: 1;
  }
</style>
