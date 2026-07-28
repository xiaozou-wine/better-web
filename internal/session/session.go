// Package session 编排一个 profile 的完整启动流程。
//
// 启动顺序不可调换，每一步都是下一步的前提：
//  1. 起本地代理转发器（上游若需认证，内核无法直接使用）
//  2. 经代理查出口地理位置（必须走代理，直连查到的是本机）
//  3. 由种子与地理信息推导指纹（时区语言必须与出口地一致）
//  4. 组装命令行并启动内核
//
// 第 2 步失败时默认中止启动而非静默回退：时区与真实出口不符，
// 比不启动更糟。
package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"better-web/internal/fingerprint"
	"better-web/internal/geo"
	"better-web/internal/kernel"
	"better-web/internal/launcher"
	"better-web/internal/model"
	"better-web/internal/proxy"
)

// State 是一个会话的运行状态。
type State string

const (
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateStopped  State = "stopped"
	StateFailed   State = "failed"
)

// Status 是会话状态的快照，用于呈现给界面。
type Status struct {
	ProfileID string    `json:"profileId"`
	State     State     `json:"state"`
	PID       int       `json:"pid,omitempty"`
	StartedAt time.Time `json:"startedAt,omitzero"`
	// Geo 是本次会话实际生效的出口地理信息。
	Geo *model.Geo `json:"geo,omitempty"`
	// Exit 是出口 IP 画像，含 ASN 判定。无代理时为 nil。
	Exit *geo.ExitInfo `json:"exit,omitempty"`
	// Warnings 是不阻断启动但需用户知晓的问题，如出口为机房 IP。
	Warnings []string `json:"warnings,omitempty"`
	// Fingerprint 是本次会话实际生效的指纹，便于界面核对一致性。
	Fingerprint *model.Fingerprint `json:"fingerprint,omitempty"`
	// Err 是失败原因的可读描述，仅在 StateFailed 时有值。
	Err string `json:"err,omitempty"`
}

// session 是单个运行中的浏览器实例及其附属资源。
type session struct {
	profileID string
	cmd       *exec.Cmd
	forwarder *proxy.Forwarder
	status    Status
	// done 在进程退出后关闭，供 Wait 使用。
	done chan struct{}
}

// Manager 管理所有运行中的会话。方法可并发调用。
type Manager struct {
	kernels *kernel.Store

	// StrictGeo 为 true 时，出口地查询失败即中止启动；为 false 时回退到
	// 默认地理信息并在状态中记录。默认 true。
	StrictGeo bool

	mu   sync.Mutex
	live map[string]*session
}

// NewManager 构造会话管理器。
func NewManager(kernels *kernel.Store) *Manager {
	return &Manager{kernels: kernels, StrictGeo: true, live: map[string]*session{}}
}

// ErrAlreadyRunning 表示该 profile 已有实例在运行。
// 同一 user-data-dir 被两个进程同时打开会导致 profile 数据损坏。
var ErrAlreadyRunning = errors.New("该 profile 已在运行")

// Start 启动一个 profile 并返回其状态。
func (m *Manager) Start(ctx context.Context, p *model.Profile) (Status, error) {
	return m.StartWith(ctx, p, nil)
}

// StartWith 启动一个 profile，并接受本次启动的一次性附加项。
//
// opt 为 nil 时等价于 Start。附加项用于链接接管：外部传进来的 URL 每次都不同，
// 存进 profile 配置没有意义，见 launcher.Options。
func (m *Manager) StartWith(ctx context.Context, p *model.Profile, opt *launcher.Options) (Status, error) {
	if p == nil {
		return Status{}, errors.New("profile 为空")
	}

	m.mu.Lock()
	if _, ok := m.live[p.ID]; ok {
		m.mu.Unlock()
		return Status{}, fmt.Errorf("%w: %s", ErrAlreadyRunning, p.Name)
	}
	// 先占位，避免并发 Start 同一 profile 时双双通过检查。
	m.live[p.ID] = &session{
		profileID: p.ID,
		status:    Status{ProfileID: p.ID, State: StateStarting},
		done:      make(chan struct{}),
	}
	m.mu.Unlock()

	st, err := m.start(ctx, p, opt)
	if err != nil {
		m.mu.Lock()
		delete(m.live, p.ID)
		m.mu.Unlock()
		return Status{ProfileID: p.ID, State: StateFailed, Err: err.Error()}, err
	}
	return st, nil
}

