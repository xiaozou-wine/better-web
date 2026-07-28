package probe

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"

	"better-web/internal/launcher"
	"better-web/internal/model"
)

// 用真实内核验证启动页与新标签页配置确实生效。
//
// 判据是端到端事实：起一个本地服务当目标页，浏览器真打开了它服务端才会
// 收到请求。这比检查配置文件可靠——参数写对了但内核不认同样表现为无效，
// 而那正是最容易犯的错（本项目起初用 Preferences 的 session.startup_urls，
// 内核会读入并保留该键却仍打开新标签页，就是这么发现的）。
//
//	BW_RUN_SCORE=1 go test -run TestRealKernelStartupOptions -timeout 300s -v ./internal/probe/
func TestRealKernelStartupOptions(t *testing.T) {
	if os.Getenv("BW_RUN_SCORE") != "1" {
		t.Skip("未设置 BW_RUN_SCORE=1，跳过真实内核启动页验证")
	}
	k := realKernel(t)

	cases := []struct {
		name    string
		startup *model.Startup
		// wantHit 为 true 时期望收到请求。
		wantHit bool
	}{
		{
			name:    "启动页 URL",
			startup: &model.Startup{Mode: model.StartupURLs},
			wantHit: true,
		},
		{
			name: "自定义新标签页",
			// 新标签页模式下内核会打开新标签页，若 --custom-ntp 生效
			// 就会请求我们的地址。
			startup: &model.Startup{Mode: model.StartupNewTab},
			wantHit: true,
		},
		{
			name:    "默认（不配置）",
			startup: nil,
			wantHit: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hit := make(chan string, 4)
			url, stop := startHitServer(t, hit)
			defer stop()

			// 按用例把地址填进对应字段。
			st := c.startup
			if st != nil {
				cp := *st
				switch cp.Mode {
				case model.StartupURLs:
					cp.URLs = []string{url}
				case model.StartupNewTab:
					cp.NewTabURL = url
				}
				st = &cp
			}

			p := &model.Profile{
				ID: "startup", Name: "startup", Kind: model.KindDaily,
				ProfileDir: t.TempDir(), Startup: st,
			}
			args, err := launcher.BuildArgs(p, nil, "", nil)
			if err != nil {
				t.Fatalf("BuildArgs 失败: %v", err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
			defer cancel()
			runArgs := append(args,
				"--no-first-run", "--no-default-browser-check",
				"--disable-backgrounding-occluded-windows",
				"--window-size=900,600",
			)
			cmd := exec.CommandContext(ctx, k.ExecPath, runArgs...)
			setProcessGroup(cmd)
			if err := cmd.Start(); err != nil {
				t.Fatalf("启动内核失败: %v", err)
			}
			defer reapKernel(cmd)

			select {
			case path := <-hit:
				if !c.wantHit {
					t.Errorf("未配置启动页却打开了 %q", path)
					return
				}
				t.Logf("生效：浏览器打开了 %s", url)
			case <-time.After(25 * time.Second):
				if c.wantHit {
					t.Error("25 秒内未收到请求，配置未生效")
					return
				}
				t.Log("未打开任何页面，符合预期")
			}
		})
	}
}

// startHitServer 起一个记录被访问路径的本地服务，返回其 URL 与关闭函数。
func startHitServer(t *testing.T, hit chan<- string) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("启动本地服务失败: %v", err)
	}
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case hit <- r.URL.Path:
			default:
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte("<html><body>ok</body></html>"))
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() { _ = srv.Serve(ln) }()

	return "http://" + ln.Addr().String() + "/target", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
}
