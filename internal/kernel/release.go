package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"time"
)

// releaseAPI 是查询可用内核版本的接口。
// 用 api.github.com 而非解析 releases 页面：页面结构随时会变，API 有稳定契约。
const releaseAPI = "https://api.github.com/repos/adryfish/fingerprint-chromium/releases"

// apiTimeout 是查询版本列表的超时。
const apiTimeout = 30 * time.Second

// Release 是一个可安装的内核发行版。
type Release struct {
	// Version 是 Chromium 完整版本号，取自 release 的 tag。
	Version string `json:"version"`
	// DownloadURL 是当前平台对应资产的下载地址。
	DownloadURL string `json:"downloadUrl"`
	// Size 是资产字节数，供界面显示下载进度总量。
	Size int64 `json:"size"`
	// AssetName 是资产文件名，用于判断压缩格式。
	AssetName string `json:"assetName"`
}

// assetSuffix 返回当前平台所需资产的文件名后缀。
//
// 只支持 Windows 的 zip 与 Linux 的 tar.xz 之外，macOS 的 dmg 无法在
// 非 macOS 上挂载解包，故仅在 darwin 上声明支持。
func assetSuffix() (string, error) {
	switch runtime.GOOS {
	case "windows":
		return "_windows_x64.zip", nil
	case "linux":
		return "_x86_64_linux.tar.xz", nil
	case "darwin":
		return "_macos.dmg", nil
	}
	return "", fmt.Errorf("暂不支持在 %s 上自动安装内核", runtime.GOOS)
}

// ghRelease 是 GitHub releases API 响应中我们关心的字段。
type ghRelease struct {
	TagName    string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// Fetcher 查询可安装的内核版本。
type Fetcher struct {
	// Client 用于访问 API。留空时使用带超时的默认客户端。
	Client *http.Client
	// APIURL 可覆盖 API 地址，供测试注入。留空时使用 releaseAPI。
	APIURL string
}

func (f *Fetcher) client() *http.Client {
	if f.Client != nil {
		return f.Client
	}
	return &http.Client{Timeout: apiTimeout}
}

func (f *Fetcher) apiURL() string {
	if f.APIURL != "" {
		return f.APIURL
	}
	return releaseAPI
}

// ListReleases 返回可安装的内核版本，按发布顺序（新版在前）。
// 跳过草稿、预发布，以及没有当前平台资产的版本。
func (f *Fetcher) ListReleases(ctx context.Context) ([]Release, error) {
	suffix, err := assetSuffix()
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.apiURL(), nil)
	if err != nil {
		return nil, err
	}
	// 声明 API 版本，避免响应结构随默认版本变动。
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := f.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("查询内核版本列表失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("查询内核版本列表失败: HTTP %d", resp.StatusCode)
	}

	var raw []ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("解析内核版本列表失败: %w", err)
	}

	var out []Release
	for _, r := range raw {
		if r.Draft || r.Prerelease || r.TagName == "" {
			continue
		}
		for _, a := range r.Assets {
			if !strings.HasSuffix(a.Name, suffix) {
				continue
			}
			out = append(out, Release{
				Version:     r.TagName,
				DownloadURL: a.URL,
				Size:        a.Size,
				AssetName:   a.Name,
			})
			break
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("没有找到适用于 %s 的内核资产", runtime.GOOS)
	}
	return out, nil
}

// LatestRelease 返回最新可安装的内核版本。
func (f *Fetcher) LatestRelease(ctx context.Context) (Release, error) {
	list, err := f.ListReleases(ctx)
	if err != nil {
		return Release{}, err
	}
	return list[0], nil
}