func (m *Manager) start(ctx context.Context, p *model.Profile, opt *launcher.Options) (Status, error) {
	// fail-closed 前置：配置组合不合法时在解析内核之前就拒绝，
	// 不给"先解析出系统 Chrome 再发现不该用"留任何窗口。
	if err := p.ValidateBrowserChoice(); err != nil {
		return Status{}, err
	}

	k, err := m.resolveBrowser(p)
	if err != nil {
		return Status{}, err
	}
	// 第二道闸：即便配置校验通过，也要确认解析出的内核确实支持伪造。
	// 两道判断的依据不同——上一道看配置意图，这一道看实际解析结果。
	// 内核目录里若混入了非 fingerprint-chromium 的构建，只有这道能拦住。
	if p.Kind == model.KindFingerprint && !k.Source.SupportsFingerprint() {
		return Status{}, fmt.Errorf(
			"内核 %s 不支持指纹伪造，拒绝以指纹模式启动: %w",
			k.ExecPath, model.ErrSystemBrowserWithFingerprint)
	}

	if err := os.MkdirAll(p.ProfileDir, 0o700); err != nil {
		return Status{}, fmt.Errorf("创建 profile 目录失败: %w", err)
	}

	// 跨进程锁。m.live 只防住同进程内的重复启动，而界面与命令行工具同时运行时
	// 两边各有一张会话表，都会放行——两个 Chromium 打开同一个 user-data-dir
	// 会损坏 profile 数据，且 Chromium 的处理是让后来者静默退出，用户看不到原因。
	releaseLock, err := acquireLock(p.ProfileDir, p.ID)
	if err != nil {
		return Status{}, err
	}

	// 第 1 步：代理转发器。
	var forwarder *proxy.Forwarder
	var proxyAddr string
	if p.Proxy != nil {
		forwarder, err = proxy.New(p.Proxy)
		if err != nil {
			releaseLock()
			return Status{}, err
		}
		proxyAddr, err = forwarder.Start()
		if err != nil {
			releaseLock()
			return Status{}, err
		}
	}
	// 启动失败时释放转发器与锁，避免端口、goroutine 与陈旧锁文件泄漏。
	cleanup := func() {
		if forwarder != nil {
			_ = forwarder.Close()
		}
		releaseLock()
	}

	// 第 2 步：出口地理位置与出口网络类型。
	res, err := m.resolveGeo(ctx, p, forwarder)
	if err != nil {
		cleanup()
		return Status{}, err
	}

	// 第 3 步：推导指纹。用内核实际版本推导品牌版本，
	// 避免出现内核报一个大版本、UA 声称另一个大版本的矛盾。
	var fp *model.Fingerprint
	var warnings []string
	if p.Kind == model.KindFingerprint {
		// 锁定的机型档案已从库中删除时，指纹会静默换成另一台设备——
		// 对已登录的账号等同于换机器，必须让用户看到。
		if p.DeviceLabel != "" {
			if _, ok := fingerprint.FindDevice(p.DeviceLabel); !ok {
				warnings = append(warnings, fmt.Sprintf(
					"锁定的机型「%s」已不在档案库中，本次改用按种子抽取的机型，"+
						"该 profile 的设备特征已发生变化", p.DeviceLabel))
			}
		}
		derived := fingerprint.DeriveWithDeviceLabel(
			p.Seed, res.geo, p.DeviceLabel, k.Version)
		fp = &derived
	}

	// 第 4 步：组装命令行并启动。
	args, err := launcher.BuildArgs(p, fp, proxyAddr, opt)
	if err != nil {
		cleanup()
		return Status{}, err
	}
	cmd := exec.CommandContext(ctx, k.ExecPath, args...)
	if err := cmd.Start(); err != nil {
		cleanup()
		return Status{}, fmt.Errorf("启动内核 %s 失败: %w", k.Version, err)
	}

	status := Status{
		ProfileID: p.ID,
		State:     StateRunning,
		PID:       cmd.Process.Pid,
		StartedAt: time.Now(),
		Geo:       res.geo,
		Exit:      res.exit,
		// 出口相关的警告与指纹相关的警告合并呈现，都属于"不阻断但需知晓"。
		Warnings:    append(res.warnings, warnings...),
		Fingerprint: fp,
	}

	m.mu.Lock()
	s := m.live[p.ID]
	if s == nil {
		// 占位被 Stop 清掉了，说明用户在启动过程中取消，直接收拾干净。
		m.mu.Unlock()
		_ = cmd.Process.Kill()
		cleanup()
		return Status{}, errors.New("启动过程中会话已被取消")
	}
	s.cmd, s.forwarder, s.status = cmd, forwarder, status
	done := s.done
	m.mu.Unlock()

	// 进程退出后回收转发器与跨进程锁：浏览器关了还占着本地端口是资源泄漏，
	// 留着锁文件会让下次启动误判为已被占用。
	go func() {
		_ = cmd.Wait()
		m.mu.Lock()
		if cur, ok := m.live[p.ID]; ok && cur.cmd == cmd {
			delete(m.live, p.ID)
		}
		m.mu.Unlock()
		if forwarder != nil {
			_ = forwarder.Close()
		}
		releaseLock()
		close(done)
	}()

	return status, nil
}

