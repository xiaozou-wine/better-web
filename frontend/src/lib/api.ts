// Wails 绑定的薄封装：统一错误形态，并集中放置界面用到的常量。
//
// Wails 把 Go 的 error 以 rejected promise 的形式抛出，reason 是字符串。
// 这里统一转成 Error 对象，让调用处能用同一套 try/catch 处理。
import {
  AssignGroupBatch,
  CheckProxy,
  CredentialProtection,
  CreateProfile,
  CreateShortcut,
  DefaultBatchConcurrency,
  DeleteBatch,
  DeleteProfile,
  DeleteProfileData,
  DetectHostGPU,
  ExportBundle,
  GetProfile,
  GroupTree,
  GroupUnassignedKey,
  ImportBundle,
  ImportProxies,
  PickExportPath,
  PickImportPath,
  InstallKernel,
  KernelsDir,
  ListAvailableKernels,
  ListDeviceProfiles,
  ListKernels,
  ListProfiles,
  ListSpoofTargets,
  OpenDefaultAppsSettings,
  ParseProxyLine,
  QueryProfiles,
  RegisterURLHandler,
  RunningSessions,
  SetURLHandler,
  StartBatch,
  StartProfile,
  StopBatch,
  StopProfile,
  TagBatch,
  UnregisterURLHandler,
  UpdateProfile,
  URLHandler,
} from '../../wailsjs/go/main/App'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import type { app, kernel, model, session, store } from '../../wailsjs/go/models'

export type ProfileView = app.ProfileView
export type CreateRequest = app.CreateRequest
export type UpdateRequest = app.UpdateRequest
export type SessionStatus = session.Status
export type Kernel = kernel.Kernel
export type Release = kernel.Release
export type DeviceProfile = model.DeviceProfile
export type Proxy = model.Proxy
export type ProxyCheck = app.ProxyCheck
export type BundleImportOptions = app.BundleImportOptions
export type BundleImportResult = app.BundleImportResult
export type CredentialProtectionInfo = app.CredentialProtection
export type Startup = model.Startup
export type Filter = store.Filter
export type GroupTreeInfo = app.GroupTree
export type GroupStat = store.GroupStat
export type TagStat = store.TagStat
export type BatchSummary = app.BatchSummary
export type BatchResult = app.BatchResult
export type ImportRequest = app.ImportRequest
export type ImportResult = app.ImportResult
export type HostGPUInfo = app.HostGPUInfo
export type URLHandlerInfo = app.URLHandlerView

/** 启动模式，与 Go 侧 model.StartupMode 对应。 */
export type StartupMode = 'newtab' | 'urls'

/** 批量标签操作的模式，与 Go 侧 app.TagBatchMode 对应。 */
export type TagMode = 'add' | 'remove' | 'replace'

/** 内核安装进度，与 Go 侧 app.InstallProgress 对应。 */
export interface InstallProgress {
  version: string
  downloaded: number
  total: number
  done: boolean
  err?: string
}

/** 把 Wails 抛出的字符串错误统一成 Error。 */
function toError(e: unknown): Error {
  if (e instanceof Error) return e
  return new Error(typeof e === 'string' ? e : JSON.stringify(e))
}

async function call<T>(fn: () => Promise<T>): Promise<T> {
  try {
    return await fn()
  } catch (e) {
    throw toError(e)
  }
}

