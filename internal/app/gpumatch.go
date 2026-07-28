package app

import (
	"context"
	"fmt"

	"better-web/internal/fingerprint"
	"better-web/internal/model"
	"better-web/internal/probe"
)

// kernelProber 把 probe 包适配成 fingerprint.GPUProber。
//
// fingerprint 不能直接依赖 probe（probe 要用 fingerprint.Derive 跑分，
// 会成环），所以桥接放在 app 层。
type kernelProber struct {
	execPath string
}

func (k kernelProber) SeedGPUFamily(ctx context.Context, seed int32) (model.GPUFamily, string, error) {
	return probe.SeedGPUFamily(ctx, k.execPath, seed)
}

// HostGPUInfo 是宿主机 GPU 的探测结果，供界面展示。
type HostGPUInfo struct {
	Family model.GPUFamily `json:"family"`
	// Renderer 是完整的 WebGL renderer 字符串，含具体型号。
	Renderer string `json:"renderer"`
}

// DetectHostGPU 探测宿主机真实 GPU 厂商族。
//
// 需要已安装内核：读的是"浏览器会报出什么"，而不是系统层查到的型号名——
// 前者才是检测方看到的值，两者未必一致。
//
// 结果按内核版本缓存：每次探测要冷启动一次浏览器（约 2 秒），而宿主机显卡
// 不会在程序运行期间变。缓存让导出 bundle、筛选种子、界面探测三处共用一次
// 结果，而不是各自启一次浏览器。
//
// 按内核版本分键而非全局单值：不同内核对同一块显卡报出的 renderer 字符串
// 可能不同（ANGLE 版本、驱动适配层都会变），混用会拿到错的值。
func (s *Service) DetectHostGPU(ctx context.Context, kernelVersion string) (HostGPUInfo, error) {
	k, err := s.kernels.Resolve(kernelVersion)
	if err != nil {
		return HostGPUInfo{}, err
	}

	if cached, ok := s.cachedHostGPU(k.Version); ok {
		return cached, nil
	}

	fam, renderer, err := probe.HostGPU(ctx, k.ExecPath)
	if err != nil {
		// 失败不缓存：可能是内核临时不可用或超时，下次该重试。
		// 缓存失败会让一次偶发故障持续影响整个会话。
		return HostGPUInfo{}, err
	}
	info := HostGPUInfo{Family: fam, Renderer: renderer}
	s.storeHostGPU(k.Version, info)
	return info, nil
}

// cachedHostGPU 读取某内核版本已探测过的宿主机 GPU。
func (s *Service) cachedHostGPU(kernelVersion string) (HostGPUInfo, bool) {
	s.gpuMu.Lock()
	defer s.gpuMu.Unlock()
	info, ok := s.gpuCache[kernelVersion]
	return info, ok
}

// storeHostGPU 缓存一次探测结果。
func (s *Service) storeHostGPU(kernelVersion string, info HostGPUInfo) {
	s.gpuMu.Lock()
	defer s.gpuMu.Unlock()
	if s.gpuCache == nil {
		s.gpuCache = map[string]HostGPUInfo{}
	}
	s.gpuCache[kernelVersion] = info
}

// SeedMatchAttempts 覆盖筛选种子的尝试上限，仅供测试使用。
// 留零时用 fingerprint.DefaultSeedAttempts。
//
// 生产代码不应设置它：调小会让本来能找到的机器筛不出结果，
// 用户会以为这台机器抽不到同族种子。
var SeedMatchAttempts int

// matchedSeed 生成一个派生 GPU 与宿主机同族的种子。
//
// 返回的第二个值是给用户看的说明，无论成败都有内容：成功时说明选中的型号，
// 失败时说明试了多少个、实际分布如何。这段文字会随 profile 一起呈现，
// 因为"筛选没成功"必须让用户知道——否则他会以为已经能过 Cloudflare 了。
func (s *Service) matchedSeed(ctx context.Context, kernelVersion string) (int32, string, error) {
	k, err := s.kernels.Resolve(kernelVersion)
	if err != nil {
		return 0, "", fmt.Errorf("筛选种子需要已安装内核: %w", err)
	}
	// 走 DetectHostGPU 而非直接调 probe：前者带缓存。用户通常先在界面上
	// 点「先探测本机 GPU」，再提交创建，这样能省掉第二次冷启动。
	info, err := s.DetectHostGPU(ctx, kernelVersion)
	if err != nil {
		return 0, "", err
	}
	host := info.Family

	// 错误原样上抛，包括 ErrNoMatchingSeed：调用方需要区分"没找到"
	// （可退回随机种子）与探测故障（应当报错），判断留给 CreateProfile。
	match, err := fingerprint.FindSeedForGPUFamily(
		ctx, kernelProber{execPath: k.ExecPath}, host, SeedMatchAttempts)
	if err != nil {
		return 0, "", err
	}
	note := fmt.Sprintf("已筛选种子匹配宿主机 GPU 族 %s（试 %d 个）：%s",
		host, len(match.Tried), match.Renderer)
	return match.Seed, note, nil
}
