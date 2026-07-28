<script lang="ts">
  /**
   * 系统链接接管的配置面板。
   *
   * 两个状态必须分开呈现：registered 是「已出现在系统的候选列表里」，
   * isDefault 是「用户已在系统设置里选了它」。后者只有用户能做——系统不允许
   * 应用自己抢默认浏览器（UserChoice 带未公开算法的校验值）。合成一个状态
   * 会让「已注册但没生效」这个最常见的中间态无法表达，而那正是需要提示的时候。
   */
  import { api, type ProfileView, type URLHandlerInfo } from './api'
  import Button from './ui/Button.svelte'

  let { profiles, onClose }: {
    profiles: ProfileView[]
    onClose: () => void
  } = $props()

  let info = $state<URLHandlerInfo | null>(null)
  let selected = $state('')
  let incognito = $state(false)
  let busy = $state(false)
  let error = $state('')
  let notice = $state('')

  // 只列日常模式的 profile。链接接管是「随手点开一个链接」的场景，用指纹
  // profile 承接等于把随机链接混进养号环境，那正是多账号场景要避免的。
  const dailyProfiles = $derived(profiles.filter((p) => p.kind === 'daily'))

  // 配置指向的 profile 已被删除时给出提示：ProfileID 有值而 ProfileName 为空。
  const targetMissing = $derived(
    Boolean(info?.profileId) && !info?.profileName,
  )

  $effect(() => {
    load()
  })

  async function load() {
    try {
      const v = await api.urlHandler()
      info = v
      selected = v.profileId ?? ''
      incognito = v.incognito
    } catch (e) {
      error = (e as Error).message
    }
  }

  async function save() {
    busy = true
    error = ''
    notice = ''
    try {
      await api.setURLHandler(selected, incognito)
      await load()
      notice = selected ? '已保存' : '已关闭链接接管'
    } catch (e) {
      error = (e as Error).message
    } finally {
      busy = false
    }
  }

  async function register() {
    busy = true
    error = ''
    notice = ''
    try {
      await api.registerURLHandler()
      await load()
      notice = '已注册。还需在系统设置里把 better-web 选为默认浏览器才会生效。'
    } catch (e) {
      error = (e as Error).message
    } finally {
      busy = false
    }
  }

  async function unregister() {
    busy = true
    error = ''
    notice = ''
    try {
      await api.unregisterURLHandler()
      await load()
      notice = '已清除注册信息'
    } catch (e) {
      error = (e as Error).message
    } finally {
      busy = false
    }
  }

  async function openSettings() {
    error = ''
    try {
      await api.openDefaultAppsSettings()
    } catch (e) {
      error = (e as Error).message
    }
  }
</script>

<div class="panel">
  <div class="head">
    <div>
      <strong>系统链接接管</strong>
      <p class="desc">
        其他应用（编辑器、聊天软件、邮件）点开的链接交给指定的 Profile 打开，
        代理与目录隔离照旧生效。Profile 没在运行就完整启动它，已经在运行就新开一个窗口。
      </p>
    </div>
    <button class="close" title="关闭" onclick={onClose}>×</button>
  </div>

  {#if info && !info.supported}
    <p class="hint">
      当前系统尚未支持注册默认浏览器。命令行的
      <code>--open-url=&lt;链接&gt;</code> 仍然可用。
    </p>
  {:else if info}
    <div class="status">
      <span class="dot" class:on={info.registered}></span>
      <span>{info.registered ? '已注册为候选浏览器' : '未注册'}</span>
      <span class="sep">·</span>
      <span class="dot" class:on={info.isDefault}></span>
      <span>{info.isDefault ? '已是系统默认浏览器' : '尚未被选为系统默认'}</span>
    </div>

    {#if info.registered && !info.isDefault}
      <p class="hint">
        注册只是让 better-web 出现在系统的候选列表里。系统不允许应用自己抢默认
        浏览器，最后一步要你手动去「设置 → 默认应用」里选中它。
      </p>
    {/if}

    <label>
      接管链接的 Profile
      <select bind:value={selected}>
        <option value="">不接管</option>
        {#each dailyProfiles as p (p.id)}
          <option value={p.id}>{p.name}</option>
        {/each}
      </select>
    </label>

    {#if dailyProfiles.length === 0}
      <p class="hint">
        还没有日常模式的 Profile。指纹 Profile 不出现在这里：把随手点开的链接
        混进养号环境，正是多账号场景要避免的事。
      </p>
    {:else}
      <p class="hint">只列日常模式的 Profile，原因同上。</p>
    {/if}

    {#if targetMissing}
      <p class="err" role="alert">
        原先指定的 Profile 已被删除，请重新选一个，否则点链接会报错。
      </p>
    {/if}

    <label class="check">
      <input type="checkbox" bind:checked={incognito} />
      用无痕窗口打开
    </label>
    <p class="hint">
      无痕窗口不保留 Cookie 与历史。只对日常模式有意义——指纹模式下无痕并不会
      改变出口 IP 与指纹，只是丢掉了养号需要的登录态，因此那个组合会被拒绝。
    </p>

    <div class="actions">
      <Button disabled={busy} onclick={save}>{busy ? '处理中…' : '保存'}</Button>
      {#if info.registered}
        <Button disabled={busy} onclick={openSettings}>打开系统默认应用设置</Button>
        <Button disabled={busy} onclick={unregister}>取消注册</Button>
      {:else}
        <Button disabled={busy} onclick={register}>注册为候选浏览器</Button>
      {/if}
    </div>
  {/if}

  {#if notice}
    <p class="ok" role="status">{notice}</p>
  {/if}
  {#if error}
    <p class="err" role="alert">{error}</p>
  {/if}
</div>

<style>
  .panel {
    background: var(--c-raised);
    border: 1px solid var(--c-border);
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
  .close {
    background: none;
    border: 0;
    color: var(--c-text-faint);
    font-size: var(--fs-lg);
    line-height: 1;
    cursor: pointer;
    padding: 0 var(--sp-1);
  }
  .close:hover {
    color: var(--c-text);
  }
  .desc {
    margin-top: var(--sp-1);
    font-size: var(--fs-base);
    line-height: var(--lh-loose);
    color: var(--c-text-muted);
  }
  .status {
    display: flex;
    align-items: center;
    gap: var(--sp-2);
    font-size: var(--fs-base);
  }
  .sep {
    color: var(--c-text-faint);
  }
  .dot {
    width: 8px;
    height: 8px;
    border-radius: var(--r-pill);
    background: var(--c-text-faint);
    flex: none;
  }
  .dot.on {
    background: var(--c-accent);
  }
  label {
    display: flex;
    flex-direction: column;
    gap: var(--sp-1);
    font-size: var(--fs-base);
  }
  label.check {
    flex-direction: row;
    align-items: center;
    gap: var(--sp-2);
  }
  .hint {
    font-size: var(--fs-sm);
    line-height: var(--lh-loose);
    color: var(--c-text-muted);
  }
  .actions {
    display: flex;
    gap: var(--sp-2);
    flex-wrap: wrap;
  }
  .ok {
    font-size: var(--fs-base);
    color: var(--c-text);
  }
  .err {
    font-size: var(--fs-base);
    color: var(--c-err-text);
  }
</style>
