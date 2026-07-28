package transfer

import (
	"fmt"
	"strings"

	"better-web/internal/model"
)

// Options 控制导入行为。
type Options struct {
	// NewSeeds 为 true 时给每条 profile 生成新种子，忽略文件里的值。
	//
	// 两种语义必须由调用方明确选择，没有安全的默认值：
	//   - 备份恢复、迁移到新机器：false，保留原种子，否则恢复出来的是另一台设备
	//   - 批量建号：true，否则所有 profile 共用同一套指纹，等于同一台机器
	NewSeeds bool

	// NamePrefix 非空时给每个导入的名称加前缀，用于区分批次或规避重名。
	NamePrefix string

	// Group 非空时覆盖文件里的分组，把整批归入同一组。
	Group string

	// SkipExistingNames 是已存在的名称集合，用于跳过重名而非报错。
	// 名称在库中有唯一索引，撞了必然写入失败。
	SkipExistingNames map[string]bool

	// HostGPUFamily 是本机真实 GPU 厂商族，用于与导出机器比对。
	//
	// 留空表示未探测，此时不产生跨厂商警告——探测要启动一次浏览器，
	// 不该为了一条警告而拖慢导入，由调用方决定是否值得探测。
	HostGPUFamily model.GPUFamily
}

// Result 是一次导入的结果。
//
// 逐条报告而非只给成败计数：导入 100 条时第 37 条失败，用户需要知道是哪条、
// 为什么，才能决定是修文件重来还是接受部分导入。
type Result struct {
	// Prepared 是校验通过、可以写入的 profile。调用方负责实际落库。
	Prepared []*model.Profile
	// Skipped 是因重名而跳过的条目名称。
	Skipped []string
	// Failed 是校验失败的条目及原因。
	Failed []Failure
	// Warnings 是不阻断导入但需用户知晓的问题。
	Warnings []string
}

// Failure 是单条导入失败的记录。
type Failure struct {
	// Index 是该条目在文件中的位置，从 1 开始，便于对照文件定位。
	Index int
	Name  string
	Err   string
}

// SeedFunc 生成新的指纹种子。由调用方注入，避免本包依赖 fingerprint。
type SeedFunc func() (int32, error)

