package fingerprint

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"math"
	"strconv"
	"strings"

	"better-web/internal/model"
)

// fallbackBrandVersion 是取不到内核实际版本时使用的品牌大版本。
//
// 品牌版本应当由内核实际版本推导而非手工维护：内核报 152 而品牌声称 148
// 是可直接检出的矛盾，而手工维护的常量迟早会忘记同步。此处仅作兜底，
// 正常路径请用 DeriveForKernel 传入内核版本。
//
// 兜底值对齐 fingerprint-chromium 148（Chromium 148.0.7778.215）。
const fallbackBrandVersion = "148"

// brandVersion 由内核完整版本号推导品牌大版本。
//
// Chromium 系浏览器的品牌大版本与内核大版本一致，所以直接取主版本号即可。
// 解析失败时退回兜底值，不返回错误：版本号异常不应阻止 profile 启动。
func brandVersion(kernelVersion string) string {
	major, _, _ := strings.Cut(kernelVersion, ".")
	major = strings.TrimSpace(major)
	if major == "" {
		return fallbackBrandVersion
	}
	if _, err := strconv.Atoi(major); err != nil {
		return fallbackBrandVersion
	}
	return major
}

// brandWeights 按真实市场占比给品牌加权，Chrome 应占绝大多数。
// 抽到罕见品牌反而会缩小匿名集。
var brandWeights = []struct {
	brand  model.Brand
	weight int
}{
	{model.BrandChrome, 88},
	{model.BrandEdge, 12},
}

// NewSeed 生成一个密码学安全的随机种子。种子是 profile 身份的根，
// 一旦生成就不应再变，否则该 profile 的指纹会整体漂移。
func NewSeed() (int32, error) {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return 0, fmt.Errorf("生成指纹种子失败: %w", err)
	}
	// 收敛到正数区间，规避 --fingerprint 对负值处理的不确定性。
	v := int32(binary.LittleEndian.Uint32(buf[:]) & 0x7FFFFFFF)
	if v == 0 {
		v = 1
	}
	return v, nil
}

// derived 是从种子派生的一组独立伪随机流。用不同 salt 派生保证各维度取值
// 互不相关，同时整体对 seed 保持确定性。
type derived struct {
	seed int32
}

// pick 用给定 salt 从 seed 派生一个 [0,n) 的确定性索引。
func (d derived) pick(salt string, n int) int {
	if n <= 0 {
		return 0
	}
	h := fnv.New64a()
	// 忽略 Write 的错误：fnv 的实现从不返回错误。
	_, _ = h.Write([]byte(salt))
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], uint32(d.seed))
	_, _ = h.Write(b[:])
	return int(h.Sum64() % uint64(n))
}

// Derive 把种子与地理信息推导成完整的指纹环境，品牌版本使用兜底值。
//
// geo 决定时区与语言，必须与代理出口地一致；传 nil 时回退到美国东部的
// 常见组合，但调用方应当总是提供真实的出口地理信息。
//
// 已知内核版本时应改用 DeriveForKernel，让品牌版本与内核实际版本对齐。
func Derive(seed int32, geo *model.Geo) model.Fingerprint {
	return DeriveForKernel(seed, geo, "")
}

// DeriveForKernel 与 Derive 一致，但用给定内核版本推导品牌版本。
//
// kernelVersion 传内核的完整版本号（如 148.0.7778.215），为空时用兜底值。
// 让品牌版本跟随内核实际版本，避免手工维护常量导致的版本矛盾。
func DeriveForKernel(seed int32, geo *model.Geo, kernelVersion string) model.Fingerprint {
	d := derived{seed: seed}

	// 抽取始终基于完整档案库的长度，抽中有已知缺陷的档案时替换为同位置的
	// 安全档案（见 pickDevice）。若改成在筛后的短表上取模，候选集大小一变，
	// 几乎所有种子都会抽到不同机型——实测 2000 个种子中 90.8% 会漂移，
	// 代价远超它修掉的那一个缺陷档案。
	device := pickDevice(d)
	brand := pickBrand(d)

	tz, locale := "America/New_York", "en-US"
	if geo != nil {
		if geo.Timezone != "" {
			tz = geo.Timezone
		}
		if geo.Locale != "" {
			locale = geo.Locale
		}
	}

	return model.Fingerprint{
		Seed:            seed,
		Device:          device,
		Brand:           brand,
		BrandVersion:    brandVersion(kernelVersion),
		Timezone:        tz,
		Locale:          locale,
		AcceptLanguages: AcceptLanguage(locale),
	}
}

