package main

import (
	"context"
	"fmt"

	"better-web/internal/app"
	"better-web/internal/kernel"
	"better-web/internal/model"
	"better-web/internal/session"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App 是绑定给前端的入口对象。它把调用转发给 app.Service，
// 只负责补上 Wails 的 context 并把错误原样上抛给界面。
type App struct {
	ctx     context.Context
	service *app.Service
	// initErr 记录启动期错误，供各方法在服务不可用时返回明确原因。
	initErr error
}

// NewApp 构造应用对象。
func NewApp() *App { return &App{} }

// startup 在应用启动时初始化服务。
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	paths, err := app.DefaultPaths()
	if err != nil {
		// 数据目录不可用时后续所有操作都会失败，让界面在首次调用时看到该错误。
		a.initErr = err
		return
	}
	svc, err := app.New(paths)
	if err != nil {
		a.initErr = err
		return
	}
	a.service = svc
}

// shutdown 在应用退出时停止全部会话并释放资源。
func (a *App) shutdown(_ context.Context) {
	if a.service != nil {
		_ = a.service.Close()
	}
}

// svc 返回服务实例，未初始化成功时返回错误而非 panic。
func (a *App) svc() (*app.Service, error) {
	if a.service == nil {
		if a.initErr != nil {
			return nil, fmt.Errorf("应用初始化失败: %w", a.initErr)
		}
		return nil, fmt.Errorf("应用尚未初始化完成")
	}
	return a.service, nil
}

// ListProfiles 返回全部 profile。
func (a *App) ListProfiles() ([]app.ProfileView, error) {
	s, err := a.svc()
	if err != nil {
		return nil, err
	}
	return s.ListProfiles()
}

// GetProfile 返回单个 profile。
func (a *App) GetProfile(id string) (app.ProfileView, error) {
	s, err := a.svc()
	if err != nil {
		return app.ProfileView{}, err
	}
	return s.GetProfile(id)
}

// CreateProfile 新建 profile。
//
// 开启 MatchHostGPU 时这个调用会跑几十秒（反复冷启动内核筛种子），
// 用 Wails 的 context 使其能随应用退出中断。
func (a *App) CreateProfile(req app.CreateRequest) (app.ProfileView, error) {
	s, err := a.svc()
	if err != nil {
		return app.ProfileView{}, err
	}
	return s.CreateProfile(a.ctx, req)
}

// DetectHostGPU 探测宿主机真实 GPU 厂商族，供界面提示是否需要筛选种子。
func (a *App) DetectHostGPU(kernelVersion string) (app.HostGPUInfo, error) {
	s, err := a.svc()
	if err != nil {
		return app.HostGPUInfo{}, err
	}
	return s.DetectHostGPU(a.ctx, kernelVersion)
}

// UpdateProfile 更新 profile。
func (a *App) UpdateProfile(req app.UpdateRequest) (app.ProfileView, error) {
	s, err := a.svc()
	if err != nil {
		return app.ProfileView{}, err
	}
	return s.UpdateProfile(req)
}

// DeleteProfile 删除 profile 配置，保留其浏览数据目录。
func (a *App) DeleteProfile(id string) error {
	s, err := a.svc()
	if err != nil {
		return err
	}
	return s.DeleteProfile(id)
}

// DeleteProfileData 清除 profile 的浏览数据。不可恢复，界面须先确认。
func (a *App) DeleteProfileData(id string) error {
	s, err := a.svc()
	if err != nil {
		return err
	}
	return s.DeleteProfileData(id)
}

// StartProfile 启动 profile。
func (a *App) StartProfile(id string) (session.Status, error) {
	s, err := a.svc()
	if err != nil {
		return session.Status{}, err
	}
	return s.Start(a.ctx, id)
}

// StopProfile 停止 profile。
func (a *App) StopProfile(id string) error {
	s, err := a.svc()
	if err != nil {
		return err
	}
	return s.Stop(id)
}

// RunningSessions 返回运行中的会话。
func (a *App) RunningSessions() ([]session.Status, error) {
	s, err := a.svc()
	if err != nil {
		return nil, err
	}
	return s.RunningSessions(), nil
}

// ListKernels 返回已安装的内核。
func (a *App) ListKernels() ([]kernel.Kernel, error) {
	s, err := a.svc()
	if err != nil {
		return nil, err
	}
	return s.ListKernels()
}

// ListAvailableKernels 查询可下载的内核版本。
func (a *App) ListAvailableKernels() ([]kernel.Release, error) {
	s, err := a.svc()
	if err != nil {
		return nil, err
	}
	return s.ListAvailableKernels(a.ctx)
}

// kernelInstallEvent 是内核安装进度事件名，前端据此订阅。
const kernelInstallEvent = "kernel:install-progress"

// InstallKernel 下载并安装内核，进度通过 kernelInstallEvent 事件推送。
//
// 同步返回安装结果：下载约 200MB，界面应在调用期间展示进度并禁用重复触发。
func (a *App) InstallKernel(rel kernel.Release) (kernel.Kernel, error) {
	s, err := a.svc()
	if err != nil {
		return kernel.Kernel{}, err
	}
	return s.InstallKernel(a.ctx, rel, func(p app.InstallProgress) {
		runtime.EventsEmit(a.ctx, kernelInstallEvent, p)
	})
}