// Prepare 校验并转换一份导出文件里的条目，不写库。
//
// 分成 Prepare 与实际落库两步：校验阶段的失败不应留下半写入的状态，
// 而逐条边校验边写入做不到这一点。
//
// dirFor 为给定 ID 生成 user-data-dir 路径，通常是 app.Paths.ProfileDir。
// newID 生成 profile ID，通常是 uuid.NewString。
func Prepare(b Bundle, opt Options, currentCatalogSize int,
	newID func() string, newSeed SeedFunc, dirFor func(id string) string) Result {

	var res Result

	// 档案库条目数不同会让同一种子抽到不同机型。只在保留原种子时才警告——
	// 生成新种子的场景下机型本来就会变，这条提示反而是噪声。
	if !opt.NewSeeds && b.CatalogSize > 0 && b.CatalogSize != currentCatalogSize {
		res.Warnings = append(res.Warnings, fmt.Sprintf(
			"导出时机型档案库有 %d 条，本机有 %d 条。机型按档案库长度取模抽取，"+
				"因此未锁定 DeviceLabel 的 profile 会抽到与原机器不同的机型。"+
				"其余指纹维度（canvas、audio）只取决于种子，不受影响。",
			b.CatalogSize, currentCatalogSize))
	}
	// GPU 厂商族不同会让原本筛过的种子重新变成跨厂商。与档案库那条同理，
	// 只在保留原种子时警告——生成新种子时原机器的 GPU 族已无关。
	//
	// 不阻断导入：跨厂商只影响能否过 Cloudflare 一类风控，profile 本身可用，
	// 而用户可能根本不在意那些站点。但必须让他知道，否则这是个静默退化。
	if !opt.NewSeeds && b.HostGPUFamily.Known() &&
		opt.HostGPUFamily.Known() && b.HostGPUFamily != opt.HostGPUFamily {
		res.Warnings = append(res.Warnings, fmt.Sprintf(
			"导出机器的 GPU 是 %s，本机是 %s。种子派生出的 GPU 型号是固定的，"+
				"因此在原机器上筛过的种子在本机会变成跨厂商——实测这会被 Cloudflare "+
				"拦截。需要过这类风控的 profile 请在本机重建并勾选匹配本机 GPU。",
			b.HostGPUFamily, opt.HostGPUFamily))
	}
	if !b.WithSecrets {
		var needPassword int
		for _, e := range b.Profiles {
			if e.Proxy != nil && e.Proxy.HadPassword {
				needPassword++
			}
		}
		if needPassword > 0 {
			res.Warnings = append(res.Warnings, fmt.Sprintf(
				"%d 个 profile 的代理原本设了密码，但导出文件不含凭据，"+
					"导入后需逐个补填，否则代理认证会失败。", needPassword))
		}
	}

	// 文件内部的重名也要拦：同一批里两条同名，第二条写库时才失败，
	// 那时第一条已经落库，状态不一致。
	seenInFile := map[string]bool{}

	for i, e := range b.Profiles {
		idx := i + 1
		name := strings.TrimSpace(e.Name)
		if opt.NamePrefix != "" {
			name = opt.NamePrefix + name
		}

		if name == "" {
			res.Failed = append(res.Failed, Failure{
				Index: idx, Name: e.Name, Err: "名称为空"})
			continue
		}
		if !e.Kind.Valid() {
			res.Failed = append(res.Failed, Failure{
				Index: idx, Name: name,
				Err: fmt.Sprintf("类型 %q 无效（应为 daily 或 fingerprint）", e.Kind)})
			continue
		}
		if seenInFile[name] {
			res.Failed = append(res.Failed, Failure{
				Index: idx, Name: name, Err: "文件中存在同名条目"})
			continue
		}
		if opt.SkipExistingNames[name] {
			res.Skipped = append(res.Skipped, name)
			continue
		}

		proxy, err := toProxy(e.Proxy)
		if err != nil {
			res.Failed = append(res.Failed, Failure{Index: idx, Name: name, Err: err.Error()})
			continue
		}

		id := newID()
		p := &model.Profile{
			ID: id, Name: name, Kind: e.Kind,
			ProfileDir:      dirFor(id),
			Proxy:           proxy,
			GeoOverride:     e.GeoOverride,
			KernelVersion:   e.KernelVersion,
			DisableSpoofing: e.DisableSpoofing,
			ExtraArgs:       e.ExtraArgs,
			DeviceLabel:     e.DeviceLabel,
			Group:           e.Group,
			Tags:            model.NormalizeTags(e.Tags),
			Notes:           e.Notes,
		}
		if opt.Group != "" {
			p.Group = opt.Group
		}

		// 种子：仅指纹模式需要。日常模式不做伪造，种子无意义。
		if e.Kind == model.KindFingerprint {
			switch {
			case opt.NewSeeds || e.Seed == 0:
				s, err := newSeed()
				if err != nil {
					res.Failed = append(res.Failed, Failure{
						Index: idx, Name: name, Err: "生成种子失败: " + err.Error()})
					continue
				}
				p.Seed = s
			default:
				p.Seed = e.Seed
			}
		}

		seenInFile[name] = true
		res.Prepared = append(res.Prepared, p)
	}

	// 批量建号时若种子来自文件且多条重复，等于多个 profile 共用一套指纹。
	// 这是最容易被平台关联的错误配置，必须报出来。
	if !opt.NewSeeds {
		if dup := duplicateSeeds(res.Prepared); len(dup) > 0 {
			res.Warnings = append(res.Warnings, fmt.Sprintf(
				"%d 组 profile 共用相同种子，它们的 canvas 等指纹完全一致，"+
					"平台侧可直接关联。若这些是不同账号，请改用生成新种子的方式导入。",
				len(dup)))
		}
	}
	return res
}

// toProxy 把导出格式的代理配置转成领域模型，并做基本校验。
func toProxy(pe *ProxyEntry) (*model.Proxy, error) {
	if pe == nil {
		return nil, nil
	}
	switch pe.Scheme {
	case model.ProxyHTTP, model.ProxyHTTPS, model.ProxySOCKS5:
	default:
		return nil, fmt.Errorf("代理协议 %q 无效（应为 http/https/socks5）", pe.Scheme)
	}
	if strings.TrimSpace(pe.Host) == "" {
		return nil, fmt.Errorf("代理缺少主机名")
	}
	if pe.Port < 1 || pe.Port > 65535 {
		return nil, fmt.Errorf("代理端口 %d 越界（应为 1-65535）", pe.Port)
	}
	return &model.Proxy{
		Scheme: pe.Scheme, Host: strings.TrimSpace(pe.Host), Port: pe.Port,
		Username: pe.Username, Password: pe.Password,
	}, nil
}

// duplicateSeeds 返回被多条 profile 共用的种子。
func duplicateSeeds(list []*model.Profile) []int32 {
	count := map[int32]int{}
	for _, p := range list {
		if p.Kind == model.KindFingerprint && p.Seed != 0 {
			count[p.Seed]++
		}
	}
	var dup []int32
	for seed, n := range count {
		if n > 1 {
			dup = append(dup, seed)
		}
	}
	return dup
}
