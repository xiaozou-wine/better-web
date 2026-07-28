// Package transfer 实现 profile 配置的批量导出与导入。
//
// 用途有两类，它们对种子的要求恰好相反：
//   - 备份恢复 / 迁移到新机器：必须保留原种子，否则恢复出来的是另一台设备
//   - 批量建号：必须生成新种子，否则所有 profile 共用同一套指纹
//
// 因此导出格式记录种子，由导入方通过 Options.NewSeeds 决定用哪种语义。
//
// 只搬配置，不搬浏览数据。user-data-dir 里的 Cookie 与登录态是 Chromium 的
// 私有 SQLite 格式，几十到几百 MB，且两台机器同时使用会冲突——那需要另一套
// 机制，不在本包范围内。
package transfer

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"better-web/internal/model"
)

// FormatVersion 是导出文件的格式版本。
//
// 导入时校验：读到更高的版本号说明文件来自更新的程序，其中可能有本版本
// 不认识的字段，静默忽略会让用户以为导入完整了。
const FormatVersion = 1

// Bundle 是一份导出文件的完整内容。
type Bundle struct {
	FormatVersion int       `json:"formatVersion"`
	ExportedAt    time.Time `json:"exportedAt"`

	// CatalogSize 是导出时机型档案库的条目数。
	//
	// 记录它是因为机型抽取对档案库长度取模：条目数不同的两台机器上，
	// 同一种子会抽到不同机型。导入时若数量不符，会警告用户 profile 的
	// 机型可能与原机器不同。DeviceLabel 非空的 profile 不受影响。
	CatalogSize int `json:"catalogSize"`

	// HostGPUFamily 是导出机器的真实 GPU 厂商族，可能为空（未探测过）。
	//
	// 记录它的理由与 CatalogSize 同类：种子派生出的 GPU 是绝对的，而"是否
	// 与宿主机同族"是相对的。实测 Cloudflare 拦截的判据正是伪造 GPU 与宿主机
	// 跨厂商（见 README），所以在 NVIDIA 机器上筛好的种子搬到 Intel 机器上
	// 就又变成跨厂商了——profile 配置一个字没改，却从能过变成被拦。
	//
	// 这种退化没有任何界面痕迹，只有实测才能发现，因此必须在导入时警告。
	HostGPUFamily model.GPUFamily `json:"hostGPUFamily,omitempty"`

	// WithSecrets 表示本文件是否含代理密码明文。
	WithSecrets bool `json:"withSecrets"`

	Profiles []Entry `json:"profiles"`
}

// Entry 是导出文件里的单条 profile 配置。
//
// 与 model.Profile 的差别：不含 ID 与 ProfileDir。两者都是本机特有的——
// ID 在导入时重新生成以免与既有记录冲突，ProfileDir 由目标机器的数据目录
// 布局决定。时间戳也不导出，导入即视为新建。
type Entry struct {
	Name string            `json:"name"`
	Kind model.ProfileKind `json:"kind"`
	// Seed 为 0 表示由导入方生成。指纹模式下非 0 时按原值恢复。
	Seed int32 `json:"seed,omitempty"`

	Proxy *ProxyEntry `json:"proxy,omitempty"`

	GeoOverride     *model.Geo          `json:"geoOverride,omitempty"`
	KernelVersion   string              `json:"kernelVersion,omitempty"`
	DisableSpoofing []model.SpoofTarget `json:"disableSpoofing,omitempty"`
	ExtraArgs       []string            `json:"extraArgs,omitempty"`
	DeviceLabel     string              `json:"deviceLabel,omitempty"`
	Group           string              `json:"group,omitempty"`
	Tags            []string            `json:"tags,omitempty"`
	Notes           string              `json:"notes,omitempty"`
}

// ProxyEntry 是导出文件里的代理配置。
//
// Password 单独定义而非复用 model.Proxy：那个结构体的 JSON 标签会无条件
// 带上密码，而导出默认必须不含密码——凭据一旦进了普通文件，就会被复制、
// 发送、提交到仓库。
type ProxyEntry struct {
	Scheme   model.ProxyScheme `json:"scheme"`
	Host     string            `json:"host"`
	Port     int               `json:"port"`
	Username string            `json:"username,omitempty"`
	// Password 仅在导出时显式要求带凭据才有值。
	Password string `json:"password,omitempty"`
	// HadPassword 标记原 profile 设置过密码但本文件未包含它，
	// 便于导入后提示用户需要补填，而不是让代理静默认证失败。
	HadPassword bool `json:"hadPassword,omitempty"`
}

// Export 把 profile 列表写成 JSON。
//
// withSecrets 为 true 时包含代理密码明文。默认不包含：导出文件常被复制、
// 通过聊天工具发送、甚至提交到仓库，凭据进入这类文件的风险远高于
// 重新填一次密码的成本。
// hostGPU 传导出机器的 GPU 厂商族，未探测时传空——它只用于导入时的
// 跨机器警告，取不到就不警告，不该为此阻断导出。
func Export(w io.Writer, profiles []*model.Profile, catalogSize int,
	withSecrets bool, hostGPU model.GPUFamily) error {
	b := Bundle{
		FormatVersion: FormatVersion,
		ExportedAt:    time.Now(),
		CatalogSize:   catalogSize,
		HostGPUFamily: hostGPU,
		WithSecrets:   withSecrets,
		Profiles:      make([]Entry, 0, len(profiles)),
	}
	for _, p := range profiles {
		if p == nil {
			continue
		}
		e := Entry{
			Name: p.Name, Kind: p.Kind, Seed: p.Seed,
			GeoOverride: p.GeoOverride, KernelVersion: p.KernelVersion,
			DisableSpoofing: p.DisableSpoofing, ExtraArgs: p.ExtraArgs,
			DeviceLabel: p.DeviceLabel, Group: p.Group,
			Tags: p.Tags, Notes: p.Notes,
		}
		if p.Proxy != nil {
			pe := &ProxyEntry{
				Scheme: p.Proxy.Scheme, Host: p.Proxy.Host,
				Port: p.Proxy.Port, Username: p.Proxy.Username,
			}
			if withSecrets {
				pe.Password = p.Proxy.Password
			} else {
				pe.HadPassword = p.Proxy.Password != ""
			}
			e.Proxy = pe
		}
		b.Profiles = append(b.Profiles, e)
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(b); err != nil {
		return fmt.Errorf("写入导出文件失败: %w", err)
	}
	return nil
}

// Parse 读取并校验一份导出文件。
func Parse(r io.Reader) (Bundle, error) {
	var b Bundle
	// 限制读取量：导出文件只含配置，正常不会到 32MB。
	// 不设上限时一个畸形文件就能耗尽内存。
	dec := json.NewDecoder(io.LimitReader(r, 32<<20))
	if err := dec.Decode(&b); err != nil {
		return Bundle{}, fmt.Errorf("解析导出文件失败: %w", err)
	}
	if b.FormatVersion == 0 {
		return Bundle{}, fmt.Errorf("文件缺少 formatVersion 字段，不是有效的导出文件")
	}
	if b.FormatVersion > FormatVersion {
		return Bundle{}, fmt.Errorf(
			"文件格式版本 %d 高于本程序支持的 %d，其中可能含无法识别的配置；"+
				"请升级程序后再导入", b.FormatVersion, FormatVersion)
	}
	if len(b.Profiles) == 0 {
		return Bundle{}, fmt.Errorf("文件中没有 profile")
	}
	return b, nil
}