// resolveBrowser 决定本次启动用哪个浏览器可执行文件。
//
// 日常模式且开启 UseSystemBrowser 时用系统 Chrome，其余情况用指纹内核。
//
// 系统 Chrome 找不到时**报错而非回退到指纹内核**：用户明确选了系统 Chrome，
// 是为了 Google 账号、同步与自动更新。静默换成指纹内核后这些能力全都没有，
// 而界面显示的仍是"系统 Chrome"，他会以为登不上账号是别的原因。
func (m *Manager) resolveBrowser(p *model.Profile) (kernel.Kernel, error) {
	if p.UseSystemBrowser && p.Kind == model.KindDaily {
		k, err := kernel.SystemChrome()
		if err != nil {
			return kernel.Kernel{}, fmt.Errorf(
				"该 profile 配置为使用系统 Chrome，但未能定位到它: %w", err)
		}
		return k, nil
	}
	return m.kernels.Resolve(p.KernelVersion)
}

// resolved 是出口探测的结果。
type resolved struct {
	geo      *model.Geo
	exit     *geo.ExitInfo
	warnings []string
}

// resolveGeo 决定本次会话使用的地理信息，并判定出口网络类型。
//
// 出口为机房 IP 只产生警告，不中止启动：判定基于组织名关键词，必然有漏判，
// 误拦比漏报更让人恼火。而时区与出口地矛盾是确定性错误，因此仍按 StrictGeo
// 中止。两者的处理力度不同是有意为之。
func (m *Manager) resolveGeo(ctx context.Context, p *model.Profile, f *proxy.Forwarder) (resolved, error) {
	// 用户指定了地理信息时，时区语言以其为准，但出口探测照做。
	//
	// 这两件事是分开的：GeoOverride 表达的是"我知道出口在哪，不必反查"，
	// 不是"我不想知道出口是不是机房 IP"。跳过探测会连带丢掉 ASN 判定与
	// 出口 IP，用户就此失去风险提示，而这与他覆盖地理信息的意图无关。
	//
	// 探测失败在这条路径上不阻断启动：地理信息已经有了，启动的前提已满足，
	// 缺的只是附加的风险提示。
	if p.GeoOverride != nil {
		g := *p.GeoOverride
		out := resolved{geo: &g}
		if f != nil {
			if info, warns, err := m.probeExit(ctx, f); err == nil {
				out.exit = info
				out.warnings = warns
			}
		}
		return out, nil
	}
	// 无代理时使用本机真实地理，交由内核默认行为处理即可。
	if f == nil {
		if p.Kind == model.KindDaily {
			return resolved{}, nil
		}
		g := geo.Fallback()
		return resolved{geo: &g}, nil
	}

	info, warns, err := m.probeExit(ctx, f)
	if err != nil {
		// 日常模式不注入时区语言（见 launcher.BuildArgs），出口地探测的结果
		// 只进状态展示，不影响启动参数。StrictGeo 防的是"指纹声称的时区与
		// 真实出口矛盾"，而日常模式的时区本就来自本机——探测成功也改不了这点，
		// 失败自然不构成拒绝启动的理由。降级为警告，与 GeoOverride 路径同理。
		if p.Kind == model.KindDaily {
			return resolved{warnings: []string{
				fmt.Sprintf("未能确认代理出口位置（%v），本次启动不受影响，"+
					"但无法提示出口是否为机房 IP", err),
			}}, nil
		}
		if m.StrictGeo {
			return resolved{}, fmt.Errorf("查询代理出口地失败，中止启动以避免时区与出口地矛盾: %w", err)
		}
		g := geo.Fallback()
		return resolved{geo: &g}, nil
	}

	g := geo.Resolve(info.Geo.CountryCode, info.Geo.Region)
	return resolved{geo: &g, exit: info, warnings: warns}, nil
}

