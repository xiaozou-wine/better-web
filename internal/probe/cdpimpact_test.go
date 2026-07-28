package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"better-web/internal/model"

	"github.com/gorilla/websocket"
)

// 启用 CDP 是否会拉低指纹跑分。
//
// 项目长期基于"CDP 有痕迹所以不能开"这个判断，但那是推断而非实测。本测试用
// 三组对照给出数据：不开端口 / 只开端口 / 开端口且有客户端持续发命令。
//
// 区分后两组是关键：检测站查的是 CDP 命令的副作用（Runtime.enable 改变异常栈
// 与 console 行为），端口无人连接时浏览器行为不变，只测"开了端口"会得出
// 偏乐观的结论。
//
// 内核升级后应重跑：若新版本收紧了 CDP 痕迹，本测试会失败并提示重新评估
// launchdebug 这条路是否仍然可用。
//
// 需要联网与真实内核，默认跳过。用 BW_PROBE_CDP=1 启用。
func TestCDPDoesNotDegradeScore(t *testing.T) {
	requireEnv(t, "BW_PROBE_CDP")
	k := realKernel(t)
	geo := &model.Geo{CountryCode: "US", Timezone: "America/Los_Angeles", Locale: "en-US"}
	_, baseArgs := probeProfile(t, 20260727, geo)

	const port = "9344" // 避开常用的 9222，减少与其他调试实例冲突
	cases := []struct {
		name   string
		extra  []string
		attach bool
	}{
		{name: "不开 CDP"},
		{name: "只开端口", extra: []string{"--remote-debugging-port=" + port,
			"--remote-allow-origins=*"}},
		{name: "开端口且客户端发命令", attach: true,
			extra: []string{"--remote-debugging-port=" + port, "--remote-allow-origins=*"}},
	}

	type score struct{ lies, headless, stealth int }
	got := make([]score, len(cases))

	for i, c := range cases {
		args := append(append([]string{}, baseArgs...), c.extra...)

		var stop func()
		if c.attach {
			stop = pokeCDPLoop(port)
		}
		res, err := (&Scorer{ExecPath: k.ExecPath, Sites: []Site{CreepJS}}).
			Run(context.Background(), args)
		if stop != nil {
			stop()
		}
		if err != nil {
			t.Fatalf("%s 跑分失败: %v", c.name, err)
		}
		if len(res) == 0 || res[0].Err != "" {
			t.Fatalf("%s 采集失败: %v", c.name, res)
		}
		m := res[0].Metrics
		got[i] = score{
			lies:     num(m["liesCount"]),
			headless: num(m["headlessPct"]),
			stealth:  num(m["stealthPct"]),
		}
		t.Logf("%-22s lies=%d headless=%d%% stealth=%d%%",
			c.name, got[i].lies, got[i].headless, got[i].stealth)
	}

	// 基准组本身必须是干净的，否则后面的对比没有意义。
	if got[0].lies != 0 {
		t.Fatalf("基准组 lies=%d，环境本身有问题，无法评估 CDP 的影响", got[0].lies)
	}

	// 关键断言：开 CDP 不应引入新的矛盾项或自动化特征。
	for i := 1; i < len(cases); i++ {
		if got[i].lies > got[0].lies {
			t.Errorf("%s 的 lies 从 %d 升到 %d —— CDP 引入了可检出的矛盾，"+
				"cmd/launchdebug 这条路需要重新评估",
				cases[i].name, got[0].lies, got[i].lies)
		}
		if got[i].headless > got[0].headless || got[i].stealth > got[0].stealth {
			t.Errorf("%s 的 headless/stealth 升高（%d/%d → %d/%d），CDP 痕迹已可被检出",
				cases[i].name, got[0].headless, got[0].stealth,
				got[i].headless, got[i].stealth)
		}
	}
}

func num(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return 0
}

// pokeCDPLoop 持续向调试端口发会留痕的 CDP 命令，返回停止函数。
func pokeCDPLoop(port string) func() {
	done := make(chan struct{})
	base := "http://127.0.0.1:" + port
	client := &http.Client{Timeout: 5 * time.Second}

	go func() {
		time.Sleep(3 * time.Second) // 等调试端口就绪
		ticker := time.NewTicker(1500 * time.Millisecond)
		defer ticker.Stop()
		var wsURL string
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
			}
			if wsURL == "" {
				wsURL = findPageTarget(client, base)
				continue
			}
			if err := pokeOnce(wsURL); err != nil {
				wsURL = "" // target 已关闭（跑分切页），重新找
			}
		}
	}()
	return func() { close(done) }
}

func findPageTarget(client *http.Client, base string) string {
	resp, err := client.Get(base + "/json/list")
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	var targets []struct {
		Type  string `json:"type"`
		URL   string `json:"url"`
		WSURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&targets); err != nil {
		return ""
	}
	for _, t := range targets {
		if t.Type == "page" && t.WSURL != "" && !strings.HasPrefix(t.URL, "about:") {
			return t.WSURL
		}
	}
	return ""
}

// pokeOnce 发一轮命令。Runtime.enable 是痕迹的主要来源——
// 它让 V8 向调试器上报异常与 console 调用，副作用是改变 Error.stack 行为。
func pokeOnce(wsURL string) error {
	d := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, _, err := d.Dial(wsURL, nil)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if err := conn.SetReadDeadline(time.Now().Add(8 * time.Second)); err != nil {
		return err
	}
	cmds := []map[string]any{
		{"id": 1, "method": "Runtime.enable"},
		{"id": 2, "method": "Page.enable"},
		{"id": 3, "method": "Network.enable"},
		{"id": 4, "method": "Runtime.evaluate",
			"params": map[string]any{"expression": "1+1", "returnByValue": true}},
	}
	for _, c := range cmds {
		if err := conn.WriteJSON(c); err != nil {
			return err
		}
	}
	for range cmds {
		if _, _, err := conn.ReadMessage(); err != nil {
			return fmt.Errorf("读回复失败: %w", err)
		}
	}
	return nil
}
