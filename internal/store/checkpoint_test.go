package store

import (
	"os"
	"path/filepath"
	"testing"

	"better-web/internal/model"
)

// Close 后 WAL 应被合并回主库。
//
// 否则最后一批写入只存在于 -wal 文件中，用户拷贝或备份 profiles.db 时
// 会漏掉这部分数据而不自知。
func TestCloseCheckpointsWAL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ckpt.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open 失败: %v", err)
	}

	// 写入足够多的记录，确保 WAL 里确实有内容。
	for i := 0; i < 20; i++ {
		p := &model.Profile{
			ID:   "ck-" + string(rune('a'+i)),
			Name: "记录" + string(rune('a'+i)),
			Kind: model.KindFingerprint, ProfileDir: dir, Seed: int32(i + 1),
		}
		if err := s.Save(p); err != nil {
			t.Fatalf("Save 失败: %v", err)
		}
	}

	walBefore := fileSize(filepath.Join(dir, "ckpt.db-wal"))
	if walBefore == 0 {
		t.Skip("WAL 未生成，可能是驱动行为差异，跳过")
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}

	if after := fileSize(filepath.Join(dir, "ckpt.db-wal")); after >= walBefore {
		t.Errorf("Close 后 WAL 未被截断: %d -> %d 字节", walBefore, after)
	}

	// 数据必须完好——checkpoint 不能丢东西。
	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("重新打开失败: %v", err)
	}
	defer func() { _ = reopened.Close() }()
	list, err := reopened.List()
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if len(list) != 20 {
		t.Errorf("重新打开后记录数 = %d, 期望 20", len(list))
	}
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}
