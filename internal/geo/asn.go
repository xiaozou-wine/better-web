package geo

import (
	"regexp"
	"strconv"
	"strings"
)

// IPKind 是出口 IP 的网络类型判定结果。
//
// 反检测的关键不在于 IP 本身"干净"，而在于它与 profile 声称的身份是否一致：
// 一个声称"美国家庭用户 + Windows 笔记本"的 profile，出口落在 AWS 的 ASN 上
// 是单一信号就能定罪的矛盾。Cloudflare、Akamai 等都维护托管商 ASN 名单。
type IPKind string

const (
	// IPKindResidential 住宅或移动网络，是多账号场景唯一可靠的选择。
	IPKindResidential IPKind = "residential"
	// IPKindHosting 数据中心、云厂商或主机商，见即被标记。
	IPKindHosting IPKind = "hosting"
	// IPKindUnknown 无法判定。不等于安全，只表示信息不足。
	IPKindUnknown IPKind = "unknown"
)

// Risky 报告该类型是否不适合用于多账号场景。
// unknown 一并视为有风险：判不出来时应当提醒用户，而非默认放行。
func (k IPKind) Risky() bool { return k != IPKindResidential }

// ExitInfo 是出口 IP 的完整画像，供启动前校验与界面展示。
type ExitInfo struct {
	IP string `json:"ip"`
	// ASN 是自治系统号，如 16509。0 表示未取到。
	ASN int `json:"asn,omitempty"`
	// Org 是 ASN 所属组织名，如 "Amazon.com, Inc."。
	Org  string  `json:"org,omitempty"`
	Kind IPKind  `json:"kind"`
	Geo  ExitGeo `json:"geo"`
}

// ExitGeo 是 ExitInfo 内嵌的地理信息，避免调用方再引入 model 包。
//
// 用具名结构体而非匿名结构体别名：Wails 的 TS 类型生成器无法为匿名结构体
// 产出类型，会退化成 any，前端因此失去类型检查。
type ExitGeo struct {
	CountryCode string `json:"countryCode"`
	Region      string `json:"region"`
}

// hostingKeywords 是组织名中指示托管商的关键词，全小写。
//
// 用关键词而非硬编码 ASN 列表：云厂商持有数千个 ASN 且持续变动，
// 逐个枚举必然过期；而组织名里的 "cloud"、"hosting" 这类词稳定得多。
// 代价是可能漏判小众主机商，因此判定结果只用于警告，不用于阻断。
var hostingKeywords = []string{
	"amazon", "aws", "google", "microsoft", "azure", "oracle cloud",
	"digitalocean", "linode", "akamai", "cloudflare", "fastly",
	"ovh", "hetzner", "vultr", "scaleway", "contabo", "leaseweb",
	"alibaba", "tencent", "huawei cloud", "ucloud",
	"hosting", "datacenter", "data center", "colocation", "colo ",
	"server", "vps", "cloud", "dedicated", "m247", "choopa",
	// 以下主机商的名称不含任何通用托管词，只能逐个列出。
	// 实测发现：Cybercon（AS7393，圣路易斯的数据中心与 colocation 商）
	// 曾被判为 unknown。这类名字无规律，遇到就补一条。
	"cybercon", "digitalspace", "quadranet", "psychz", "nocix",
	"frantech", "buyvm", "ipxo", "zenlayer", "gthost",
}

// residentialKeywords 是指示住宅或移动网络的关键词，全小写。
// 命中这些词的优先判为住宅，因为部分 ISP 名称里也含 "cloud" 之类的噪声词。
var residentialKeywords = []string{
	"telecom", "broadband", "cable", "dsl", "fiber", "fibre",
	"wireless", "mobile", "cellular", "comcast", "verizon", "at&t",
	"charter", "spectrum", "cox communication", "centurylink",
	"deutsche telekom", "vodafone", "orange", "telefonica",
	"bt group", "sky broadband", "virgin media",
}

// asnPattern 匹配 "AS16509 Amazon.com, Inc." 这类前缀带 ASN 的组织串，
// 这是 ipinfo.io 的 org 字段格式。
var asnPattern = regexp.MustCompile(`^AS(\d+)\s*(.*)$`)

// ParseOrg 从组织串中拆出 ASN 与组织名。
// 不含 ASN 前缀时返回 0 与原串，调用方仍可据组织名判定类型。
func ParseOrg(org string) (asn int, name string) {
	org = strings.TrimSpace(org)
	m := asnPattern.FindStringSubmatch(org)
	if m == nil {
		return 0, org
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, org
	}
	return n, strings.TrimSpace(m[2])
}

// ClassifyOrg 按组织名判定 IP 类型。
//
// 住宅关键词优先于托管关键词：部分正规 ISP 名称含 "cloud" 等噪声词，
// 若托管优先会把真住宅误判成机房，导致用户被无谓地拦下。
func ClassifyOrg(org string) IPKind {
	s := strings.ToLower(strings.TrimSpace(org))
	if s == "" {
		return IPKindUnknown
	}
	for _, kw := range residentialKeywords {
		if strings.Contains(s, kw) {
			return IPKindResidential
		}
	}
	for _, kw := range hostingKeywords {
		if strings.Contains(s, kw) {
			return IPKindHosting
		}
	}
	return IPKindUnknown
}
