// Command seturlhandler 在命令行配置链接接管的目标 profile。
//
// 与界面的分工：界面是常规入口，这个工具用于排障与自动化核对——
// 界面里的下拉框看不出实际存进库里的是什么。
package main

import (
	"flag"
	"fmt"
	"os"

	"better-web/internal/app"
)

func main() {
	profileName := flag.String("profile", "", "接管链接的 profile 名称，留空表示只查看当前配置")
	incognito := flag.Bool("incognito", false, "用无痕窗口打开接管的链接")
	clear := flag.Bool("clear", false, "关闭链接接管")
	flag.Parse()

	paths, err := app.DefaultPaths()
	if err != nil {
		fmt.Fprintln(os.Stderr, "定位数据目录失败:", err)
		os.Exit(1)
	}
	svc, err := app.New(paths)
	if err != nil {
		fmt.Fprintln(os.Stderr, "初始化失败:", err)
		os.Exit(1)
	}
	defer func() { _ = svc.Close() }()

	switch {
	case *clear:
		if err := svc.SetURLHandler("", false); err != nil {
			fmt.Fprintln(os.Stderr, "关闭失败:", err)
			os.Exit(1)
		}
		fmt.Println("已关闭链接接管")
	case *profileName != "":
		id, err := findProfileID(svc, *profileName)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := svc.SetURLHandler(id, *incognito); err != nil {
			fmt.Fprintln(os.Stderr, "设置失败:", err)
			os.Exit(1)
		}
	}

	v, err := svc.URLHandler()
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取状态失败:", err)
		os.Exit(1)
	}
	fmt.Printf("已注册为候选浏览器: %v\n", v.Registered)
	fmt.Printf("已是系统默认浏览器: %v\n", v.IsDefault)
	if v.ProfileID == "" {
		fmt.Println("接管目标: 未设置")
		return
	}
	name := v.ProfileName
	if name == "" {
		name = "（该 profile 已被删除）"
	}
	fmt.Printf("接管目标: %s [%s]\n", name, v.ProfileID)
	fmt.Printf("无痕窗口: %v\n", v.Incognito)
}

func findProfileID(svc *app.Service, name string) (string, error) {
	list, err := svc.ListProfiles()
	if err != nil {
		return "", fmt.Errorf("读取 profile 列表失败: %w", err)
	}
	for _, p := range list {
		if p.Name == name {
			return p.ID, nil
		}
	}
	names := make([]string, 0, len(list))
	for _, p := range list {
		names = append(names, p.Name)
	}
	return "", fmt.Errorf("找不到名为 %q 的 profile。现有: %v", name, names)
}