export const api = {
  listProfiles: () => call(ListProfiles),
  getProfile: (id: string) => call(() => GetProfile(id)),
  createProfile: (req: CreateRequest) => call(() => CreateProfile(req)),
  updateProfile: (req: UpdateRequest) => call(() => UpdateProfile(req)),
  deleteProfile: (id: string) => call(() => DeleteProfile(id)),
  deleteProfileData: (id: string) => call(() => DeleteProfileData(id)),
  start: (id: string) => call(() => StartProfile(id)),
  stop: (id: string) => call(() => StopProfile(id)),
  runningSessions: () => call(RunningSessions),
  listKernels: () => call(ListKernels),
  listAvailableKernels: () => call(ListAvailableKernels),
  installKernel: (rel: Release) => call(() => InstallKernel(rel)),
  kernelsDir: () => call(KernelsDir),
  listDeviceProfiles: () => call(ListDeviceProfiles),
  listSpoofTargets: () => call(ListSpoofTargets),
  /**
   * 探测本机真实 GPU 厂商族。会启动一次浏览器（约 2 秒），
   * 因此只在用户主动点击时调用，不要放进表单初始化。
   */
  detectHostGPU: (kernelVersion: string) =>
    call(() => DetectHostGPU(kernelVersion)),

  /**
   * 检测代理连通性与出口质量，可在保存前先验。
   * 不启动浏览器，仅经上游发一次探测请求。
   */
  checkProxy: (p: Proxy) => call(() => CheckProxy(p)),

  /**
   * 解析一行代理配置，供表单把粘贴的一行拆进各字段。
   * 格式与批量导入一致，见 model.ParseProxy。
   */
  parseProxyLine: (line: string) => call(() => ParseProxyLine(line)),
  credentialProtection: () => call(CredentialProtection),

  /** 在桌面创建直达该 profile 的快捷方式，返回快捷方式路径。 */
  createShortcut: (id: string) => call(() => CreateShortcut(id)),

  // 系统链接接管：让其他应用点开的链接走指定的 profile 实例。
  //
  // registered 与 isDefault 是两件事：注册只是让 better-web 出现在系统的
  // 候选列表里，设为默认只有用户能做——系统不允许应用自己抢。界面必须
  // 分别呈现，否则「已注册但没生效」这个最常见的中间态无法表达。
  urlHandler: () => call(URLHandler),
  /** profileId 传空串表示关闭接管。 */
  setURLHandler: (profileId: string, incognito: boolean) =>
    call(() => SetURLHandler(profileId, incognito)),
  registerURLHandler: () => call(RegisterURLHandler),
  unregisterURLHandler: () => call(UnregisterURLHandler),
  /** 打开系统默认应用设置页，供用户完成最后一步手动选择。 */
  openDefaultAppsSettings: () => call(OpenDefaultAppsSettings),

  // 配置文件导出导入。与 importProxies 的区别：那个从文本批量建号，
  // 这两个是把现有配置搬到别处（备份、换机器）。
  pickExportPath: () => call(PickExportPath),
  pickImportPath: () => call(PickImportPath),
  /** withSecrets 为 true 时导出文件含代理密码明文，调用前须提示用户。 */
  exportBundle: (path: string, withSecrets: boolean) =>
    call(() => ExportBundle(path, withSecrets)),
  importBundle: (path: string, opt: BundleImportOptions) =>
    call(() => ImportBundle(path, opt)),

  // 分组与筛选
  queryProfiles: (f: Filter) => call(() => QueryProfiles(f)),
  groupTree: () => call(GroupTree),
  groupUnassignedKey: () => call(GroupUnassignedKey),

  // 批量操作
  startBatch: (ids: string[], concurrency = 0) =>
    call(() => StartBatch(ids, concurrency)),
  stopBatch: (ids: string[]) => call(() => StopBatch(ids)),
  deleteBatch: (ids: string[]) => call(() => DeleteBatch(ids)),
  assignGroupBatch: (ids: string[], group: string) =>
    call(() => AssignGroupBatch(ids, group)),
  // Wails 为 Go 的具名字符串类型（app.TagBatchMode）生成了签名引用，
  // 但没有在 models.ts 里定义该类型，导致引用悬空。运行时它就是字符串，
  // 因此在这唯一的调用点做一次断言，其余代码仍用 TagMode 联合类型约束。
  tagBatch: (ids: string[], tags: string[], mode: TagMode) =>
    call(() => TagBatch(ids, tags, mode as unknown as Parameters<typeof TagBatch>[2])),
  defaultBatchConcurrency: () => call(DefaultBatchConcurrency),

  // 代理批量导入
  importProxies: (req: ImportRequest) => call(() => ImportProxies(req)),

  /** 订阅内核安装进度，返回取消订阅函数。 */
  onInstallProgress: (fn: (p: InstallProgress) => void) =>
    EventsOn('kernel:install-progress', fn),
}

/** 把字节数格式化成便于阅读的大小。 */
export function formatBytes(n: number): string {
  if (n <= 0) return '0 MB'
  const mb = n / 1024 / 1024
  return mb >= 1024 ? `${(mb / 1024).toFixed(2)} GB` : `${mb.toFixed(1)} MB`
}

/** 会话状态到中文标签的映射，与 Go 侧 session.State 对应。 */
export const stateLabels: Record<string, string> = {
  starting: '启动中',
  running: '运行中',
  stopped: '已停止',
  failed: '启动失败',
}

/** profile 类型到中文标签的映射。 */
export const kindLabels: Record<string, string> = {
  daily: '日常',
  fingerprint: '指纹',
}
