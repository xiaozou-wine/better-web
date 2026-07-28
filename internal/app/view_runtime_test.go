package app

import (
	"context"
	"strings"
	"testing"

	"better-web/internal/model"
)

// 未运行的 profile 不应带运行时字段。
//
// 出口画像与警告描述的是"本次会话的事实"，profile 停止后它们就失效了。
// 继续显示会让用户以为看到的是当前状态。
func TestStoppedProfileHasNoRuntimeFields(t *testing.T) {
	s, _ := newTestService(t)
	v, err := s.CreateProfile(context.Background(), CreateRequest{
		Name: "未运行", Kind: model.KindFingerprint,
		Proxy: &model.Proxy{
			Scheme: model.ProxySOCKS5, Host: "127.0.0.1", Port: 1080,
		},
	})
	if err != nil {
		t.Fatalf("CreateProfile 失败: %v", err)
	}
	if v.Exit != nil {
		t.Errorf("未运行的 profile 带了出口信息: %+v", v.Exit)
	}
	if len(v.Warnings) != 0 {
		t.Errorf("未运行的 profile 带了运行时警告: %v", v.Warnings)
	}
}

// 运行时警告必须出现在发往前端的 JSON 中。
//
// 机房 IP 一类的警告不阻断启动，全靠界面呈现让用户知晓。若字段没进 JSON，
// 该警告等于不存在——后端算得再准也没有意义。本测试守住这条链路。
func TestProfileViewJSONCarriesRuntimeFields(t *testing.T) {
	s, _ := newTestService(t)
	v, err := s.CreateProfile(context.Background(), CreateRequest{Name: "带字段", Kind: model.KindFingerprint})
	if err != nil {
		t.Fatalf("CreateProfile 失败: %v", err)
	}

	// 直接构造带运行时数据的视图，验证序列化契约而不依赖真实会话。
	v.Warnings = []string{"出口是机房 IP（Example Hosting），多账号场景下极易被识别"}
	got := dumpJSON(t, v)

	if !strings.Contains(got, `"warnings"`) {
		t.Errorf("JSON 中缺少 warnings 字段，前端无法呈现运行时警告: %s", got)
	}
	if !strings.Contains(got, "机房 IP") {
		t.Errorf("JSON 中缺少警告内容: %s", got)
	}
}

// 视图仍然不得泄漏代理密码，新增运行时字段不能破坏这条约束。
func TestRuntimeFieldsDoNotLeakPassword(t *testing.T) {
	s, _ := newTestService(t)
	const secret = "runtime-leak-check-7a3f"
	v, err := s.CreateProfile(context.Background(), CreateRequest{
		Name: "带密码", Kind: model.KindFingerprint,
		Proxy: &model.Proxy{
			Scheme: model.ProxySOCKS5, Host: "127.0.0.1", Port: 1080,
			Username: "u", Password: secret,
		},
	})
	if err != nil {
		t.Fatalf("CreateProfile 失败: %v", err)
	}
	if got := dumpJSON(t, v); strings.Contains(got, secret) {
		t.Error("视图 JSON 中泄漏了代理密码")
	}
}