// probeExit 经代理查询出口画像，并据 ASN 判定生成风险警告。
//
// 抽出来是因为两条路径都需要它：自动对齐地理信息时用它的地理结果，
// 用户覆盖地理信息时只用它的 ASN 判定与出口 IP。
func (m *Manager) probeExit(ctx context.Context, f *proxy.Forwarder) (*geo.ExitInfo, []string, error) {
	client, err := f.HTTPClient()
	if err != nil {
		return nil, nil, err
	}
	info, err := geo.NewResolver(client).LookupExit(ctx)
	if err != nil {
		return nil, nil, err
	}

	var warns []string
	switch info.Kind {
	case geo.IPKindHosting:
		warns = append(warns, fmt.Sprintf(
			"出口是机房 IP（%s），多账号场景下极易被识别", info.Org))
	case geo.IPKindUnknown:
		warns = append(warns, "无法判定出口网络类型，请自行确认是否为住宅 IP")
	}
	return &info, warns, nil
}

// Stop 停止指定 profile 的会话。未在运行时返回 nil。
func (m *Manager) Stop(id string) error {
	m.mu.Lock()
	s, ok := m.live[id]
	m.mu.Unlock()
	if !ok {
		return nil
	}

	if s.cmd == nil || s.cmd.Process == nil {
		// 仍在启动中，移除占位让 start 收尾时自行清理。
		m.mu.Lock()
		delete(m.live, id)
		m.mu.Unlock()
		return nil
	}
	// 交由浏览器自行退出以便落盘 profile 数据。
	// 各平台的优雅关闭方式不同，见 terminate_windows.go / terminate_unix.go。
	if err := terminate(s.cmd.Process); err != nil {
		return fmt.Errorf("停止 profile %s 失败: %w", id, err)
	}
	return nil
}

// Wait 阻塞直到指定 profile 的进程退出。未在运行时立即返回。
func (m *Manager) Wait(id string) {
	m.mu.Lock()
	s, ok := m.live[id]
	m.mu.Unlock()
	if !ok {
		return
	}
	<-s.done
}

// Status 返回指定 profile 的状态快照。未在运行时返回 StateStopped。
func (m *Manager) Status(id string) Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.live[id]; ok {
		return s.status
	}
	return Status{ProfileID: id, State: StateStopped}
}

// Running 返回所有运行中会话的状态快照。
func (m *Manager) Running() []Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Status, 0, len(m.live))
	for _, s := range m.live {
		out = append(out, s.status)
	}
	return out
}

// StopAll 停止全部会话，用于应用退出时收尾。
func (m *Manager) StopAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.live))
	for id := range m.live {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		_ = m.Stop(id)
	}
}
