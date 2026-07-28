package probe

import (
	"encoding/json"
	"fmt"
	"os"
)

// Gate 是跑分结果的准入门槛。
//
// 用途是内核升级后的回归检查：patch 在新版 Chromium 上失效往往不会报错，
// 只会让某些伪造项静默失灵。把关键指标定成门槛，升级时自动发现退化。
//
// 门槛只覆盖能确定为退化的指标。像 likeHeadless 这类受宿主机环境影响的
// 指标不设硬门槛，只记录趋势。
type Gate struct {
	// MaxLies 是允许的 CreepJS lies 项数上限。
	//
	// 默认 0：lies 衡量伪造项之间是否互相矛盾，出现任何一项都说明
	// 有伪造与其他特征打架，是必须查的退化。
	MaxLies int
	// MaxHeadlessPct 是允许的 headless 比率上限。
	// 该指标为确证性判定，非零即意味着暴露了无头特征。
	MaxHeadlessPct float64
	// MaxStealthPct 是允许的 stealth 比率上限。
	// 它衡量"刻意隐藏的痕迹"，伪造做得糙会拉高它。
	MaxStealthPct float64
	// RequireBrowserScanNormal 要求 BrowserScan 判定为 Normal。
	RequireBrowserScanNormal bool
}

// DefaultGate 是默认门槛，取自内核 148.0.7778.215 的实测基线。
//
// 三项都设为 0 不是理想主义：实测该内核在这三项上确实都是 0，
// 因此任何非零值都是相对已知良好状态的退化，值得中断并排查。
var DefaultGate = Gate{
	MaxLies:                  0,
	MaxHeadlessPct:           0,
	MaxStealthPct:            0,
	RequireBrowserScanNormal: true,
}

// Violation 是一条门槛违规。
type Violation struct {
	Site   string `json:"site"`
	Metric string `json:"metric"`
	Got    any    `json:"got"`
	Limit  any    `json:"limit"`
	Reason string `json:"reason"`
}

func (v Violation) String() string {
	return fmt.Sprintf("[%s] %s = %v（上限 %v）: %s",
		v.Site, v.Metric, v.Got, v.Limit, v.Reason)
}

// Check 用门槛校验跑分结果，返回全部违规项。
//
// 采集失败也算违规：拿不到数据无法判断是否退化，静默放过等于失去门禁作用。
func (g Gate) Check(results []ScoreResult) []Violation {
	var out []Violation
	seen := map[string]bool{}

	for _, r := range results {
		seen[r.Site] = true
		if r.Err != "" {
			out = append(out, Violation{
				Site: r.Site, Metric: "采集", Got: "失败", Limit: "成功",
				Reason: r.Err,
			})
			continue
		}
		switch r.Site {
		case CreepJS.Name:
			out = append(out, g.checkCreepJS(r)...)
		case BrowserScan.Name:
			out = append(out, g.checkBrowserScan(r)...)
		}
	}
	return out
}

func (g Gate) checkCreepJS(r ScoreResult) []Violation {
	var out []Violation

	// 评分未渲染说明这次采集不可信，不能当成"通过"。
	if rendered, ok := r.Metrics["rendered"].(bool); ok && !rendered {
		return []Violation{{
			Site: r.Site, Metric: "rendered", Got: false, Limit: true,
			Reason: "评分区块未渲染，本次结果不可信",
		}}
	}

	if n, ok := numMetric(r.Metrics, "liesCount"); ok && int(n) > g.MaxLies {
		out = append(out, Violation{
			Site: r.Site, Metric: "liesCount", Got: int(n), Limit: g.MaxLies,
			Reason: fmt.Sprintf("出现互相矛盾的伪造项: %v", r.Metrics["lies"]),
		})
	}
	if n, ok := numMetric(r.Metrics, "headlessPct"); ok && n > g.MaxHeadlessPct {
		out = append(out, Violation{
			Site: r.Site, Metric: "headlessPct", Got: n, Limit: g.MaxHeadlessPct,
			Reason: "暴露了无头浏览器的确证特征",
		})
	}
	if n, ok := numMetric(r.Metrics, "stealthPct"); ok && n > g.MaxStealthPct {
		out = append(out, Violation{
			Site: r.Site, Metric: "stealthPct", Got: n, Limit: g.MaxStealthPct,
			Reason: "暴露了刻意隐藏的痕迹，伪造实现可能已失效",
		})
	}
	return out
}

func (g Gate) checkBrowserScan(r ScoreResult) []Violation {
	if !g.RequireBrowserScanNormal {
		return nil
	}
	passed, ok := r.Metrics["passed"].(bool)
	if !ok {
		return []Violation{{
			Site: r.Site, Metric: "passed", Got: r.Metrics["passed"], Limit: true,
			Reason: "未能解析出判定结论，选择器可能已变更",
		}}
	}
	if !passed {
		return []Violation{{
			Site: r.Site, Metric: "verdict", Got: r.Metrics["verdict"], Limit: "Normal",
			Reason: "被识别为自动化环境",
		}}
	}
	return nil
}

// numMetric 从指标表中取数值。JSON 反序列化后数字均为 float64。
func numMetric(m map[string]any, key string) (float64, bool) {
	switch v := m[key].(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	}
	return 0, false
}

// LoadScores 读取归档的跑分结果，用于与新结果比对。
func LoadScores(path string) ([]ScoreResult, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []ScoreResult
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("解析跑分归档 %s 失败: %w", path, err)
	}
	return out, nil
}
