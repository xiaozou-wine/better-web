package app

import (
	"encoding/json"
	"testing"
	"time"
)

// markUsed 把 profile 标记为已使用过，用于测试依赖使用历史的逻辑
// （如换内核大版本的漂移防护）。
func markUsed(t *testing.T, s *Service, id string) {
	t.Helper()
	if err := s.store.TouchLastUse(id, time.Now()); err != nil {
		t.Fatalf("标记使用时间失败: %v", err)
	}
}

// dumpJSON 按前端实际收到的形式序列化视图，用于断言敏感字段不外泄。
// Wails 走 JSON 传输，因此 JSON 里没有就是前端拿不到。
func dumpJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	return string(b)
}
