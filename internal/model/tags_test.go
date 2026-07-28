package model

import (
	"slices"
	"strings"
	"testing"
)

func TestNormalizeTags(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"空输入", nil, nil},
		{"全是空白", []string{"", "  ", "\t"}, nil},
		{"去空白", []string{" a ", "b "}, []string{"a", "b"}},
		{"去重", []string{"a", "a", "b"}, []string{"a", "b"}},
		// 大小写不同视为同一标签，否则筛选时会漏项。
		{"大小写去重", []string{"Tag", "tag", "TAG"}, []string{"Tag"}},
		{"保持写入顺序", []string{"z", "a", "m"}, []string{"z", "a", "m"}},
		{"混合", []string{" X ", "", "x", "y"}, []string{"X", "y"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := NormalizeTags(c.in)
			if !slices.Equal(got, c.want) {
				t.Errorf("NormalizeTags(%v) = %v, 期望 %v", c.in, got, c.want)
			}
		})
	}
}

// 超长标签截断而非报错：用户填标签时不该被打断。
func TestNormalizeTagsTruncatesLongTags(t *testing.T) {
	long := strings.Repeat("字", MaxTagLength+10)
	got := NormalizeTags([]string{long})
	if len(got) != 1 {
		t.Fatalf("结果数量 = %d", len(got))
	}
	// 按 rune 截断，不能把多字节字符切坏。
	if n := len([]rune(got[0])); n != MaxTagLength {
		t.Errorf("截断后长度 = %d 个字符, 期望 %d", n, MaxTagLength)
	}
	if !strings.HasPrefix(long, got[0]) {
		t.Error("截断结果应是原串的前缀")
	}
}

func TestNormalizeGroup(t *testing.T) {
	cases := map[string]string{
		"":            "",
		"   ":         "",
		"  分组  ":      "分组",
		"electronics": "electronics",
	}
	for in, want := range cases {
		if got := NormalizeGroup(in); got != want {
			t.Errorf("NormalizeGroup(%q) = %q, 期望 %q", in, got, want)
		}
	}

	long := strings.Repeat("组", MaxTagLength+5)
	if n := len([]rune(NormalizeGroup(long))); n != MaxTagLength {
		t.Errorf("超长分组名截断后 = %d 个字符, 期望 %d", n, MaxTagLength)
	}
}

func TestHasTag(t *testing.T) {
	p := &Profile{Tags: []string{"已验证", "Verified"}}

	for _, want := range []string{"已验证", "Verified", "verified", "VERIFIED", " 已验证 "} {
		if !p.HasTag(want) {
			t.Errorf("HasTag(%q) = false, 期望 true", want)
		}
	}
	for _, notWant := range []string{"", "  ", "未验证", "verifie"} {
		if p.HasTag(notWant) {
			t.Errorf("HasTag(%q) = true, 期望 false", notWant)
		}
	}
}

func TestHasTagOnEmptyProfile(t *testing.T) {
	p := &Profile{}
	if p.HasTag("anything") {
		t.Error("无标签的 profile 不应命中任何标签")
	}
}
