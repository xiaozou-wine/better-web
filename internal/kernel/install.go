package kernel

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// maxArchiveSize 是可接受的压缩包上限。内核包约 200MB，1GB 足够留余量，
// 同时避免异常响应把磁盘写满。
const maxArchiveSize = 1 << 30

// Progress 是下载进度回调。total 为 0 表示服务端未提供长度。
type Progress func(downloaded, total int64)

// Installer 下载并安装内核。
type Installer struct {
	store *Store
	// Client 用于下载。留空时使用无整体超时的客户端：内核包约 200MB，
	// 固定超时会在慢速网络上误杀，改由 context 控制取消。
	Client *http.Client
}

// NewInstaller 构造安装器。
func NewInstaller(store *Store) *Installer {
	return &Installer{store: store}
}

func (i *Installer) client() *http.Client {
	if i.Client != nil {
		return i.Client
	}
	return &http.Client{}
}

// Install 下载指定发行版并安装到内核目录。
//
// 安装是原子的：先解压到临时目录，校验出可执行文件后再整体改名到目标位置。
// 中途失败不会留下半装的版本——那会被 List 识别成一个用不了的内核。
func (i *Installer) Install(ctx context.Context, rel Release, onProgress Progress) (Kernel, error) {
	if rel.Version == "" {
		return Kernel{}, errors.New("发行版缺少版本号")
	}
	if rel.DownloadURL == "" {
		return Kernel{}, errors.New("发行版缺少下载地址")
	}
	// 版本号会作为目录名，必须挡住路径穿越。
	if !safeVersion(rel.Version) {
		return Kernel{}, fmt.Errorf("版本号 %q 含非法字符", rel.Version)
	}

	target := filepath.Join(i.store.root, rel.Version)
	if _, err := os.Stat(filepath.Join(target, execName())); err == nil {
		return Kernel{}, fmt.Errorf("版本 %s 已安装", rel.Version)
	}
	if err := os.MkdirAll(i.store.root, 0o700); err != nil {
		return Kernel{}, fmt.Errorf("创建内核目录失败: %w", err)
	}

	// 临时目录与目标同盘，保证最后的改名是原子操作而非跨卷拷贝。
	staging, err := os.MkdirTemp(i.store.root, ".staging-"+rel.Version+"-*")
	if err != nil {
		return Kernel{}, fmt.Errorf("创建临时目录失败: %w", err)
	}
	// 失败时清理临时目录；成功路径下 staging 已被改名，RemoveAll 无副作用。
	defer func() { _ = os.RemoveAll(staging) }()

	archive := filepath.Join(staging, "kernel-archive")
	if err := i.download(ctx, rel, archive, onProgress); err != nil {
		return Kernel{}, err
	}

	extractDir := filepath.Join(staging, "extract")
	if err := extractZip(archive, extractDir); err != nil {
		return Kernel{}, err
	}
	// 压缩包已解完，尽早释放这 200MB。
	if err := os.Remove(archive); err != nil {
		return Kernel{}, fmt.Errorf("清理压缩包失败: %w", err)
	}

	// 内核包通常带一层顶层目录，需要定位真正含可执行文件的那一层。
	root, err := findExecRoot(extractDir)
	if err != nil {
		return Kernel{}, err
	}

	if err := os.Rename(root, target); err != nil {
		return Kernel{}, fmt.Errorf("安装内核到 %s 失败: %w", target, err)
	}
	return Kernel{Version: rel.Version, ExecPath: filepath.Join(target, execName())}, nil
}

// safeVersion 报告版本号是否可安全用作目录名。
// 只允许数字与点，挡住 ..、路径分隔符与盘符。
func safeVersion(v string) bool {
	if v == "" || strings.Contains(v, "..") {
		return false
	}
	for _, r := range v {
		if (r < '0' || r > '9') && r != '.' {
			return false
		}
	}
	return true
}

