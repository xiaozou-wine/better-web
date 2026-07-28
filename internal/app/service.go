package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"better-web/internal/fingerprint"
	"better-web/internal/geo"
	"better-web/internal/kernel"
	"better-web/internal/model"
	"better-web/internal/proxy"
	"better-web/internal/secret"
	"better-web/internal/session"
	"better-web/internal/shortcut"
	"better-web/internal/store"

	"github.com/google/uuid"
)

// Service 是前端可调用的全部能力。方法可并发调用。
type Service struct {
	paths    Paths
	store    *store.Store
	kernels  *kernel.Store
	sessions *session.Manager

	// CheckProxyTimeoutOverride 覆盖 CheckProxy 的超时，仅供测试使用。
	// 留零时用 CheckProxyTimeout。生产代码不应设置它——缩短超时会让
	// 慢速住宅代理被误判为不可用。
	CheckProxyTimeoutOverride time.Duration

	// gpuMu 保护 gpuCache。Service 的方法可并发调用，而导出 bundle 与
	// 筛选种子都会读写它。
	gpuMu sync.Mutex
	// gpuCache 缓存各内核版本探测出的宿主机 GPU，键为内核完整版本号。
	//
	// 缓存的理由：每次探测要冷启动一次浏览器（约 2 秒），而宿主机显卡不会
	// 在程序运行期间变。见 DetectHostGPU 的说明。
	gpuCache map[string]HostGPUInfo
}

// New 初始化服务：建目录、开数据库、准备内核与会话管理器。
func New(paths Paths) (*Service, error) {
	if err := paths.EnsureDirs(); err != nil {
		return nil, err
	}
	db, err := store.Open(paths.DB)
	if err != nil {
		return nil, err
	}
	kernels := kernel.NewStore(paths.Kernels)
	return &Service{
		paths:    paths,
		store:    db,
		kernels:  kernels,
		sessions: session.NewManager(kernels),
	}, nil
}

// Close 停止全部会话并释放资源，供应用退出时调用。
func (s *Service) Close() error {
	s.sessions.StopAll()
	return s.store.Close()
}

