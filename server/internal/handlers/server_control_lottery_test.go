package handlers

import "testing"

// 中奖检测纯函数测试:订购配置 vs 实际硬件,三个维度(CPU/内存/硬盘)
func TestHwLottery_CpuUpgrade(t *testing.T) {
	spec := &hwOrderedSpec{CPU: "Intel Xeon E5-1620v2", MemoryGB: 32}
	r := hwComputeLottery(spec, map[string]interface{}{
		"processorName": "Intel Xeon E5-1630v4",
		"memorySize":    map[string]interface{}{"value": 65536, "unit": "MB"},
	})
	if !r.Checked || !r.Won {
		t.Fatalf("应双中奖: %+v", r)
	}
	if len(r.Items) != 2 {
		t.Fatalf("CPU+内存两项: %d", len(r.Items))
	}
	if r.Items[0].Kind != "cpu" || r.Items[0].Ordered != "Intel Xeon E5-1620v2" {
		t.Fatalf("CPU 项: %+v", r.Items[0])
	}
}

func TestHwLottery_SameSpec(t *testing.T) {
	spec := &hwOrderedSpec{CPU: "Intel Xeon E5-1620v2", MemoryGB: 32}
	r := hwComputeLottery(spec, map[string]interface{}{
		"processorName": "Xeon E5-1620v2", // 去品牌词后应判同
		"memorySize":    map[string]interface{}{"value": 32, "unit": "GB"},
	})
	if !r.Checked || r.Won {
		t.Fatalf("同配置不应中奖: %+v", r)
	}
}

func TestHwLottery_NilSpec(t *testing.T) {
	r := hwComputeLottery(nil, map[string]interface{}{})
	if r.Checked || r.Won {
		t.Fatalf("订购配置不可用不应判中奖: %+v", r)
	}
	if r.Reason == "" {
		t.Fatal("应带 Reason")
	}
}

func TestHwLottery_DiskUpgrade(t *testing.T) {
	spec := &hwOrderedSpec{
		Disks: []hwDiskSpec{{Count: 2, SizeGB: 480, Type: "ssd"}},
	}
	r := hwComputeLottery(spec, map[string]interface{}{
		"diskGroups": []interface{}{
			map[string]interface{}{
				"numberOfDisks": 2,
				"diskSize":      map[string]interface{}{"value": 2, "unit": "TB"},
				"diskType":      "NVMe",
			},
		},
	})
	if !r.Checked || !r.Won || len(r.Items) != 1 || r.Items[0].Kind != "disk" {
		t.Fatalf("SSD→NVMe 应中奖: %+v", r)
	}
}

// 量化收益与等级:内存翻倍+CPU 跨代 → tier 2;三代全中+翻倍 → 封顶 3;跨族 CPU 不算代差
func TestLotteryTierAndGains(t *testing.T) {
	// CPU 换型号 + 内存 32→64 翻倍:两项 + 翻倍加成 → 封顶 3
	spec := &hwOrderedSpec{CPU: "Intel Xeon E5-1620v2", MemoryGB: 32}
	hw := map[string]interface{}{
		"processorName": "Intel Xeon E5-1630v4",
		"memorySize":    map[string]interface{}{"value": float64(64), "unit": "GB"},
	}
	lot := hwComputeLottery(spec, hw)
	if !lot.Won || len(lot.Items) != 2 {
		t.Fatalf("应中 CPU+内存两项: %+v", lot.Items)
	}
	for _, it := range lot.Items {
		if it.Kind == "memory" && it.GainPct < 99.9 {
			t.Errorf("内存涨幅应约 100%%,实际 %.1f", it.GainPct)
		}
	}
	if lot.Tier != 3 {
		t.Errorf("两项+翻倍应封顶 tier 3,实际 %d", lot.Tier)
	}

	// CPU 换品牌、内存不变:只中一项,tier 1
	lot2 := hwComputeLottery(&hwOrderedSpec{CPU: "Intel Xeon E5-1620v2", MemoryGB: 32},
		map[string]interface{}{
			"processorName": "AMD EPYC 7302",
			"memorySize":    map[string]interface{}{"value": float64(32), "unit": "GB"},
		})
	if !lot2.Won {
		t.Fatal("跨品牌 CPU 不同仍应算中奖")
	}
	if lot2.Tier != 1 {
		t.Errorf("单项无翻倍应为 tier 1,实际 %d", lot2.Tier)
	}

	// 硬盘介质升级带 mediaUp,不加容量 pct
	lot3 := hwComputeLottery(&hwOrderedSpec{Disks: []hwDiskSpec{{Count: 2, SizeGB: 480, Type: "sa"}}},
		map[string]interface{}{
			"diskGroups": []interface{}{
				map[string]interface{}{"numberOfDisks": float64(2), "diskSize": map[string]interface{}{"value": float64(480), "unit": "GB"}, "diskType": "nvme"},
			},
		})
	if !lot3.Won || len(lot3.Items) != 1 {
		t.Fatalf("介质升级应中硬盘一项: %+v", lot3.Items)
	}
	if !lot3.Items[0].MediaUp || lot3.Items[0].GainPct != 0 {
		t.Errorf("纯介质升级应只标 mediaUp: %+v", lot3.Items[0])
	}
	if lot3.Tier != 1 {
		t.Errorf("单项介质升级应 tier 1,实际 %d", lot3.Tier)
	}
}