// DeriveWithDevice 与 Derive 一致，但强制使用指定机型，供用户手动锁定机型。
func DeriveWithDevice(seed int32, geo *model.Geo, device model.DeviceProfile) model.Fingerprint {
	fp := Derive(seed, geo)
	fp.Device = device
	return fp
}

// DeriveWithDeviceLabel 按机型标签推导指纹。
//
// label 为空或在档案库中找不到时，回退为按种子抽取——找不到通常意味着
// 档案库删掉了该条目，此时静默换设备比报错更危险，因此由调用方通过
// FindDevice 的第二个返回值判断是否需要提示用户。
func DeriveWithDeviceLabel(seed int32, geo *model.Geo, label, kernelVersion string) model.Fingerprint {
	fp := DeriveForKernel(seed, geo, kernelVersion)
	if label == "" {
		return fp
	}
	if device, ok := FindDevice(label); ok {
		fp.Device = device
	}
	return fp
}

// FindDevice 按标签查找机型档案。第二个返回值报告是否找到。
func FindDevice(label string) (model.DeviceProfile, bool) {
	for _, d := range deviceCatalog {
		if d.Label == label {
			return d, true
		}
	}
	return model.DeviceProfile{}, false
}

func pickBrand(d derived) model.Brand {
	total := 0
	for _, b := range brandWeights {
		total += b.weight
	}
	n := d.pick("brand", total)
	for _, b := range brandWeights {
		if n < b.weight {
			return b.brand
		}
		n -= b.weight
	}
	return model.BrandChrome
}

// AcceptLanguage 由主 locale 构造 Accept-Language 头的值。
// 真实浏览器会带上不含地区的基语言作为次选，缺失会显得可疑。
func AcceptLanguage(locale string) string {
	base := locale
	if i := strings.IndexByte(locale, '-'); i > 0 {
		base = locale[:i]
	}
	if base == locale {
		return locale
	}
	return fmt.Sprintf("%s,%s;q=0.9", locale, base)
}

// deviceMemoryBuckets 是 W3C device-memory 规范允许的全部取值，升序。
// 报出档位外的值（例如 6 或 12）会立刻暴露伪造。
var deviceMemoryBuckets = []float64{0.25, 0.5, 1, 2, 4, 8}

// NormalizeDeviceMemory 按 Chrome 的规范算法把物理内存换算成
// navigator.deviceMemory 的上报值：向下取整到最近的 2 的幂，再夹到
// [0.25, 8] 区间。所以 16GiB 的真机报 8，6GiB 的真机报 4。
// 参考 https://developer.chrome.com/blog/device-memory/
//
// 用途仅限校验档案库里 DeviceMemory 的取值是否落在规范档位内，
// 不影响运行时行为：该字段没有对应命令行参数，内核 148 起自行从种子
// 在 8/16/32 中选值上报——即已超出本函数的 [0.25, 8] 值域。
// 需要判断浏览器实际会报什么时，不要调用本函数，去实测。
func NormalizeDeviceMemory(gib float64) float64 {
	lowest, highest := deviceMemoryBuckets[0], deviceMemoryBuckets[len(deviceMemoryBuckets)-1]
	if gib <= lowest {
		return lowest
	}
	if gib >= highest {
		return highest
	}
	// 向下取整到最近的 2 的幂。
	return math.Exp2(math.Floor(math.Log2(gib)))
}
