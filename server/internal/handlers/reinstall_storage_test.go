package handlers

import "testing"

// 这些规则全部来自 OVH 官方分区文档
// (pages/bare_metal_cloud/dedicated_servers/partitioning_ovh)，
// schema 的 RaidLevelEnum 是 [0,1,5,6,7,10] 一视同仁，拦不住这些组合。

// 官方兼容表：ext4 / XFS / Btrfs 这一行 RAID 7 是 ❌
func TestFSRaidCompat_ext4不支持RAID7(t *testing.T) {
	if checkFSRaidCompat("ext4", 7) == "" {
		t.Error("ext4 + RAID7 应该被拦下")
	}
	for _, lv := range []int64{0, 1, 5, 6, 10} {
		if msg := checkFSRaidCompat("ext4", lv); msg != "" {
			t.Errorf("ext4 + RAID%d 是合法组合，却被拦: %s", lv, msg)
		}
	}
}

// 同一张表：ZFS 那一行 RAID 10 是 ❌，但 RAID 7 是 ✅（和 ext4 正好相反）
func TestFSRaidCompat_zfs不支持RAID10(t *testing.T) {
	if checkFSRaidCompat("zfs", 10) == "" {
		t.Error("zfs + RAID10 应该被拦下")
	}
	for _, lv := range []int64{0, 1, 5, 6, 7} {
		if msg := checkFSRaidCompat("zfs", lv); msg != "" {
			t.Errorf("zfs + RAID%d 是合法组合，却被拦: %s", lv, msg)
		}
	}
}

func TestFSRaidCompat_ntfs只支持RAID1(t *testing.T) {
	if checkFSRaidCompat("ntfs", 1) != "" {
		t.Error("ntfs + RAID1 是文档里唯一合法的组合")
	}
	for _, lv := range []int64{0, 5, 6, 7, 10} {
		if checkFSRaidCompat("ntfs", lv) == "" {
			t.Errorf("ntfs + RAID%d 应该被拦下", lv)
		}
	}
}

func TestFSRaidCompat_vmfs不支持任何RAID(t *testing.T) {
	for _, fs := range []string{"ufs", "vmfs5", "vmfs6", "vmfsl"} {
		for _, lv := range []int64{0, 1, 5, 6, 7, 10} {
			if checkFSRaidCompat(fs, lv) == "" {
				t.Errorf("%s + RAID%d 应该被拦下", fs, lv)
			}
		}
	}
}

// swap 那一行文档自相矛盾（表格标 RAID 0 可用、RAID 1 不可用，脚注却写"只能设为 1"），
// 所以代码有意不对 swap 下判断。这条测试锁住这个"不判断"的决定 ——
// 哪天有人凭感觉给 swap 加规则，会先在这里被挡住，逼他去查清楚文档到底怎么说。
func TestFSRaidCompat_swap不做判断(t *testing.T) {
	for _, lv := range []int64{0, 1, 5, 6, 7, 10} {
		if msg := checkFSRaidCompat("swap", lv); msg != "" {
			t.Errorf("swap 的 RAID 规则在官方文档里表格和脚注互相矛盾，"+
				"代码不该替用户下判断，却拦了 RAID%d: %s", lv, msg)
		}
	}
}

func layout(parts ...map[string]interface{}) interface{} {
	items := make([]interface{}, 0, len(parts))
	for _, p := range parts {
		items = append(items, p)
	}
	return []interface{}{
		map[string]interface{}{
			"partitioning": map[string]interface{}{"layout": items},
		},
	}
}

// 文档："Up to 1 partition can be configured to fill the remaining space (size 0)"
func TestNormalizeStorage_只允许一个占满剩余空间的分区(t *testing.T) {
	_, err := normalizeStorageConfig(layout(
		map[string]interface{}{"fileSystem": "ext4", "mountPoint": "/", "size": 0},
		map[string]interface{}{"fileSystem": "ext4", "mountPoint": "/data", "size": 0},
	))
	if err == nil {
		t.Fatal("两个 size=0 的分区应该被拦下")
	}

	// 一个是允许的
	if _, err := normalizeStorageConfig(layout(
		map[string]interface{}{"fileSystem": "ext4", "mountPoint": "/", "size": 0},
		map[string]interface{}{"fileSystem": "ext4", "mountPoint": "/data", "size": 10240},
	)); err != nil {
		t.Errorf("只有一个 size=0 是合法的，却报错: %v", err)
	}
}

// 文档：swap 不允许占满磁盘
func TestNormalizeStorage_swap不能占满磁盘(t *testing.T) {
	_, err := normalizeStorageConfig(layout(
		map[string]interface{}{"fileSystem": "swap", "mountPoint": "swap", "size": 0},
	))
	if err == nil {
		t.Fatal("swap + size=0 应该被拦下")
	}
	// 给了大小就没问题
	if _, err := normalizeStorageConfig(layout(
		map[string]interface{}{"fileSystem": "swap", "mountPoint": "swap", "size": 8192},
	)); err != nil {
		t.Errorf("swap 指定了大小是合法的，却报错: %v", err)
	}
}

// 磁盘组编号从 1 起（文档："By default, the OS will be installed on diskGroupId 1"）。
// 0 是前端"没选"的占位值，发出去等于指定了一个不存在的组。
func TestNormalizeStorage_diskGroupId为0时不发送(t *testing.T) {
	out, err := normalizeStorageConfig([]interface{}{
		map[string]interface{}{
			"diskGroupId":  0,
			"partitioning": map[string]interface{}{"layout": []interface{}{map[string]interface{}{"fileSystem": "ext4", "mountPoint": "/", "size": 0}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := out[0]["diskGroupId"]; ok {
		t.Errorf("diskGroupId=0 不该发出去，实际发了 %v", out[0]["diskGroupId"])
	}

	// 真实组号要保留
	out, err = normalizeStorageConfig([]interface{}{
		map[string]interface{}{
			"diskGroupId":  2,
			"partitioning": map[string]interface{}{"layout": []interface{}{map[string]interface{}{"fileSystem": "ext4", "mountPoint": "/", "size": 0}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out[0]["diskGroupId"] != int64(2) {
		t.Errorf("真实磁盘组号应该保留，实际 %v", out[0]["diskGroupId"])
	}
}