func (i *Installer) download(ctx context.Context, rel Release, dst string, onProgress Progress) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rel.DownloadURL, nil)
	if err != nil {
		return err
	}
	resp, err := i.client().Do(req)
	if err != nil {
		return fmt.Errorf("下载内核失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载内核失败: HTTP %d", resp.StatusCode)
	}

	total := resp.ContentLength
	if total <= 0 {
		total = rel.Size
	}
	if total > maxArchiveSize {
		return fmt.Errorf("内核包大小 %d 字节超过上限 %d", total, maxArchiveSize)
	}

	f, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("创建下载文件失败: %w", err)
	}
	defer func() { _ = f.Close() }()

	// 限制读取量，防止服务端谎报长度后写满磁盘。
	src := io.Reader(io.LimitReader(resp.Body, maxArchiveSize+1))
	if onProgress != nil {
		src = &progressReader{r: src, total: total, cb: onProgress}
	}

	written, err := io.Copy(f, src)
	if err != nil {
		return fmt.Errorf("下载内核失败: %w", err)
	}
	if written > maxArchiveSize {
		return fmt.Errorf("内核包超过 %d 字节上限", maxArchiveSize)
	}
	// 长度已知时校验完整性：截断的包解压会报出难以定位的错误。
	if total > 0 && written != total {
		return fmt.Errorf("下载不完整: 收到 %d 字节，期望 %d", written, total)
	}
	return f.Sync()
}

// progressReader 在读取过程中上报进度。
type progressReader struct {
	r     io.Reader
	n     int64
	total int64
	cb    Progress
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.n += int64(n)
		p.cb(p.n, p.total)
	}
	return n, err
}

// extractZip 把 zip 解压到 dst。
//
// 逐条校验目标路径必须落在 dst 之内：压缩包里的 "../" 条目可以覆写
// 目录之外的任意文件（Zip Slip）。内核包来自第三方，必须当不可信输入处理。
func extractZip(src, dst string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return fmt.Errorf("打开内核压缩包失败: %w", err)
	}
	defer func() { _ = r.Close() }()

	if err := os.MkdirAll(dst, 0o700); err != nil {
		return fmt.Errorf("创建解压目录失败: %w", err)
	}
	base, err := filepath.Abs(dst)
	if err != nil {
		return err
	}

	for _, f := range r.File {
		path, err := safeJoin(base, f.Name)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(path, 0o700); err != nil {
				return fmt.Errorf("创建目录失败: %w", err)
			}
			continue
		}
		// 跳过符号链接：它同样能指向目录之外，而内核包不需要它。
		if f.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if err := writeZipEntry(f, path); err != nil {
			return err
		}
	}
	return nil
}

// safeJoin 把压缩包内的相对路径拼到 base 下，并确保结果不逃出 base。
func safeJoin(base, name string) (string, error) {
	// zip 规范用正斜杠，Windows 上需转成本地分隔符再判断。
	clean := filepath.Clean(filepath.FromSlash(name))
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		return "", fmt.Errorf("压缩包内含非法路径 %q，已中止解压", name)
	}
	path := filepath.Join(base, clean)
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("压缩包内含越界路径 %q，已中止解压", name)
	}
	return path, nil
}

func writeZipEntry(f *zip.File, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("读取压缩包条目 %s 失败: %w", f.Name, err)
	}
	defer func() { _ = rc.Close() }()

	// 保留可执行位：Linux/macOS 下丢了权限内核就起不来。
	mode := f.Mode().Perm() | 0o600
	out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("创建文件 %s 失败: %w", path, err)
	}
	defer func() { _ = out.Close() }()

	// 限制单条目大小，防御解压炸弹。
	if _, err := io.Copy(out, io.LimitReader(rc, maxArchiveSize)); err != nil {
		return fmt.Errorf("写入文件 %s 失败: %w", path, err)
	}
	return out.Close()
}

// findExecRoot 在解压结果中定位含内核可执行文件的目录。
// 内核包通常带一层顶层目录，因此需要向下找一层。
func findExecRoot(dir string) (string, error) {
	if _, err := os.Stat(filepath.Join(dir, execName())); err == nil {
		return dir, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("读取解压结果失败: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sub := filepath.Join(dir, e.Name())
		if _, err := os.Stat(filepath.Join(sub, execName())); err == nil {
			return sub, nil
		}
	}
	return "", fmt.Errorf("解压结果中找不到 %s，压缩包结构可能已变更", execName())
}
