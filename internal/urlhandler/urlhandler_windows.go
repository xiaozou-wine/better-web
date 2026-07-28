//go:build windows

package urlhandler

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// 注册表写入 HKCU 而非 HKLM。
//
// 按 MS 文档，安装完成后再改机器级关联会失败——UAC 下管理员进程也不该
// 悄悄改全机器的默认程序。per-user 才是正确层级，且不需要提权。
//
// 路径拆成常量而非内联：Register 与 Unregister 必须操作同一组键，
// 写死两遍迟早漂移，而漂移的表现是"注销后仍留着半份注册信息"。
const (
	classesKey      = `SOFTWARE\Classes\` + ProgID
	capabilitiesKey = `SOFTWARE\` + appName + `\Capabilities`
	registeredKey   = `SOFTWARE\RegisteredApplications`
	// appRootKey 是 Unregister 要连带删掉的父键。
	appRootKey = `SOFTWARE\` + appName
)

// userChoiceKey 是系统记录用户选择的默认 https 处理程序的位置。
//
// 只读不写：这个键的 Hash 值算法未公开，写入会被 UCPD 拦截，
// 强行改还会让系统认为关联已损坏而回退到 Edge。
const userChoiceKey = `SOFTWARE\Microsoft\Windows\Shell\Associations\UrlAssociations\https\UserChoice`

// Register 把 better-web 写入注册表，使其出现在系统默认应用的候选列表里。
//
// exePath 留空时用当前可执行文件。注册后不代表已生效——用户还需在
// "设置 → 默认应用"里手动选中，见包注释。
func Register(exePath string) error {
	exe := strings.TrimSpace(exePath)
	if exe == "" {
		p, err := os.Executable()
		if err != nil {
			return fmt.Errorf("定位当前程序路径失败: %w", err)
		}
		exe = p
	}
	command := CommandLineFor(exe)

	if err := writeClassKeys(exe, command); err != nil {
		return err
	}
	if err := writeCapabilities(); err != nil {
		return err
	}
	if err := writeRegisteredApplications(); err != nil {
		return err
	}

	// 通知系统关联已变化。不调它的话新注册的应用要等很久（或到下次登录）
	// 才出现在设置页里，用户会以为注册失败。
	notifyAssocChanged()
	return nil
}

// writeClassKeys 写 ProgID 的 shell\open\command 与图标。
func writeClassKeys(exe, command string) error {
	if err := setDefaultString(classesKey, appDescription); err != nil {
		return err
	}
	// URL Protocol 空值标记该 ProgID 可处理 URL 而非文件扩展名。
	if err := setStringValue(classesKey, "URL Protocol", ""); err != nil {
		return err
	}
	if err := setDefaultString(classesKey+`\DefaultIcon`, `"`+exe+`",0`); err != nil {
		return err
	}
	return setDefaultString(classesKey+`\shell\open\command`, command)
}

// writeCapabilities 写 Default Programs 要求的能力声明。
func writeCapabilities() error {
	if err := setStringValue(capabilitiesKey, "ApplicationName", appName); err != nil {
		return err
	}
	if err := setStringValue(capabilitiesKey, "ApplicationDescription", appDescription); err != nil {
		return err
	}
	// http 与 https 都要声明。只声明其一时系统不会把它当浏览器候选，
	// 而"新浏览器已注册"的系统通知也不会出现（条件是同时接管两个协议）。
	for _, scheme := range []string{"http", "https"} {
		if err := setStringValue(capabilitiesKey+`\UrlAssociations`, scheme, ProgID); err != nil {
			return err
		}
	}
	return nil
}

// writeRegisteredApplications 告诉系统去哪里找上面那份能力声明。
func writeRegisteredApplications() error {
	return setStringValue(registeredKey, appName, capabilitiesKey)
}

// Unregister 清除注册信息。
//
// 已经不存在的键不算错误：注销要能对"注册过一半就失败"的状态收拾干净，
// 因此逐个删、忽略 ErrNotExist，最后才报告真正的失败。
func Unregister() error {
	var errs []error

	// RegisteredApplications 里只删自己那个值，整个键是系统共用的。
	if err := deleteValue(registeredKey, appName); err != nil {
		errs = append(errs, err)
	}
	// 子键必须自底向上删：RegDeleteKey 删不掉还有子键的键。
	for _, k := range []string{
		classesKey + `\shell\open\command`,
		classesKey + `\shell\open`,
		classesKey + `\shell`,
		classesKey + `\DefaultIcon`,
		classesKey,
		capabilitiesKey + `\UrlAssociations`,
		capabilitiesKey,
		appRootKey,
	} {
		if err := deleteKey(k); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("清除注册信息时出错: %w", errors.Join(errs...))
	}
	notifyAssocChanged()
	return nil
}

// Query 返回当前注册状态。
//
// Registered 以 RegisteredApplications 里的值为准而非只看 Classes 键存在：
// 前者才是系统据以枚举候选应用的入口，少了它其余键写得再全也不会出现在设置里。
func Query() (Status, error) {
	var st Status

	if v, err := getStringValue(registeredKey, appName); err == nil && v == capabilitiesKey {
		st.Registered = true
	} else if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return Status{}, fmt.Errorf("读取注册状态失败: %w", err)
	}

	progID, err := getStringValue(userChoiceKey, "ProgId")
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return Status{}, fmt.Errorf("读取系统默认浏览器失败: %w", err)
	}
	st.IsDefault = progID == ProgID
	return st, nil
}

// Supported 报告当前平台是否实现了默认浏览器注册。
func Supported() bool { return true }

// OpenSettings 打开系统的默认应用设置页。
//
// 用 rundll32 调 ShellExec_RunDLL 而非 cmd /c start：后者要拼 shell 命令行，
// 而 ms-settings: 这类 URI 交给 cmd 处理时对特殊字符的转义规则很微妙。
// 这里的 URI 是编译期常量，但仍按不拼 shell 的方式走——习惯比个案重要。
func OpenSettings() error {
	cmd := exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", settingsURI)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("打开系统默认应用设置页失败: %w", err)
	}
	// 不等它退出：rundll32 交给 shell 后就返回，而设置页是另一个进程。
	go func() { _ = cmd.Wait() }()
	return nil
}

// createNoWindow 对应 Windows 的 CREATE_NO_WINDOW 进程创建标志。
//
// syscall 包没有导出这个常量，按 Microsoft 文档的定义写在此处：
// https://learn.microsoft.com/windows/win32/procthread/process-creation-flags
//
// better-web 是 GUI 子系统程序（无控制台），不加这个标志时 Windows 会为
// rundll32 新建一个控制台——表现就是点按钮时黑框闪一下。
const createNoWindow = 0x08000000

// notifyAssocChanged 通知 shell 关联已变化。
//
// 失败时静默：这只是让设置页尽快刷新的优化，注册本身已经写进注册表了。
// 为此报错会让一次成功的注册看起来像失败。
func notifyAssocChanged() {
	const (
		shcneAssocChanged = 0x08000000
		shcnfDword        = 0x0003
		shcnfFlush        = 0x1000
	)
	shell32 := windows.NewLazySystemDLL("shell32.dll")
	proc := shell32.NewProc("SHChangeNotify")
	if err := proc.Find(); err != nil {
		return
	}
	_, _, _ = proc.Call(
		uintptr(shcneAssocChanged), uintptr(shcnfDword|shcnfFlush), 0, 0)
}

// setDefaultString 写某个键的默认值（无名值）。
func setDefaultString(path, value string) error {
	return setStringValue(path, "", value)
}

// setStringValue 写一个字符串值，键不存在时创建。
func setStringValue(path, name, value string) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("创建注册表键 HKCU\\%s 失败: %w", path, err)
	}
	defer func() { _ = key.Close() }()

	if err := key.SetStringValue(name, value); err != nil {
		return fmt.Errorf("写入 HKCU\\%s 的 %q 失败: %w", path, name, err)
	}
	return nil
}

// getStringValue 读一个字符串值。键或值不存在时返回 registry.ErrNotExist。
func getStringValue(path, name string) (string, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer func() { _ = key.Close() }()

	v, _, err := key.GetStringValue(name)
	return v, err
}

// deleteValue 删除一个值。不存在时视为已完成。
func deleteValue(path, name string) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, path, registry.SET_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("打开 HKCU\\%s 失败: %w", path, err)
	}
	defer func() { _ = key.Close() }()

	if err := key.DeleteValue(name); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("删除 HKCU\\%s 的 %q 失败: %w", path, name, err)
	}
	return nil
}

// deleteKey 删除一个键。不存在时视为已完成。
func deleteKey(path string) error {
	if err := registry.DeleteKey(registry.CURRENT_USER, path); err != nil &&
		!errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("删除注册表键 HKCU\\%s 失败: %w", path, err)
	}
	return nil
}
