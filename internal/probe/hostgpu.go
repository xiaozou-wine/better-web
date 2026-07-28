package probe

import (
	"context"
	"fmt"
	"time"

	"better-web/internal/model"
)

// HostGPU 探测宿主机的真实 GPU 厂商族。
//
// 实现方式：启动内核并显式关闭 GPU 伪造，读 WebGL 的 renderer 字符串。
// 不读注册表或调用系统 API——要的是"浏览器会报出什么"，那才是检测方看到的
// 值；系统层查到的型号名与 ANGLE 报出的字符串未必一致。
//
// 必须传 --disable-spoofing=gpu：否则读到的是伪造后的值，探测就没有意义。
// 也必须保留 GPU（不能加 --disable-gpu），否则 WebGL 退化为 SwiftShader，
// 报出的 renderer 与真实驱动无关。
func HostGPU(ctx context.Context, execPath string) (model.GPUFamily, string, error) {
	p := &GPUProbe{ExecPath: execPath, Timeout: 45 * time.Second}
	// --fingerprint=1 只为让 --disable-spoofing 生效路径与生产一致；
	// 该参数不影响 GPU，因为下一个参数已把 GPU 伪造关掉。
	rep, err := p.CollectGPU(ctx, []string{
		"--fingerprint=1", "--disable-spoofing=gpu",
	})
	if err != nil {
		return model.GPUFamilyUnknown, "", fmt.Errorf("探测宿主机 GPU 失败: %w", err)
	}
	renderer := rep.WebGL1.Renderer
	if renderer == "" {
		renderer = rep.WebGL2.Renderer
	}
	if renderer == "" {
		return model.GPUFamilyUnknown, "", fmt.Errorf(
			"未能读到 WebGL renderer，可能是内核以软件渲染启动")
	}
	fam := model.ParseGPUFamily(renderer)
	if !fam.Known() {
		// 不静默返回 unknown：调用方据此决定要不要筛种子，
		// 判不出族却当成成功会让筛选逻辑空转。
		return fam, renderer, fmt.Errorf(
			"无法从 renderer %q 判定 GPU 厂商族", renderer)
	}
	return fam, renderer, nil
}

// SeedGPUFamily 报告给定种子会派生出哪个 GPU 厂商族。
//
// GPU 由内核从种子自行派生，没有对应的命令行参数，也无法在 Go 侧计算——
// 派生算法在内核的 C++ 代码里。所以只能实际启动一次内核来问。
// 这是筛选种子必须付出的成本，每个候选种子一次冷启动。
func SeedGPUFamily(ctx context.Context, execPath string, seed int32) (model.GPUFamily, string, error) {
	p := &GPUProbe{ExecPath: execPath, Timeout: 45 * time.Second}
	rep, err := p.CollectGPU(ctx, []string{
		fmt.Sprintf("--fingerprint=%d", seed),
	})
	if err != nil {
		return model.GPUFamilyUnknown, "", err
	}
	renderer := rep.WebGL1.Renderer
	if renderer == "" {
		renderer = rep.WebGL2.Renderer
	}
	return model.ParseGPUFamily(renderer), renderer, nil
}