// ProfileView 是发给前端的 profile 视图。
//
// 与 model.Profile 的区别：代理密码被替换为"是否已设置"的布尔标记。
// 前端不需要密码原文，把凭据送进渲染层等于让它出现在 DevTools、
// 日志和崩溃报告里。
type ProfileView struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Kind          model.ProfileKind `json:"kind"`
	Seed          int32             `json:"seed"`
	ProfileDir    string            `json:"profileDir"`
	Proxy         *ProxyView        `json:"proxy,omitempty"`
	GeoOverride   *model.Geo        `json:"geoOverride,omitempty"`
	KernelVersion string            `json:"kernelVersion,omitempty"`
	ExtraArgs     []string          `json:"extraArgs,omitempty"`
	Notes         string            `json:"notes,omitempty"`
	// DisableSpoofing 非空表示该 profile 关闭了部分伪造，界面应显著提示。
	DisableSpoofing []model.SpoofTarget `json:"disableSpoofing,omitempty"`
	Group           string              `json:"group,omitempty"`
	Tags            []string            `json:"tags,omitempty"`
	// DeviceLabel 非空表示锁定了机型；为空表示机型由种子抽取。
	DeviceLabel string `json:"deviceLabel,omitempty"`
	// UseSystemBrowser 表示该 profile 用系统 Chrome 启动而非指纹内核。
	// 界面应显式呈现：它决定了能不能登 Google 账号，也决定了有无指纹伪造。
	UseSystemBrowser bool `json:"useSystemBrowser,omitempty"`
	// Startup 是启动页与新标签页配置，nil 表示用内核默认的空白新标签页。
	Startup   *model.Startup `json:"startup,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
	LastUseAt time.Time      `json:"lastUseAt,omitzero"`
	State     session.State  `json:"state"`
	// Fingerprint 是该 profile 当前会推导出的指纹预览，便于界面核对一致性。
	Fingerprint *model.Fingerprint `json:"fingerprint,omitempty"`

	// Exit 是本次会话实际的出口画像，仅在运行中且经代理时有值。
	// 与 Fingerprint 的区别：那是配置预览，这是运行时事实。
	Exit *geo.ExitInfo `json:"exit,omitempty"`
	// Warnings 是本次会话的运行时警告，如出口为机房 IP。
	// 只在运行中有值，界面应显著提示——这类问题不阻断启动，
	// 若不呈现出来用户就永远不会知道。
	Warnings []string `json:"warnings,omitempty"`
}

// ProxyView 是不含密码的代理视图。
type ProxyView struct {
	Scheme   model.ProxyScheme `json:"scheme"`
	Host     string            `json:"host"`
	Port     int               `json:"port"`
	Username string            `json:"username,omitempty"`
	// HasPassword 表示已设置密码，但不回传密码本身。
	HasPassword bool `json:"hasPassword"`
}

func toProxyView(p *model.Proxy) *ProxyView {
	if p == nil {
		return nil
	}
	return &ProxyView{
		Scheme: p.Scheme, Host: p.Host, Port: p.Port,
		Username: p.Username, HasPassword: p.Password != "",
	}
}

func (s *Service) toView(p *model.Profile) ProfileView {
	st := s.sessions.Status(p.ID)
	v := ProfileView{
		ID: p.ID, Name: p.Name, Kind: p.Kind, Seed: p.Seed,
		ProfileDir: p.ProfileDir, Proxy: toProxyView(p.Proxy),
		GeoOverride: p.GeoOverride, KernelVersion: p.KernelVersion,
		ExtraArgs: p.ExtraArgs, Notes: p.Notes,
		DisableSpoofing: p.DisableSpoofing,
		Group:           p.Group, Tags: p.Tags,
		DeviceLabel: p.DeviceLabel, Startup: p.Startup,
		UseSystemBrowser: p.UseSystemBrowser,
		CreatedAt:        p.CreatedAt, LastUseAt: p.LastUseAt,
		State:    st.State,
		Exit:     st.Exit,
		Warnings: st.Warnings,
	}
	if p.Kind == model.KindFingerprint {
		fp := fingerprint.DeriveWithDeviceLabel(
			p.Seed, p.GeoOverride, p.DeviceLabel, p.KernelVersion)
		v.Fingerprint = &fp
	}
	return v
}

// CreateRequest 是创建 profile 的入参。
type CreateRequest struct {
	Name string            `json:"name"`
	Kind model.ProfileKind `json:"kind"`
	// Proxy 为 nil 表示直连。
	Proxy *model.Proxy `json:"proxy,omitempty"`
	// GeoOverride 非空时跳过出口 IP 反查。留空即自动对齐，这是推荐做法。
	GeoOverride   *model.Geo `json:"geoOverride,omitempty"`
	KernelVersion string     `json:"kernelVersion,omitempty"`
	Notes         string     `json:"notes,omitempty"`

	// DisableSpoofing 列出要关闭的伪造子系统，仅用于排障，正常留空。
	DisableSpoofing []model.SpoofTarget `json:"disableSpoofing,omitempty"`

	// Group 是所属分组，留空表示未分组。
	Group string `json:"group,omitempty"`
	// Tags 是标签集合，入库时会去重与清洗。
	Tags []string `json:"tags,omitempty"`
	// DeviceLabel 锁定机型档案，留空表示由种子随机抽取。
	DeviceLabel string `json:"deviceLabel,omitempty"`
	// Startup 是启动页与新标签页配置，nil 表示用内核默认（空白新标签页）。
	Startup *model.Startup `json:"startup,omitempty"`

	// UseSystemBrowser 为 true 时用系统已装的官方 Chrome，仅日常模式可用。
	// 见 model.Profile.UseSystemBrowser 的说明。
	UseSystemBrowser bool `json:"useSystemBrowser,omitempty"`

	// MatchHostGPU 为 true 时筛选种子，使派生出的 GPU 与宿主机同厂商族。
	//
	// 为什么需要它：实测 Cloudflare 的判据是伪造 GPU 与宿主机跨厂商
	// （见 README 的商业风控实测）。GPU 由内核从种子派生、无参数可控，
	// 所以只能靠反复生成种子并实测来间接控制。
	//
	// 代价是新建 profile 变慢：每个候选种子要冷启动一次内核，实测单次约 2 秒，
	// 最多试 DefaultSeedAttempts 次。因此默认关闭，由用户按需开启。
	//
	// 只影响新建。已有 profile 的种子不会被改——改种子等于身份漂移。
	MatchHostGPU bool `json:"matchHostGPU,omitempty"`
}

// CreateProfile 新建一个 profile。指纹模式会生成一个全新的随机种子。
//
// ctx 用于取消 MatchHostGPU 的种子筛选——那一步要反复冷启动内核，
// 最长可达数十秒，不给取消途径会让界面卡住无法中断。
func (s *Service) CreateProfile(ctx context.Context, req CreateRequest) (ProfileView, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return ProfileView{}, errors.New("profile 名称不能为空")
	}
	if !req.Kind.Valid() {
		return ProfileView{}, fmt.Errorf("profile 类型 %q 无效", req.Kind)
	}
	// 提前校验机型档案：存了不存在的标签会在启动时静默回退成按种子抽取，
	// 用户以为锁定了机型其实没有。
	if req.DeviceLabel != "" {
		if _, ok := fingerprint.FindDevice(req.DeviceLabel); !ok {
			return ProfileView{}, fmt.Errorf("机型档案 %q 不存在", req.DeviceLabel)
		}
	}

	id := uuid.NewString()
	p := &model.Profile{
		ID:               id,
		Name:             name,
		Kind:             req.Kind,
		ProfileDir:       s.paths.ProfileDir(id),
		Proxy:            req.Proxy,
		GeoOverride:      req.GeoOverride,
		KernelVersion:    req.KernelVersion,
		Notes:            req.Notes,
		DisableSpoofing:  req.DisableSpoofing,
		Group:            req.Group,
		Tags:             req.Tags,
		DeviceLabel:      req.DeviceLabel,
		Startup:          req.Startup,
		UseSystemBrowser: req.UseSystemBrowser,
	}
	// fail-closed 在落库之前：指纹模式配了系统浏览器就拒绝创建，
	// 不留下一条启动时才报错的记录。
	if err := p.ValidateBrowserChoice(); err != nil {
		return ProfileView{}, err
	}
	if req.Kind == model.KindFingerprint {
		seed, err := fingerprint.NewSeed()
		if err != nil {
			return ProfileView{}, err
		}
		p.Seed = seed

		if req.MatchHostGPU {
			// 筛选失败一律报错，绝不静默退回随机种子。用户开这个选项就是
			// 为了过 Cloudflare，静默给一个跨厂商的种子会让他以为已经能过，
			// 直到账号出问题才发现——比直接失败糟得多。
			matched, note, err := s.matchedSeed(ctx, req.KernelVersion)
			if err != nil {
				return ProfileView{}, fmt.Errorf("按宿主机 GPU 筛选种子失败: %w", err)
			}
			p.Seed = matched
			p.Notes = appendNote(p.Notes, note)
		}
	} else if req.MatchHostGPU {
		// 日常模式不做任何伪造，没有种子可筛。静默忽略会让用户以为生效了。
		return ProfileView{}, errors.New("日常模式不伪造指纹，无法按宿主机 GPU 筛选种子")
	}

	if err := s.store.Save(p); err != nil {
		return ProfileView{}, err
	}
	return s.toView(p), nil
}

// appendNote 把一行说明追加到备注里，保留用户原有内容。
func appendNote(existing, line string) string {
	if line == "" {
		return existing
	}
	if strings.TrimSpace(existing) == "" {
		return line
	}
	return existing + "\n" + line
}

// UpdateRequest 是更新 profile 的入参。
//
// 不含 Seed：种子是 profile 身份的根，改动会让该 profile 的指纹整体漂移，
// 等同于换了一台设备。需要新指纹应当新建 profile。
type UpdateRequest struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Proxy       *model.Proxy `json:"proxy,omitempty"`
	GeoOverride *model.Geo   `json:"geoOverride,omitempty"`
	// ClearProxy 为 true 时清除代理配置。用单独的标记而非依赖 Proxy 为 nil，
	// 否则无法区分"不改动"与"要清除"。
	ClearProxy    bool     `json:"clearProxy"`
	KernelVersion string   `json:"kernelVersion,omitempty"`
	ExtraArgs     []string `json:"extraArgs,omitempty"`
	Notes         string   `json:"notes,omitempty"`

	// DisableSpoofing 列出要关闭的伪造子系统，仅用于排障。
	DisableSpoofing []model.SpoofTarget `json:"disableSpoofing,omitempty"`

	// ConfirmKernelChange 为 true 时允许给已用过的 profile 换内核大版本。
	// 换大版本会改变该 profile 的指纹，等同于账号换了设备，因此默认拒绝。
	ConfirmKernelChange bool `json:"confirmKernelChange"`

	// Group 是所属分组，空串表示移出分组。
	Group string `json:"group,omitempty"`
	// Tags 整体替换原有标签。要增删单个标签用 TagBatch，语义更明确。
	Tags []string `json:"tags,omitempty"`

	// DeviceLabel 锁定机型档案，空串表示恢复为由种子抽取。
	//
	// 换机型会改变该 profile 的设备特征，对已登录的账号等同于换设备。
	// 与换内核大版本一样需要显式确认，见 ConfirmDeviceChange。
	DeviceLabel string `json:"deviceLabel,omitempty"`
	// ConfirmDeviceChange 为 true 时允许给已用过的 profile 换机型。
	ConfirmDeviceChange bool `json:"confirmDeviceChange"`

	// Startup 是启动页与新标签页配置，nil 表示恢复为内核默认。
	//
	// 不需要确认：它只影响打开哪些页面，不改变任何指纹维度。
	Startup *model.Startup `json:"startup,omitempty"`

	// UseSystemBrowser 为 true 时用系统已装的官方 Chrome，仅日常模式可用。
	//
	// 切换它会换掉浏览器可执行文件，因此该 profile 的 user-data-dir 里
	// 已有的登录态、扩展与设置未必被新浏览器完整识别——两者的 Preferences
	// 格式虽同源但版本与功能集不同。不阻断，因为日常 profile 本无指纹身份
	// 可言，重新登录的成本远低于加一道确认。
	UseSystemBrowser bool `json:"useSystemBrowser,omitempty"`
}

// UpdateProfile 更新一个 profile 的可变字段。
func (s *Service) UpdateProfile(req UpdateRequest) (ProfileView, error) {
	p, err := s.store.Get(req.ID)
	if err != nil {
		return ProfileView{}, err
	}
	if name := strings.TrimSpace(req.Name); name != "" {
		p.Name = name
	}
	switch {
	case req.ClearProxy:
		p.Proxy = nil
	case req.Proxy != nil:
		// 前端不持有密码原文，提交空密码时保留原有凭据，
		// 否则每次编辑都会把密码清掉。
		if req.Proxy.Password == "" && p.Proxy != nil &&
			req.Proxy.Host == p.Proxy.Host && req.Proxy.Username == p.Proxy.Username {
			req.Proxy.Password = p.Proxy.Password
		}
		p.Proxy = req.Proxy
	}
	// 换内核大版本会让已建立的身份漂移，只对用过的 profile 设卡：
	// 从未启动过的 profile 还没在任何平台留下痕迹，随便换都无害。
	if p.Kind == model.KindFingerprint && !p.LastUseAt.IsZero() &&
		crossesMajor(p.KernelVersion, req.KernelVersion) && !req.ConfirmKernelChange {
		return ProfileView{}, &ErrKernelDrift{From: p.KernelVersion, To: req.KernelVersion}
	}

	// 换机型同样让已建立的身份漂移，与换内核大版本同等对待。
	if p.Kind == model.KindFingerprint && !p.LastUseAt.IsZero() &&
		req.DeviceLabel != p.DeviceLabel && !req.ConfirmDeviceChange {
		return ProfileView{}, &ErrDeviceDrift{From: p.DeviceLabel, To: req.DeviceLabel}
	}
	// 锁定的机型必须真实存在，否则会静默回退成按种子抽取。
	if req.DeviceLabel != "" {
		if _, ok := fingerprint.FindDevice(req.DeviceLabel); !ok {
			return ProfileView{}, fmt.Errorf("机型档案 %q 不存在", req.DeviceLabel)
		}
	}

	p.GeoOverride = req.GeoOverride
	p.KernelVersion = req.KernelVersion
	p.ExtraArgs = req.ExtraArgs
	p.Notes = req.Notes
	p.DisableSpoofing = req.DisableSpoofing
	p.Group = req.Group
	p.Tags = req.Tags
	p.DeviceLabel = req.DeviceLabel
	p.Startup = req.Startup
	p.UseSystemBrowser = req.UseSystemBrowser

	// 与创建同样在落库前 fail-closed。编辑路径同样能造出非法组合：
	// 把日常 profile 改成指纹模式时，原有的 UseSystemBrowser 会留下来。
	if err := p.ValidateBrowserChoice(); err != nil {
		return ProfileView{}, err
	}

	if err := s.store.Save(p); err != nil {
		return ProfileView{}, err
	}
	return s.toView(p), nil
}

// ListProfiles 返回全部 profile，最近使用的在前。
func (s *Service) ListProfiles() ([]ProfileView, error) {
	list, err := s.store.List()
	if err != nil {
		return nil, err
	}
	out := make([]ProfileView, 0, len(list))
	for _, p := range list {
		out = append(out, s.toView(p))
	}
	return out, nil
}

// GetProfile 按 ID 返回单个 profile。
func (s *Service) GetProfile(id string) (ProfileView, error) {
	p, err := s.store.Get(id)
	if err != nil {
		return ProfileView{}, err
	}
	return s.toView(p), nil
}

// DeleteProfile 删除 profile 配置记录。
//
// 不删除磁盘上的 user-data-dir：其中含 Cookie、登录态与浏览历史，
// 误删不可恢复。目录清理由 DeleteProfileData 在用户明确确认后单独执行。
func (s *Service) DeleteProfile(id string) error {
	if st := s.sessions.Status(id); st.State == session.StateRunning {
		return errors.New("该 profile 正在运行，请先停止再删除")
	}
	return s.store.Delete(id)
}

// DeleteProfileData 删除 profile 的浏览数据目录。
//
// 不可恢复：会一并清除该 profile 的 Cookie、登录态与浏览历史。
// 调用方必须先向用户明确确认。
func (s *Service) DeleteProfileData(id string) error {
	p, err := s.store.Get(id)
	if err != nil {
		return err
	}
	if st := s.sessions.Status(id); st.State == session.StateRunning {
		return errors.New("该 profile 正在运行，请先停止再清除数据")
	}
	// 只允许删自己管理的目录，避免配置被篡改后误删无关路径。
	if !s.paths.owns(p.ProfileDir) {
		return fmt.Errorf("拒绝删除受管目录之外的路径: %s", p.ProfileDir)
	}
	if err := os.RemoveAll(p.ProfileDir); err != nil {
		return fmt.Errorf("清除 profile 数据失败: %w", err)
	}
	return nil
}

// Start 启动一个 profile。
func (s *Service) Start(ctx context.Context, id string) (session.Status, error) {
	p, err := s.store.Get(id)
	if err != nil {
		return session.Status{}, err
	}
	st, err := s.sessions.Start(ctx, p)
	if err != nil {
		return st, err
	}
	// 使用时间只用于列表排序，写失败不影响会话，记录后继续。
	if err := s.store.TouchLastUse(id, time.Now()); err != nil {
		return st, nil
	}
	return st, nil
}

// Stop 停止一个 profile。
func (s *Service) Stop(id string) error { return s.sessions.Stop(id) }

// WaitSession 阻塞直到该 profile 的浏览器进程真正退出。未在运行时立即返回。
//
// Stop 只投递关闭消息就返回，浏览器落盘退出还需要一点时间。需要确保进程
// 已消失时用这个——例如清除该 profile 的浏览数据前，或测试收尾时避免
// 留下残留进程。
func (s *Service) WaitSession(id string) { s.sessions.Wait(id) }

// RunningSessions 返回全部运行中会话的状态。
func (s *Service) RunningSessions() []session.Status { return s.sessions.Running() }

// SessionStatus 返回指定 profile 的会话状态。
func (s *Service) SessionStatus(id string) session.Status { return s.sessions.Status(id) }

// ListKernels 返回已安装的内核列表。
func (s *Service) ListKernels() ([]kernel.Kernel, error) { return s.kernels.List() }

// ListAvailableKernels 查询可下载的内核版本。
func (s *Service) ListAvailableKernels(ctx context.Context) ([]kernel.Release, error) {
	return (&kernel.Fetcher{}).ListReleases(ctx)
}

// InstallProgress 是内核安装进度，通过 Wails 事件推送给界面。
type InstallProgress struct {
	Version    string `json:"version"`
	Downloaded int64  `json:"downloaded"`
	Total      int64  `json:"total"`
	// Done 为 true 表示安装结束；Err 非空表示失败。
	Done bool   `json:"done"`
	Err  string `json:"err,omitempty"`
}

// InstallKernel 下载并安装指定版本的内核。
//
// onProgress 可为 nil。下载约 200MB，调用方应在后台执行并向界面推送进度。
func (s *Service) InstallKernel(ctx context.Context, rel kernel.Release,
	onProgress func(InstallProgress)) (kernel.Kernel, error) {

	var cb kernel.Progress
	if onProgress != nil {
		cb = func(downloaded, total int64) {
			onProgress(InstallProgress{
				Version: rel.Version, Downloaded: downloaded, Total: total,
			})
		}
	}
	k, err := kernel.NewInstaller(s.kernels).Install(ctx, rel, cb)
	if onProgress != nil {
		p := InstallProgress{Version: rel.Version, Done: true, Total: rel.Size}
		if err != nil {
			p.Err = err.Error()
		} else {
			p.Downloaded = rel.Size
		}
		onProgress(p)
	}
	return k, err
}

// KernelsDir 返回内核安装目录，供界面提示用户放置内核。
func (s *Service) KernelsDir() string { return s.paths.Kernels }

// ListDeviceProfiles 返回预置机型档案库，供界面展示与手动锁定机型。
//
// 含 KnownIssue 的档案也会返回，界面应据此显著提示风险：这些档案不参与
// 随机抽取，只在用户知晓风险后显式选择时使用。
func (s *Service) ListDeviceProfiles() []model.DeviceProfile { return fingerprint.Catalog() }

// ListSpoofTargets 返回可单独关闭的伪造子系统，供界面提供排障选项。
func (s *Service) ListSpoofTargets() []model.SpoofTarget { return model.SpoofTargets() }

// CreateShortcut 在桌面创建直接启动该 profile 的快捷方式，返回其路径。
//
// 快捷方式的目标是本程序加 --profile=<名称> 参数，双击即绕过管理面板启动。
// 用名称而非 ID 作参数：ID 是 UUID，写进快捷方式属性里无法阅读也无法核对。
func (s *Service) CreateShortcut(id string) (string, error) {
	p, err := s.store.Get(id)
	if err != nil {
		return "", err
	}
	return shortcut.Create(shortcut.Options{ProfileName: p.Name})
}

// CredentialProtection 说明代理密码在本机的保护级别。
type CredentialProtection struct {
	// Encrypted 为 false 时密码以明文入库，界面应当明确提示。
	Encrypted bool `json:"encrypted"`
	// Detail 是可读说明，直接展示给用户。
	Detail string `json:"detail"`
}

// CredentialProtection 返回当前平台的凭据保护级别。
//
// 暴露给界面是必要的：非 Windows 平台会退化为明文存储，
// 若不告知用户，他们会以为密码已受保护而放松对数据目录的警惕。
func (s *Service) CredentialProtection() CredentialProtection {
	return CredentialProtection{
		Encrypted: secret.Available(),
		Detail:    secret.Description(),
	}
}

// ProxyCheck 是代理连通性与出口质量的检测结果。
type ProxyCheck struct {
	OK bool `json:"ok"`
	// Err 是失败原因，OK 为 false 时有值。
	Err string `json:"err,omitempty"`
	// Exit 是出口画像，仅在连通时有值。
	Exit *geo.ExitInfo `json:"exit,omitempty"`
	// Aligned 是按出口地推导出的时区与语言，便于界面展示将要生效的值。
	Aligned *model.Geo `json:"aligned,omitempty"`
	// Warnings 是不阻断使用但需用户知晓的问题，如出口为机房 IP。
	Warnings []string `json:"warnings,omitempty"`
	// ElapsedMs 是完整往返耗时，用于粗略判断代理速度。
	ElapsedMs int64 `json:"elapsedMs"`
}

// CheckProxyTimeout 是单次代理检测的总超时。
//
// 比启动流程的超时短得多：这是用户点按钮后要盯着等的操作，
// 让人等 30 秒才知道代理不通是糟糕的体验。宁可提前判失败让用户重试。
const CheckProxyTimeout = 12 * time.Second

// checkProxyTimeout 返回本次检测的超时。
// CheckProxyTimeoutOverride 非零时优先，供测试避免每条用例都耗满超时。
func (s *Service) checkProxyTimeout() time.Duration {
	if s.CheckProxyTimeoutOverride > 0 {
		return s.CheckProxyTimeoutOverride
	}
	return CheckProxyTimeout
}

// CheckProxy 检测一个代理配置是否可用，并判定出口质量。
//
// 不启动浏览器，只起转发器发一次探测请求，因此可在保存配置前先行验证。
// 出口为机房 IP 时给出警告而不失败：判定基于组织名关键词，会有漏判，
// 由用户结合自己的场景决定是否可接受。
func (s *Service) CheckProxy(ctx context.Context, p *model.Proxy) ProxyCheck {
	started := time.Now()
	ctx, cancel := context.WithTimeout(ctx, s.checkProxyTimeout())
	defer cancel()
	fail := func(err error) ProxyCheck {
		return ProxyCheck{OK: false, Err: err.Error(), ElapsedMs: time.Since(started).Milliseconds()}
	}

	if p == nil {
		return fail(errors.New("未配置代理"))
	}
	f, err := proxy.New(p)
	if err != nil {
		return fail(err)
	}
	// 只用 HTTPClient 走上游探测，不监听本地端口，无需 Start/Close 配对。
	client, err := f.HTTPClient()
	if err != nil {
		return fail(err)
	}
	info, err := geo.NewResolver(client).LookupExit(ctx)
	if err != nil {
		return fail(fmt.Errorf("经代理查询出口失败: %w", err))
	}

	aligned := geo.Resolve(info.Geo.CountryCode, info.Geo.Region)
	out := ProxyCheck{
		OK: true, Exit: &info, Aligned: &aligned,
		ElapsedMs: time.Since(started).Milliseconds(),
	}
	switch info.Kind {
	case geo.IPKindHosting:
		out.Warnings = append(out.Warnings, fmt.Sprintf(
			"出口是机房 IP（%s），多账号场景下极易被识别，建议改用住宅代理", info.Org))
	case geo.IPKindUnknown:
		out.Warnings = append(out.Warnings,
			"无法判定出口网络类型，请自行确认是否为住宅 IP")
	}
	return out
}
