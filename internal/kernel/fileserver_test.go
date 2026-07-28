package kernel

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// newFileServer 起一个提供指定文件的测试服务器，返回其 URL。
// 用于让真实包走与线上一致的 HTTP 下载路径。
func newFileServer(t *testing.T, path string, size int64) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, err := os.Open(path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer func() { _ = f.Close() }()
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Length", fmt.Sprint(size))
		http.ServeContent(w, r, "kernel.zip", statModTime(f), f)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func statModTime(f *os.File) time.Time {
	if fi, err := f.Stat(); err == nil {
		return fi.ModTime()
	}
	return time.Time{}
}
