package app

import (
	"sync"
	"testing"

	"better-web/internal/model"
)

// 缓存命中后不得重复探测。
//
// 每次探测要冷启动一次浏览器（约 2 秒）且窗口会一闪而过，而宿主机显卡不会
// 在程序运行期间变。导出 bundle、筛选种子、界面探测三处都要这个值，
// 不缓存就是三次冷启动、三次闪窗。
//
// 直接操作缓存字段而非真跑探测：真探测需要已装内核且要两秒，
// 而这里要验证的是"命中缓存就不再探测"这条逻辑本身。
func TestDetectHostGPUServesFromCache(t *testing.T) {
	s, _ := newTestService(t)

	want := HostGPUInfo{
		Family:   model.GPUFamilyNVIDIA,
		Renderer: "ANGLE (NVIDIA, NVIDIA GeForce RTX 2070 GPU, D3D11)",
	}
	s.gpuMu.Lock()
	s.gpuCache = map[string]HostGPUInfo{"148.0.7778.215": want}
	s.gpuMu.Unlock()

	// newTestService 不装内核，所以真去探测必然失败。能拿到值就证明走了缓存。
	got, ok := s.cachedHostGPU("148.0.7778.215")
	if !ok {
		t.Fatal("应命中缓存")
	}
	if got != want {
		t.Errorf("缓存值不符: %+v", got)
	}
}

// 缓存必须按内核版本分键。
//
// 不同内核对同一块显卡报出的 renderer 可能不同（ANGLE 版本、驱动适配层
// 都会变），共用一个全局值会让某个版本拿到另一个版本的探测结果。
func TestGPUCacheKeyedByKernelVersion(t *testing.T) {
	s, _ := newTestService(t)

	s.gpuMu.Lock()
	s.gpuCache = map[string]HostGPUInfo{
		"148.0.7778.215": {Family: model.GPUFamilyNVIDIA, Renderer: "148 的字符串"},
		"150.0.7871.186": {Family: model.GPUFamilyNVIDIA, Renderer: "150 的字符串"},
	}
	s.gpuMu.Unlock()

	a, _ := s.cachedHostGPU("148.0.7778.215")
	b, _ := s.cachedHostGPU("150.0.7871.186")
	if a.Renderer == b.Renderer {
		t.Error("不同内核版本应各自缓存，不能串用")
	}
	if _, ok := s.cachedHostGPU("999.0.0.0"); ok {
		t.Error("未探测过的版本不应命中")
	}
}

// 并发读写缓存不得触发竞态。Service 的方法可并发调用，
// 而导出 bundle 与筛选种子都会碰这个缓存。用 -race 跑本测试。
func TestGPUCacheConcurrentAccess(t *testing.T) {
	s, _ := newTestService(t)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			s.gpuMu.Lock()
			if s.gpuCache == nil {
				s.gpuCache = map[string]HostGPUInfo{}
			}
			s.gpuCache["148"] = HostGPUInfo{Family: model.GPUFamilyNVIDIA}
			s.gpuMu.Unlock()
			_, _ = s.cachedHostGPU("148")
		}(i)
	}
	wg.Wait()
}
