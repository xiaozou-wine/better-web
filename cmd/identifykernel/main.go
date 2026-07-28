// Command identifykernel 判断一个 CDP 调试端口后面是指纹内核还是普通 Chrome。
//
// 为什么需要它：端口号说明不了任何事——端口是启动参数，普通 Chrome 也能开在
// 任何端口上。而把爬虫或自动化接到错误的实例上，会静默地用真实环境出网：
// 不报错、页面正常打开，但指纹和 IP 都是本机的。
//
// 判据不看版本号（不同构建可能撞号），而是探测指纹伪造是否实际生效：
//   - 用 --fingerprint 启动的实例，navigator.hardwareConcurrency 等值受参数控制
//   - 普通 Chrome 报的是宿主机真实值
//
// 做法：读取目标实例的关键指纹项，与本机真实值比对。全部一致说明没有伪造。
//
// 用法：
//
//	go run ./cmd/identifykernel [端口]
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// probeExpr 在目标实例里执行，取回用于判定的指纹项。
const probeExpr = `JSON.stringify({
  ua: navigator.userAgent,
  platform: navigator.platform,
  cores: navigator.hardwareConcurrency,
  mem: navigator.deviceMemory,
  tz: Intl.DateTimeFormat().resolvedOptions().timeZone,
  lang: navigator.language,
  uaPlatform: navigator.userAgentData ? navigator.userAgentData.platform : '',
  uaPlatformVersion: '',
  webdriver: navigator.webdriver === true,
  screen: screen.width + 'x' + screen.height,
  webgl: (() => {
    try {
      const gl = document.createElement('canvas').getContext('webgl');
      const e = gl && gl.getExtension('WEBGL_debug_renderer_info');
      return e ? String(gl.getParameter(e.UNMASKED_RENDERER_WEBGL)) : '';
    } catch (err) { return ''; }
  })(),
})`

type probe struct {
	UA         string  `json:"ua"`
	Platform   string  `json:"platform"`
	Cores      int     `json:"cores"`
	Mem        float64 `json:"mem"`
	TZ         string  `json:"tz"`
	Lang       string  `json:"lang"`
	UAPlatform string  `json:"uaPlatform"`
	Webdriver  bool    `json:"webdriver"`
	Screen     string  `json:"screen"`
	WebGL      string  `json:"webgl"`
}

func main() {
	port := "9222"
	if len(os.Args) > 1 {
		port = os.Args[1]
	}
	base := "http://127.0.0.1:" + port
	client := &http.Client{Timeout: 6 * time.Second}

	ver, err := fetchVersion(client, base)
	if err != nil {
		fmt.Fprintf(os.Stderr, "端口 %s 无响应: %v\n", port, err)
		os.Exit(1)
	}
	fmt.Printf("端口 %s\n", port)
	fmt.Printf("  Browser        %s\n", ver["Browser"])
	fmt.Printf("  V8             %s\n", ver["V8-Version"])

	wsURL := findPage(client, base)
	if wsURL == "" {
		// 没有可用页面时新开一个：about:blank 上一样能读 navigator。
		if u := newTab(client, base); u != "" {
			wsURL = u
		}
	}
	if wsURL == "" {
		fmt.Fprintln(os.Stderr, "找不到可用的页面 target，无法探测")
		os.Exit(1)
	}

	p, err := evalProbe(wsURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "探测失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n目标实例报告的环境:\n")
	fmt.Printf("  platform       %s\n", p.Platform)
	fmt.Printf("  cores          %d\n", p.Cores)
	fmt.Printf("  deviceMemory   %v\n", p.Mem)
	fmt.Printf("  timezone       %s\n", p.TZ)
	fmt.Printf("  language       %s\n", p.Lang)
	fmt.Printf("  screen         %s\n", p.Screen)
	fmt.Printf("  webdriver      %v\n", p.Webdriver)
	if p.WebGL != "" {
		fmt.Printf("  WebGL renderer %s\n", trunc(p.WebGL, 66))
	}

	// 与本机真实值比对。核数是最可靠的一项：宿主机的逻辑核数是确定的，
	// 而指纹内核会按档案库的设定值上报。
	realCores := runtime.NumCPU()
	realTZ, _ := time.Now().Zone()
	fmt.Printf("\n本机真实值:\n")
	fmt.Printf("  逻辑核数       %d\n", realCores)
	fmt.Printf("  时区缩写       %s\n", realTZ)

	var signals []string
	if p.Cores != realCores {
		signals = append(signals, fmt.Sprintf(
			"核数不符（报 %d，本机 %d）—— 指纹参数已生效", p.Cores, realCores))
	}
	if p.Webdriver {
		signals = append(signals, "navigator.webdriver 为 true —— 自动化特征暴露")
	}

	fmt.Println()
	switch {
	case p.Cores != realCores:
		fmt.Println("判定: 指纹内核，且 --fingerprint-hardware-concurrency 已生效。")
		fmt.Println("      可以安全地把自动化接到这个端口。")
	case p.Cores == realCores:
		fmt.Println("判定: 无法确认伪造生效 —— 报告的核数与本机一致。")
		fmt.Println("      可能是普通 Chrome，也可能是档案库恰好设了相同核数。")
		fmt.Println("      接自动化前请先核对时区与出口 IP 是否符合该 profile 的预期。")
	}
	for _, s := range signals {
		fmt.Printf("  · %s\n", s)
	}

	// 出口 IP 要单独提示：它与内核无关，取决于有没有挂代理。
	fmt.Println("\n注意: 本工具只判断指纹是否生效，不检查出口 IP。")
	fmt.Println("      确认代理链路请用 cmd/checkproxy，或在实例里访问 IP 回显服务。")
}

