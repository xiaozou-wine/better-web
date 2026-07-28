package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"better-web/internal/model"
)

// 代理配置无效时必须明确失败，不能返回 OK。
func TestCheckProxyFailsOnInvalidConfig(t *testing.T) {
	s, _ := newTestService(t)
	cases := map[string]*model.Proxy{
		"未配置":    nil,
		"缺 host": {Scheme: model.ProxySOCKS5, Port: 1080},
		"端口越界":   {Scheme: model.ProxySOCKS5, Host: "127.0.0.1", Port: 70000},
		"协议不支持":  {Scheme: "ftp", Host: "127.0.0.1", Port: 1080},
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			got := s.CheckProxy(context.Background(), p)
			if got.OK {
				t.Error("无效配置却返回 OK")
			}
			if got.Err == "" {
				t.Error("失败时未给出原因")
			}
		})
	}
}

// 上游不可达时必须失败，且不能把本机出口当成代理出口报告——
// 那会让用户以为代理生效了，实际在用真实 IP。
func TestCheckProxyFailsWhenUpstreamUnreachable(t *testing.T) {
	s, _ := newTestService(t)
	// 192.0.2.0/24 是 TEST-NET-1 保留段，连接会被丢弃而非拒绝，
	// 因此必然耗满超时。缩短超时避免每条用例都等 12 秒。
	s.CheckProxyTimeoutOverride = 2 * time.Second
	p := &model.Proxy{Scheme: model.ProxySOCKS5, Host: "192.0.2.1", Port: 1080}

	got := s.CheckProxy(context.Background(), p)
	if got.OK {
		t.Fatal("上游不可达却返回 OK，可能查到了本机出口")
	}
	if got.Exit != nil {
		t.Error("失败时不应返回出口信息")
	}
	if got.ElapsedMs < 0 {
		t.Error("耗时不应为负")
	}
}

// 失败原因应能指向代理环节，便于用户判断是配置错还是网络问题。
func TestCheckProxyErrorMentionsProxy(t *testing.T) {
	s, _ := newTestService(t)
	s.CheckProxyTimeoutOverride = 2 * time.Second
	p := &model.Proxy{Scheme: model.ProxySOCKS5, Host: "192.0.2.1", Port: 1080}
	got := s.CheckProxy(context.Background(), p)
	if got.OK {
		t.Skip("意外连通，跳过")
	}
	if !strings.Contains(got.Err, "代理") {
		t.Errorf("错误信息未提及代理，用户难以定位: %q", got.Err)
	}
}

// context 取消后应尽快返回，不能挂住界面。
func TestCheckProxyRespectsCanceledContext(t *testing.T) {
	s, _ := newTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	p := &model.Proxy{Scheme: model.ProxySOCKS5, Host: "192.0.2.1", Port: 1080}
	got := s.CheckProxy(ctx, p)
	if got.OK {
		t.Error("context 已取消却返回 OK")
	}
}
