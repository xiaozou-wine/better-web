//go:build windows

package urlhandler

import (
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// TestRegisterUnregisterRoundTrip 实测注册与注销的完整往返。
//
// 真写 HKCU 而非打桩：这个包的全部价值就在于写对注册表位置，
// 打桩测的是"我以为的位置"，测不出真正的问题。写的是当前用户的键、
// 不需要提权，且 t.Cleanup 保证收尾——但仍会短暂影响本机的关联注册，
// 因此只在本地跑，不适合放进无人值守的 CI。
func TestRegisterUnregisterRoundTrip(t *testing.T) {
	// 先记下当前状态。测试机上可能已经注册过（用户自己开了这个功能），
	// 那时测完必须还原，否则会把用户的配置抹掉。
	before, err := Query()
	if err != nil {
		t.Fatalf("Query 失败: %v", err)
	}
	t.Cleanup(func() {
		if before.Registered {
			if err := Register(""); err != nil {
				t.Errorf("还原原有注册状态失败: %v", err)
			}
			return
		}
		if err := Unregister(); err != nil {
			t.Errorf("清理注册信息失败: %v", err)
		}
	})

	const fakeExe = `C:\Program Files\better-web\better-web.exe`
	if err := Register(fakeExe); err != nil {
		t.Fatalf("Register 失败: %v", err)
	}

	st, err := Query()
	if err != nil {
		t.Fatalf("注册后 Query 失败: %v", err)
	}
	if !st.Registered {
		t.Error("Register 后 Registered 应为 true")
	}

	// 逐个核对写入的键。少任何一个都会让系统的默认应用列表里看不到它，
	// 而 Register 本身不报错——这正是要靠测试兜住的失败方式。
	assertValue(t, classesKey+`\shell\open\command`, "", CommandLineFor(fakeExe))
	assertValue(t, capabilitiesKey+`\UrlAssociations`, "http", ProgID)
	assertValue(t, capabilitiesKey+`\UrlAssociations`, "https", ProgID)
	assertValue(t, capabilitiesKey, "ApplicationName", appName)
	assertValue(t, registeredKey, appName, capabilitiesKey)

	// 命令行里的 exe 路径必须带引号，否则含空格的路径会被截断。
	cmd := readValue(t, classesKey+`\shell\open\command`, "")
	if !strings.HasPrefix(cmd, `"`) {
		t.Errorf("命令行里的路径应加引号: %q", cmd)
	}

	if err := Unregister(); err != nil {
		t.Fatalf("Unregister 失败: %v", err)
	}
	st, err = Query()
	if err != nil {
		t.Fatalf("注销后 Query 失败: %v", err)
	}
	if st.Registered {
		t.Error("Unregister 后 Registered 应为 false")
	}
	// 键必须真的删掉，只删 RegisteredApplications 会留下半份垃圾。
	if _, err := registry.OpenKey(
		registry.CURRENT_USER, classesKey, registry.QUERY_VALUE,
	); err == nil {
		t.Errorf("注销后 HKCU\\%s 仍存在", classesKey)
	}
}

// TestUnregisterIsIdempotent 钉住重复注销不报错。
//
// 注销要能对"注册到一半就失败"的状态收拾干净，因此不存在的键不算错误。
func TestUnregisterIsIdempotent(t *testing.T) {
	before, err := Query()
	if err != nil {
		t.Fatalf("Query 失败: %v", err)
	}
	if before.Registered {
		t.Skip("本机已注册，跳过以免影响用户配置")
	}
	for i := range 2 {
		if err := Unregister(); err != nil {
			t.Errorf("第 %d 次 Unregister 报错: %v", i+1, err)
		}
	}
}

func readValue(t *testing.T, path, name string) string {
	t.Helper()
	v, err := getStringValue(path, name)
	if err != nil {
		t.Fatalf("读取 HKCU\\%s 的 %q 失败: %v", path, name, err)
	}
	return v
}

func assertValue(t *testing.T, path, name, want string) {
	t.Helper()
	if got := readValue(t, path, name); got != want {
		t.Errorf("HKCU\\%s 的 %q = %q, 期望 %q", path, name, got, want)
	}
}
