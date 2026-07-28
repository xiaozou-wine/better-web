package geo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"better-web/internal/model"
)

// LookupTimeout 是单次出口地查询的超时。住宅代理延迟普遍偏高，
// 但启动流程不该被无限阻塞。
const LookupTimeout = 10 * time.Second

// Endpoint 描述一个出口地查询服务。多个服务轮替以避免单点失败与限流。
type Endpoint struct {
	Name string
	URL  string
	// Parse 从响应体解出国家码与地区码。
	Parse func([]byte) (country, region string, err error)
	// ParseExit 额外解出 IP 与 ASN 组织信息，用于判定出口是住宅还是机房。
	// 可为 nil：并非所有服务都提供这些字段，此时只做地理判定。
	ParseExit func([]byte) (ip, org string, err error)
}

// DefaultEndpoints 是默认的查询服务列表，按顺序尝试。
// 全部使用免登录的公共接口，只取国家与地区字段。
var DefaultEndpoints = []Endpoint{
	{
		Name: "ipinfo.io",
		URL:  "https://ipinfo.io/json",
		Parse: func(b []byte) (string, string, error) {
			var r struct {
				Country string `json:"country"`
				Region  string `json:"region"`
			}
			if err := json.Unmarshal(b, &r); err != nil {
				return "", "", err
			}
			return r.Country, r.Region, nil
		},
		ParseExit: func(b []byte) (string, string, error) {
			// org 形如 "AS16509 Amazon.com, Inc."。
			var r struct {
				IP  string `json:"ip"`
				Org string `json:"org"`
			}
			if err := json.Unmarshal(b, &r); err != nil {
				return "", "", err
			}
			return r.IP, r.Org, nil
		},
	},
	{
		Name: "ip-api.com",
		URL:  "http://ip-api.com/json/?fields=countryCode,region,query,as,isp",
		Parse: func(b []byte) (string, string, error) {
			var r struct {
				CountryCode string `json:"countryCode"`
				Region      string `json:"region"`
			}
			if err := json.Unmarshal(b, &r); err != nil {
				return "", "", err
			}
			return r.CountryCode, r.Region, nil
		},
		ParseExit: func(b []byte) (string, string, error) {
			var r struct {
				Query string `json:"query"` // 出口 IP
				AS    string `json:"as"`    // 形如 "AS16509 Amazon.com, Inc."
				ISP   string `json:"isp"`
			}
			if err := json.Unmarshal(b, &r); err != nil {
				return "", "", err
			}
			// as 字段带 ASN 前缀，信息更全；缺失时退回 isp。
			org := r.AS
			if org == "" {
				org = r.ISP
			}
			return r.Query, org, nil
		},
	},
	{
		Name:  "cloudflare",
		URL:   "https://cloudflare.com/cdn-cgi/trace",
		Parse: parseCloudflareTrace,
	},
}

// Resolver 通过给定的 HTTP 客户端查询出口地。
// 调用方必须传入走目标代理的客户端，否则查到的是本机出口而非代理出口。
type Resolver struct {
	Client    *http.Client
	Endpoints []Endpoint
}

// NewResolver 用指定客户端构造 Resolver。client 为 nil 时使用直连客户端，
// 这只适用于不配代理的场景。
func NewResolver(client *http.Client) *Resolver {
	if client == nil {
		client = &http.Client{Timeout: LookupTimeout}
	}
	return &Resolver{Client: client, Endpoints: DefaultEndpoints}
}

// ErrAllEndpointsFailed 表示所有查询服务都失败。
var ErrAllEndpointsFailed = errors.New("所有出口地查询服务均失败")

// Lookup 查询当前客户端的出口地并推导时区与语言。
// 所有服务均失败时返回 ErrAllEndpointsFailed，调用方应决定是回退到
// Fallback 还是中止启动——静默回退会导致时区与真实出口不符。
func (r *Resolver) Lookup(ctx context.Context) (model.Geo, error) {
	var errs []error
	for _, ep := range r.Endpoints {
		country, region, err := r.query(ctx, ep)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", ep.Name, err))
			continue
		}
		if country == "" {
			errs = append(errs, fmt.Errorf("%s: 响应中缺少国家码", ep.Name))
			continue
		}
		return Resolve(country, region), nil
	}
	return model.Geo{}, fmt.Errorf("%w: %w", ErrAllEndpointsFailed, errors.Join(errs...))
}

// LookupExit 查询出口 IP 的完整画像：地理信息 + ASN 类型判定。
//
// 与 Lookup 的分工：Lookup 只为对齐时区语言，是启动的必要前提；
// LookupExit 额外判定出口是住宅还是机房，用于启动前提醒用户配置矛盾。
// 判定基于组织名关键词，会有漏判，因此结果只应用于警告而非阻断。
//
// 只有部分服务提供 ASN 信息，全部服务都取不到时返回的 ExitInfo 中
// Kind 为 unknown，但地理信息仍然有效。
func (r *Resolver) LookupExit(ctx context.Context) (ExitInfo, error) {
	var errs []error
	for _, ep := range r.Endpoints {
		body, err := r.fetch(ctx, ep)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", ep.Name, err))
			continue
		}
		country, region, err := ep.Parse(body)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", ep.Name, err))
			continue
		}
		if country == "" {
			errs = append(errs, fmt.Errorf("%s: 响应中缺少国家码", ep.Name))
			continue
		}

		info := ExitInfo{Kind: IPKindUnknown}
		info.Geo.CountryCode, info.Geo.Region = country, region

		// ASN 信息是增值项，取不到不影响本次查询成功。
		if ep.ParseExit != nil {
			if ip, org, err := ep.ParseExit(body); err == nil {
				info.IP = ip
				info.ASN, info.Org = ParseOrg(org)
				info.Kind = ClassifyOrg(org)
			}
		}
		return info, nil
	}
	return ExitInfo{}, fmt.Errorf("%w: %w", ErrAllEndpointsFailed, errors.Join(errs...))
}

// fetch 取回响应体，不做解析。
func (r *Resolver) fetch(ctx context.Context, ep Endpoint) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, LookupTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ep.URL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := r.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	// 限制读取长度，防止异常响应或代理注入的页面撑爆内存。
	return readAtMost(resp.Body, 64<<10)
}

func (r *Resolver) query(ctx context.Context, ep Endpoint) (string, string, error) {
	body, err := r.fetch(ctx, ep)
	if err != nil {
		return "", "", err
	}
	return ep.Parse(body)
}
