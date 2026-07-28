package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"better-web/internal/model"
	"better-web/internal/secret"
)

// 决定性断言：数据库文件里不得出现代理密码的明文。
//
// 这是本包加密改动要解决的核心问题——此前密码明文存在 proxy 列的 JSON 中，
// 任何能读到该文件的程序都能直接拿到凭据。
func TestDatabaseFileContainsNoPlaintextPassword(t *testing.T) {
	if !secret.Available() {
		t.Skip("当前平台无系统级加密，明文存储是已知且已标注的行为")
	}
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	const sentinel = "SENTINEL-PASSWORD-9f3a7c21"
	err = s.Save(&model.Profile{
		ID: "p1", Name: "加密测试", Kind: model.KindFingerprint,
		Seed: 123, ProfileDir: dir,
		Proxy: &model.Proxy{
			Scheme: model.ProxySOCKS5, Host: "gate.example.com", Port: 7000,
			Username: "user1", Password: sentinel,
		},
	})
	if err != nil {
		t.Fatalf("Save 失败: %v", err)
	}
	// 关闭以确保 WAL 内容落盘，否则可能只检查了主库文件。
	if err := s.Close(); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}

	// 主库、WAL、SHM 三个文件都要查：密码可能只存在于其中之一。
	for _, suffix := range []string{"", "-wal", "-shm"} {
		path := dbPath + suffix
		data, err := os.ReadFile(path)
		if err != nil {
			continue // 该文件可能不存在，属正常
		}
		if strings.Contains(string(data), sentinel) {
			t.Errorf("%s 中含有密码明文", filepath.Base(path))
		}
	}
}

// 加密不能破坏可用性：密码必须能原样读回，否则代理认证会失败。
func TestPasswordRoundTripsThroughDatabase(t *testing.T) {
	s := openTemp(t)
	const pw = "r0undtr1p!@#"
	if err := s.Save(&model.Profile{
		ID: "p1", Name: "往返", Kind: model.KindDaily, ProfileDir: "d",
		Proxy: &model.Proxy{
			Scheme: model.ProxySOCKS5, Host: "h", Port: 1080,
			Username: "u", Password: pw,
		},
	}); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}

	got, err := s.Get("p1")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if got.Proxy == nil || got.Proxy.Password != pw {
		t.Errorf("密码未能原样读回")
	}

	// List 路径也走同一套解密逻辑，一并验证。
	list, err := s.List()
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if len(list) != 1 || list[0].Proxy == nil || list[0].Proxy.Password != pw {
		t.Error("List 返回的密码不正确")
	}
}

// Save 不得改动调用方持有的 Proxy：调用方随后可能还要用明文密码去连代理。
func TestSaveDoesNotMutateCallerProxy(t *testing.T) {
	if !secret.Available() {
		t.Skip("当前平台无系统级加密")
	}
	s := openTemp(t)
	const pw = "keep-me-plain"
	proxy := &model.Proxy{
		Scheme: model.ProxySOCKS5, Host: "h", Port: 1080,
		Username: "u", Password: pw,
	}
	p := &model.Profile{
		ID: "p1", Name: "不改动", Kind: model.KindDaily, ProfileDir: "d", Proxy: proxy,
	}
	if err := s.Save(p); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}
	if proxy.Password != pw {
		t.Errorf("Save 改动了调用方的密码: %q", proxy.Password)
	}
	if secret.IsEncrypted(proxy.Password) {
		t.Error("调用方持有的密码被替换成了密文")
	}
}

// 升级兼容：数据库里遗留的明文密码必须仍能正常读取。
func TestLegacyPlaintextPasswordStillReadable(t *testing.T) {
	s := openTemp(t)
	const legacy = "legacy-plain-password"

	// 绕过 Save 直接写入明文，模拟升级前的历史数据。
	_, err := s.db.Exec(`
		INSERT INTO profiles (id, name, kind, seed, profile_dir, proxy,
			kernel_version, notes, created_at, updated_at, last_use_at)
		VALUES ('old','历史记录','daily',0,'d',?,'','',0,0,0)`,
		`{"scheme":"socks5","host":"h","port":1080,"username":"u","password":"`+legacy+`"}`)
	if err != nil {
		t.Fatalf("写入历史记录失败: %v", err)
	}

	got, err := s.Get("old")
	if err != nil {
		t.Fatalf("读取历史记录失败: %v", err)
	}
	if got.Proxy == nil || got.Proxy.Password != legacy {
		t.Errorf("历史明文密码读取错误: %+v", got.Proxy)
	}

	// 再次保存后应转为密文。
	if err := s.Save(got); err != nil {
		t.Fatalf("重新保存失败: %v", err)
	}
	if secret.Available() {
		var stored string
		if err := s.db.QueryRow(`SELECT proxy FROM profiles WHERE id='old'`).
			Scan(&stored); err != nil {
			t.Fatalf("查询失败: %v", err)
		}
		if strings.Contains(stored, legacy) {
			t.Error("重新保存后密码仍是明文，未完成迁移")
		}
	}
}

// 无密码的代理配置不应引入空密文，否则无法区分"没有密码"和"密码为空"。
func TestProxyWithoutPasswordStaysEmpty(t *testing.T) {
	s := openTemp(t)
	if err := s.Save(&model.Profile{
		ID: "p1", Name: "无密码", Kind: model.KindDaily, ProfileDir: "d",
		Proxy: &model.Proxy{Scheme: model.ProxySOCKS5, Host: "h", Port: 1080},
	}); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}
	got, err := s.Get("p1")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if got.Proxy.Password != "" {
		t.Errorf("无密码的代理读回后 password = %q", got.Proxy.Password)
	}
}