// KernelsDir 返回内核安装目录，供界面提示用户放置内核。
func (a *App) KernelsDir() (string, error) {
	s, err := a.svc()
	if err != nil {
		return "", err
	}
	return s.KernelsDir(), nil
}

// ListDeviceProfiles 返回预置机型档案库。
func (a *App) ListDeviceProfiles() ([]model.DeviceProfile, error) {
	s, err := a.svc()
	if err != nil {
		return nil, err
	}
	return s.ListDeviceProfiles(), nil
}

// ListSpoofTargets 返回可单独关闭的伪造子系统，供界面提供排障选项。
func (a *App) ListSpoofTargets() ([]model.SpoofTarget, error) {
	s, err := a.svc()
	if err != nil {
		return nil, err
	}
	return s.ListSpoofTargets(), nil
}

// CredentialProtection 返回代理密码在本机的保护级别，供界面提示用户。
func (a *App) CredentialProtection() (app.CredentialProtection, error) {
	s, err := a.svc()
	if err != nil {
		return app.CredentialProtection{}, err
	}
	return s.CredentialProtection(), nil
}

// PickExportPath 弹出保存对话框，返回用户选择的路径。取消时返回空串。
//
// 用原生对话框而非让前端拼路径：前端拿不到用户的文档目录，也无法校验
// 目标位置是否可写。
func (a *App) PickExportPath() (string, error) {
	return runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "导出 Profile 配置",
		DefaultFilename: "better-web-profiles.json",
		Filters: []runtime.FileFilter{
			{DisplayName: "JSON 配置 (*.json)", Pattern: "*.json"},
		},
	})
}

// PickImportPath 弹出打开对话框，返回用户选择的路径。取消时返回空串。
func (a *App) PickImportPath() (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "导入 Profile 配置",
		Filters: []runtime.FileFilter{
			{DisplayName: "JSON 配置 (*.json)", Pattern: "*.json"},
		},
	})
}

// ExportBundle 把全部 profile 配置导出到指定路径，返回导出条数。
//
// withSecrets 为 true 时含代理密码明文，界面必须显著提示后果——
// 导出文件常被复制与转发，凭据进入其中的风险远高于重填一次密码。
func (a *App) ExportBundle(path string, withSecrets bool) (int, error) {
	s, err := a.svc()
	if err != nil {
		return 0, err
	}
	return s.ExportBundle(path, withSecrets)
}

// ImportBundle 从配置文件导入 profile。
func (a *App) ImportBundle(path string, opt app.BundleImportOptions) (app.BundleImportResult, error) {
	s, err := a.svc()
	if err != nil {
		return app.BundleImportResult{}, err
	}
	return s.ImportBundle(path, opt)
}

// CreateShortcut 在桌面创建直接启动指定 profile 的快捷方式，返回其路径。
func (a *App) CreateShortcut(id string) (string, error) {
	s, err := a.svc()
	if err != nil {
		return "", err
	}
	return s.CreateShortcut(id)
}

// URLHandler 返回链接接管的配置与系统注册状态。
func (a *App) URLHandler() (app.URLHandlerView, error) {
	s, err := a.svc()
	if err != nil {
		return app.URLHandlerView{}, err
	}
	return s.URLHandler()
}

// SetURLHandler 指定接管系统链接的 profile 与无痕开关。
// profileID 传空串表示关闭接管。
func (a *App) SetURLHandler(profileID string, incognito bool) error {
	s, err := a.svc()
	if err != nil {
		return err
	}
	return s.SetURLHandler(profileID, incognito)
}

// RegisterURLHandler 把 better-web 注册成系统的候选默认浏览器。
//
// 注册成功不等于已生效：系统不允许应用自己抢默认浏览器，用户还需在
// "设置 → 默认应用"里手动选。界面必须把这一步说清楚。
func (a *App) RegisterURLHandler() error {
	s, err := a.svc()
	if err != nil {
		return err
	}
	return s.RegisterURLHandler()
}

// UnregisterURLHandler 清除系统里的注册信息。
func (a *App) UnregisterURLHandler() error {
	s, err := a.svc()
	if err != nil {
		return err
	}
	return s.UnregisterURLHandler()
}

// OpenDefaultAppsSettings 打开系统的默认应用设置页，供用户完成最后一步。
func (a *App) OpenDefaultAppsSettings() error {
	s, err := a.svc()
	if err != nil {
		return err
	}
	return s.OpenDefaultAppsSettings()
}

// ParseProxyLine 解析一行代理配置，供界面把粘贴的一行填进表单各字段。
//
// 不放在前端用 TS 重写一份：两份实现迟早会漂移，而这里漂移的后果是
// 密码含 @ 或 : 时前后端切法不一致——表单显示的凭据与实际存的不是一回事。
//
// 不需要 Service：解析是纯函数，与数据目录、会话状态都无关，
// 因此即便服务初始化失败也能用。
func (a *App) ParseProxyLine(line string) (*model.Proxy, error) {
	return model.ParseProxy(line)
}

// CheckProxy 检测代理是否可用并判定出口质量，供保存配置前先行验证。
func (a *App) CheckProxy(p *model.Proxy) (app.ProxyCheck, error) {
	s, err := a.svc()
	if err != nil {
		return app.ProxyCheck{}, err
	}
	return s.CheckProxy(a.ctx, p), nil
}
