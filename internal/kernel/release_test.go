package kernel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
)

// fakeReleasesJSON 构造一份 GitHub releases API 风格的响应，
// 资产名按当前平台生成，使测试在各平台都有意义。
func fakeReleasesJSON(t *testing.T) string {
	t.Helper()
	suffix, err := assetSuffix()
	if err != nil {
		t.Skipf("当前平台 %s 不支持自动安装: %v", runtime.GOOS, err)
	}
	return `[
	  {"tag_name":"148.0.7778.215","draft":false,"prerelease":false,"assets":[
	    {"name":"other-platform.tar.gz","size":1,"browser_download_url":"https://example.test/other"},
	    {"name":"ungoogled-chromium_148.0.7778.215-1.1` + suffix + `","size":189767686,
	     "browser_download_url":"https://example.test/win148"}
	  ]},
	  {"tag_name":"149.0.0.1-rc","draft":false,"prerelease":true,"assets":[
	    {"name":"pre` + suffix + `","size":2,"browser_download_url":"https://example.test/pre"}
	  ]},
	  {"tag_name":"147.0.0.0","draft":true,"prerelease":false,"assets":[
	    {"name":"draft` + suffix + `","size":3,"browser_download_url":"https://example.test/draft"}
	  ]},
	  {"tag_name":"146.0.0.0","draft":false,"prerelease":false,"assets":[
	    {"name":"no-matching-asset.txt","size":4,"browser_download_url":"https://example.test/none"}
	  ]},
	  {"tag_name":"144.0.7559.132","draft":false,"prerelease":false,"assets":[
	    {"name":"ungoogled-chromium_144.0.7559.132-1.1` + suffix + `","size":180000000,
	     "browser_download_url":"https://example.test/win144"}
	  ]}
	]`
}

func TestListReleasesFiltersUnusable(t *testing.T) {
	body := fakeReleasesJSON(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Errorf("Accept 头 = %q, 期望声明 API 版本", got)
		}
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	f := &Fetcher{Client: srv.Client(), APIURL: srv.URL}
	list, err := f.ListReleases(context.Background())
	if err != nil {
		t.Fatalf("ListReleases 失败: %v", err)
	}

	// 预发布、草稿、无当前平台资产的版本都应被剔除。
	if len(list) != 2 {
		t.Fatalf("可用版本数 = %d, 期望 2: %+v", len(list), list)
	}
	if list[0].Version != "148.0.7778.215" || list[1].Version != "144.0.7559.132" {
		t.Errorf("版本列表 = %q, %q", list[0].Version, list[1].Version)
	}
	if list[0].DownloadURL != "https://example.test/win148" {
		t.Errorf("下载地址 = %q, 未选中当前平台资产", list[0].DownloadURL)
	}
	if list[0].Size != 189767686 {
		t.Errorf("资产大小 = %d", list[0].Size)
	}
}

func TestLatestReleaseReturnsFirst(t *testing.T) {
	body := fakeReleasesJSON(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	rel, err := (&Fetcher{Client: srv.Client(), APIURL: srv.URL}).LatestRelease(context.Background())
	if err != nil {
		t.Fatalf("LatestRelease 失败: %v", err)
	}
	if rel.Version != "148.0.7778.215" {
		t.Errorf("最新版本 = %q", rel.Version)
	}
}

func TestListReleasesErrorsWhenNoAssetForPlatform(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"tag_name":"148.0.0.1","assets":[
		  {"name":"nothing-useful.txt","size":1,"browser_download_url":"https://example.test/x"}]}]`))
	}))
	defer srv.Close()

	_, err := (&Fetcher{Client: srv.Client(), APIURL: srv.URL}).ListReleases(context.Background())
	if err == nil {
		t.Fatal("没有可用资产时期望报错")
	}
	if !strings.Contains(err.Error(), runtime.GOOS) {
		t.Errorf("错误信息应指出平台, 实际: %v", err)
	}
}

func TestListReleasesReportsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 触发 API 限流是最常见的失败，必须报出明确原因。
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	_, err := (&Fetcher{Client: srv.Client(), APIURL: srv.URL}).ListReleases(context.Background())
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("期望报出 HTTP 403, 实际 %v", err)
	}
}

func TestListReleasesReportsMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{not json`))
	}))
	defer srv.Close()

	if _, err := (&Fetcher{Client: srv.Client(), APIURL: srv.URL}).ListReleases(context.Background()); err == nil {
		t.Error("响应格式错误时期望报错")
	}
}
