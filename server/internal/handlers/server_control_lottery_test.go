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