func fetchVersion(c *http.Client, base string) (map[string]string, error) {
	resp, err := c.Get(base + "/json/version")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var m map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

func findPage(c *http.Client, base string) string {
	resp, err := c.Get(base + "/json/list")
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	var ts []struct {
		Type  string `json:"type"`
		URL   string `json:"url"`
		WSURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ts); err != nil {
		return ""
	}
	for _, t := range ts {
		if t.Type == "page" && t.WSURL != "" {
			return t.WSURL
		}
	}
	return ""
}

func newTab(c *http.Client, base string) string {
	req, err := http.NewRequest(http.MethodPut, base+"/json/new?about:blank", nil)
	if err != nil {
		return ""
	}
	resp, err := c.Do(req)
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	var t struct {
		WSURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&t); err != nil {
		return ""
	}
	return t.WSURL
}

func evalProbe(wsURL string) (probe, error) {
	var out probe
	d := websocket.Dialer{HandshakeTimeout: 6 * time.Second}
	conn, _, err := d.Dial(wsURL, nil)
	if err != nil {
		return out, fmt.Errorf("连接 target 失败（若报 403，说明实例启动时缺 "+
			"--remote-allow-origins=*）: %w", err)
	}
	defer func() { _ = conn.Close() }()
	if err := conn.SetReadDeadline(time.Now().Add(15 * time.Second)); err != nil {
		return out, err
	}

	if err := conn.WriteJSON(map[string]any{
		"id": 1, "method": "Runtime.evaluate",
		"params": map[string]any{"expression": probeExpr, "returnByValue": true},
	}); err != nil {
		return out, err
	}

	// 事件通知会混在回复里，按 id 筛出目标响应。
	for i := 0; i < 40; i++ {
		var msg struct {
			ID     int `json:"id"`
			Result struct {
				Result struct {
					Value string `json:"value"`
				} `json:"result"`
			} `json:"result"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := conn.ReadJSON(&msg); err != nil {
			return out, err
		}
		if msg.ID != 1 {
			continue
		}
		if msg.Error != nil {
			return out, fmt.Errorf("CDP 报错: %s", msg.Error.Message)
		}
		if err := json.Unmarshal([]byte(msg.Result.Result.Value), &out); err != nil {
			return out, fmt.Errorf("解析探测结果失败: %w", err)
		}
		return out, nil
	}
	return out, fmt.Errorf("未收到探测回复")
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.TrimSpace(s[:n]) + "…"
}
