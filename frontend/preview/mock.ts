/**
 * 仅用于视觉预览：桩掉 Wails 的 window.go 绑定，喂一批假数据，
 * 让界面能在普通浏览器里渲染出来做主题与布局检查。
 *
 * 不参与生产构建（preview/ 目录只被 preview/index.html 引用）。
 */

const profiles = [
  {
    id: 'p1',
    name: '美国-洛杉矶-01',
    kind: 'fingerprint',
    state: 'running',
    group: '主力',
    tags: ['steam', '已验证'],
    seed: 8821337742,
    notes: '主账号，勿动',
    proxy: { scheme: 'socks5', host: '198.51.100.24', port: 1080, hasPassword: true },
    fingerprint: {
      device: { label: 'Windows 11 / RTX 3060' },
      timezone: 'America/Los_Angeles',
      locale: 'en-US',
    },
    exit: { ip: '198.51.100.24', org: 'Example Residential', asn: 64512 },
    startup: { mode: 'newtab', urls: [], newTabUrl: 'https://store.steampowered.com' },
    warnings: [],
  },
  {
    id: 'p2',
    name: '日本-东京-02',
    kind: 'fingerprint',
    state: 'starting',
    group: '主力',
    tags: ['备用'],
    seed: 5510028834,
    notes: '',
    proxy: { scheme: 'http', host: 'gate.example.com', port: 7000, hasPassword: false },
    fingerprint: {
      device: { label: 'macOS 14 / Apple M2' },
      timezone: 'Asia/Tokyo',
      locale: 'ja-JP',
    },
    exit: null,
    startup: null,
    warnings: ['出口 IP 归属机房网段，平台侧风控更严格'],
  },
  {
    id: 'p3',
    name: '日常浏览',
    kind: 'daily',
    state: 'stopped',
    group: '',
    tags: [],
    seed: 0,
    notes: '本机真实环境，用来查资料',
    proxy: null,
    fingerprint: null,
    exit: null,
    startup: null,
    warnings: [],
  },
  {
    id: 'p4',
    name: '德国-法兰克福-07',
    kind: 'fingerprint',
    state: 'failed',
    group: '欧洲',
    tags: ['待排查'],
    seed: 1902847733,
    notes: '',
    proxy: { scheme: 'socks5', host: '203.0.113.88', port: 9050, hasPassword: true },
    fingerprint: {
      device: { label: 'Windows 10 / GTX 1660' },
      timezone: 'Europe/Berlin',
      locale: 'de-DE',
    },
    exit: null,
    startup: null,
    warnings: [],
  },
]

const tree = {
  total: 12,
  unassigned: 1,
  groups: [
    { name: '主力', count: 5 },
    { name: '欧洲', count: 4 },
    { name: '测试', count: 2 },
  ],
  tags: [
    { name: 'steam', count: 3 },
    { name: '已验证', count: 5 },
    { name: '备用', count: 2 },
    { name: '待排查', count: 1 },
  ],
}

const stub = {
  ListProfiles: () => profiles,
  QueryProfiles: () => profiles,
  GroupTree: () => tree,
  GroupUnassignedKey: () => '\u0000unassigned',
  ListKernels: () => [{ version: '131.0.6778.86' }],
  ListAvailableKernels: () => [],
  KernelsDir: () => 'C:\\Users\\demo\\.better-web\\kernels',
  ListDeviceProfiles: () => [],
  ListSpoofTargets: () => [],
  CredentialProtection: () => ({ encrypted: true, detail: 'DPAPI 加密（仅当前用户可解）' }),
  DefaultBatchConcurrency: () => 4,
  RunningSessions: () => [],
  // 未桩的方法一律返回 null（见下面的 Proxy），而表单会去读返回值的字段，
  // 于是预览里点「填入」会显示一个 TypeError 冒充的解析失败。桩掉它。
  ParseProxyLine: () => ({
    scheme: 'socks5',
    host: '198.51.100.10',
    port: 31280,
    username: 'user',
    password: 'pass',
  }),
  // 不桩它的话返回 null，而面板用 {#if info} 兜住 null，
  // 于是预览里这块整个不渲染——看起来像组件写坏了。
  //
  // 桩成"已注册但还不是系统默认"：那是实际使用中最常见、也最需要看清
  // 提示文案的中间态。
  URLHandler: () => ({
    registered: true,
    isDefault: false,
    supported: true,
    profileId: 'p3',
    profileName: '日常浏览',
    incognito: false,
  }),
}

// Wails 的调用形态是 window.go.main.App.<Method>()，返回 Promise。
;(window as unknown as Record<string, unknown>).go = {
  main: {
    App: new Proxy(stub, {
      get(target: Record<string, unknown>, key: string) {
        const fn = target[key]
        if (typeof fn === 'function') {
          return (...args: unknown[]) => Promise.resolve((fn as (...a: unknown[]) => unknown)(...args))
        }
        // 未桩的方法返回空，避免抛错打断渲染。
        return () => Promise.resolve(null)
      },
    }),
  },
}

/*
 * runtime 桩。wailsjs 的 EventsOn 是转调 EventsOnMultiple 实现的，
 * 只桩 EventsOn 会在订阅内核进度时抛 TypeError 并中断整个 mount。
 */
;(window as unknown as Record<string, unknown>).runtime = {
  EventsOnMultiple: () => () => {},
  EventsOn: () => () => {},
  EventsOnce: () => () => {},
  EventsOff: () => {},
  EventsOffAll: () => {},
  EventsEmit: () => {},
  LogPrint: () => {},
  LogDebug: () => {},
  LogInfo: () => {},
  LogWarning: () => {},
  LogError: () => {},
  WindowSetTitle: () => {},
}

// 允许用 ?theme=light|dark 指定主题，供截图脚本逐个主题取图。
const wanted = new URLSearchParams(location.search).get('theme')
if (wanted === 'light' || wanted === 'dark') {
  localStorage.setItem('bw.theme', wanted)
}
