package store

import (
	"fmt"
	"path/filepath"
	"testing"

	"better-web/internal/model"
)

// List 位于界面轮询的热路径上：每 2 秒调一次，每次返回全部 profile。
// 测它是为了确认轮询间隔不需要放宽，以及是否需要缓存。
func BenchmarkList(b *testing.B) {
	for _, n := range []int{10, 100, 500} {
		b.Run(fmt.Sprintf("%d个profile", n), func(b *testing.B) {
			s := openBench(b, n)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				list, err := s.List()
				if err != nil {
					b.Fatalf("List 失败: %v", err)
				}
				if len(list) != n {
					b.Fatalf("记录数 = %d, 期望 %d", len(list), n)
				}
			}
		})
	}
}

func BenchmarkGet(b *testing.B) {
	s := openBench(b, 500)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Get(fmt.Sprintf("bench-%d", i%500)); err != nil {
			b.Fatalf("Get 失败: %v", err)
		}
	}
}

// Save 在每次编辑 profile 时调用，不在热路径上，但值得确认没有异常开销。
func BenchmarkSave(b *testing.B) {
	s := openBench(b, 0)
	p := &model.Profile{
		ID: "bench-save", Name: "基准", Kind: model.KindFingerprint,
		ProfileDir: `C:\p\bench`, Seed: 42,
		Proxy: &model.Proxy{
			Scheme: model.ProxySOCKS5, Host: "127.0.0.1", Port: 1080,
			Username: "u", Password: "p",
		},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := s.Save(p); err != nil {
			b.Fatalf("Save 失败: %v", err)
		}
	}
}

// openBench 打开一个预置 n 条记录的库。
func openBench(b *testing.B, n int) *Store {
	b.Helper()
	s, err := Open(filepath.Join(b.TempDir(), "bench.db"))
	if err != nil {
		b.Fatalf("Open 失败: %v", err)
	}
	b.Cleanup(func() { _ = s.Close() })

	for i := 0; i < n; i++ {
		p := &model.Profile{
			ID:   fmt.Sprintf("bench-%d", i),
			Name: fmt.Sprintf("profile-%d", i),
			Kind: model.KindFingerprint, ProfileDir: `C:\p\` + fmt.Sprint(i),
			Seed: int32(i + 1),
			Proxy: &model.Proxy{
				Scheme: model.ProxySOCKS5, Host: "gate.example.com", Port: 7000 + i,
				Username: "user", Password: "pass",
			},
		}
		if err := s.Save(p); err != nil {
			b.Fatalf("预置数据失败: %v", err)
		}
	}
	return s
}
