package fingerprint

import "testing"

// 量化"往档案库追加一条"对已有 profile 的影响面。
//
// pick 是对档案库长度取模，长度一变索引整体错位，因此追加档案会让大部分
// 种子抽到不同机型——对已在使用的 profile 就是身份漂移，等于所有账号
// 同时换了设备。
//
// 本测试不是通过/失败断言，而是把代价摆出来：档案库需要扩充时，
// 得先知道会牵动多少 profile，并决定是接受漂移还是改用不影响索引的做法。
func TestQuantifyCatalogGrowthImpact(t *testing.T) {
	const n = 3000
	cur := len(safeCatalog)
	if cur < 2 {
		t.Skip("可用档案不足，无法比较")
	}

	// 模拟"少一条"时的抽取结果，与当前对比。
	prev := cur - 1
	var changed int
	for i := 0; i < n; i++ {
		seed := int32(1 + i*7919)
		d := derived{seed: seed}
		if safeCatalog[d.pick("device", prev)].Label !=
			safeCatalog[d.pick("device", cur)].Label {
			changed++
		}
	}
	pct := float64(changed) / float64(n) * 100
	t.Logf("档案库从 %d 条增到 %d 条: %d/%d 个种子换机型（%.1f%%）",
		prev, cur, changed, n, pct)
	t.Log("含义: 扩充档案库会让这个比例的已有 profile 指纹漂移。")
	t.Log("      新建的 profile 不受影响——它们的种子本来就没用过。")
	t.Log("      若要在不影响已有 profile 的前提下扩充，需要改用与档案库长度")
	t.Log("      无关的映射（如按种子哈希落到固定的桶区间），代价是实现复杂度。")
}
