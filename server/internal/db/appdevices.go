package db

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

// App 配对体系:设备令牌 + 一次性配对码。
//
// 威胁模型与取舍(与 /api/telegram/webhook 时代不同,这里没有公网回调面):
//   - 配对码 2 分钟过期、一次性、8 位无易混字符 → 暴力空间足够大,
//     且兑换接口按来源 IP 限流(复用 auth 失败计数的同款思路)
//   - 设备令牌 32 字节随机,库里只存 SHA-256 —— 拿到库拿不到令牌
//   - 令牌可单独吊销:手机丢了在网页端点一下,不用换主密钥、其他设备不掉线
//
// 表在 ensureTables 里 CREATE IF NOT EXISTS,无增量列,不存在老库迁移问题。

// AppDevice 已配对设备(网页端管理界面用)。时间列为 RFC3339 文本,Scan 用 NullString 再解析
type AppDevice struct {
	ID        int64
	Name      string
	CreatedAt time.Time
	LastUsed  sql.NullTime
	Revoked   sql.NullTime
}

// scanDeviceTime 把 RFC3339 文本列解析成 NullTime(空串/坏值 = NULL)
func scanDeviceTime(s sql.NullString) sql.NullTime {
	if !s.Valid || s.String == "" {
		return sql.NullTime{}
	}
	if t, err := time.Parse(time.RFC3339, s.String); err == nil {
		return sql.NullTime{Valid: true, Time: t}
	}
	return sql.NullTime{}
}

// ErrPairingCodeNotFound / ErrPairingCodeUsed / ErrPairingCodeExpired 配对码三种死法,handler 分别给话
var (
	ErrPairingCodeNotFound = errors.New("配对码不存在")
	ErrPairingCodeUsed     = errors.New("配对码已被使用")
	ErrPairingCodeExpired  = errors.New("配对码已过期")
	ErrDeviceNotFound      = errors.New("设备不存在")
)

// generatePairingCode 生成 8 位配对码,去掉 0/O/1/I 这类肉眼易混字符
func generatePairingCode() (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b), nil
}

// CreatePairingCode 生成一枚 2 分钟有效的一次性配对码(网页端调用,需 API Key)
func (d *DB) CreatePairingCode() (code string, expiresAt time.Time, err error) {
	code, err = generatePairingCode()
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt = time.Now().Add(2 * time.Minute)
	// 同码撞库极小概率发生;真撞了就重生成一次,两次都撞到天文数字级不可能
	for i := 0; i < 2; i++ {
		_, err = d.Exec(`INSERT INTO app_pairing_codes(code, created_at, expires_at) VALUES(?,?,?)`,
			code, time.Now().UTC().Format(time.RFC3339), expiresAt.UTC().Format(time.RFC3339))
		if err == nil {
			return code, expiresAt, nil
		}
		if code, err = generatePairingCode(); err != nil {
			return "", time.Time{}, err
		}
	}
	return "", time.Time{}, err
}

// RedeemPairingCode 原子兑换:校验未用未过期并当场置 used,返回新建设备的明文令牌(仅此一次可见)。
//
// 一次性保证:单条 UPDATE 带 WHERE used_at IS NULL AND expires_at > now 做原子认领,
// 并发兑换同一码只有一个连接能改到行 —— 与 Telegram 按钮 claim 同一模式。
// 置 used 与 INSERT 设备在同一个事务:建设备失败时回滚,码回到未用状态,
// 用户网络闪断重试同一码仍能成功(而不是白白浪费一枚码)。
func (d *DB) RedeemPairingCode(code, deviceName string) (token string, deviceID int64, err error) {
	now := time.Now().UTC().Format(time.RFC3339)
	// 事务包住"认领码 + 建设备";d.Exec 在事务内走 tx
	tx, err := d.Begin()
	if err != nil {
		return "", 0, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	res, err := tx.Exec(
		`UPDATE app_pairing_codes SET used_at = ? WHERE code = ? AND used_at IS NULL AND expires_at > ?`,
		now, code, now)
	if err != nil {
		return "", 0, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// 区分三种死法给不同提示:不存在(打错)/ 已用(重放了)/ 过期(超 2 分钟)
		var usedAt, expires sql.NullString
		row := tx.QueryRow(`SELECT used_at, expires_at FROM app_pairing_codes WHERE code = ?`, code)
		if e := row.Scan(&usedAt, &expires); e != nil {
			return "", 0, ErrPairingCodeNotFound
		}
		if usedAt.Valid {
			return "", 0, ErrPairingCodeUsed
		}
		return "", 0, ErrPairingCodeExpired
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", 0, err
	}
	token = hex.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	if deviceName == "" {
		deviceName = "未命名设备"
	}
	res2, err := tx.Exec(
		`INSERT INTO app_devices(name, token_hash, created_at) VALUES(?,?,?)`,
		deviceName, hex.EncodeToString(sum[:]), now)
	if err != nil {
		return "", 0, err
	}
	deviceID, _ = res2.LastInsertId()
	if err := tx.Commit(); err != nil {
		return "", 0, err
	}
	committed = true
	return token, deviceID, nil
}

// hashToken 令牌明文 → 存储摘要
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// DeviceTokenValid 校验令牌是否属于未吊销设备;命中时顺带刷新 last_used(节流:5 分钟内不重复写)
func (d *DB) DeviceTokenValid(token string) (deviceID int64, ok bool) {
	// 列名是 last_used(见 schema);时间统一存 RFC3339 文本,Scan 用 NullString
	row := d.QueryRow(
		`SELECT id, last_used FROM app_devices WHERE token_hash = ? AND revoked_at IS NULL`, hashToken(token))
	var device int64
	var last sql.NullString
	if err := row.Scan(&device, &last); err != nil {
		return 0, false
	}
	refresh := !last.Valid
	if last.Valid {
		if t, err := time.Parse(time.RFC3339, last.String); err != nil || time.Since(t) > 5*time.Minute {
			refresh = true
		}
	}
	if refresh {
		// best-effort 刷新,失败不影响本次鉴权
		_, _ = d.Exec(`UPDATE app_devices SET last_used = ? WHERE id = ?`,
			time.Now().UTC().Format(time.RFC3339), device)
	}
	return device, true
}

// ListDevices 全部设备(含已吊销,管理界面要能看到历史)
func (d *DB) ListDevices() ([]AppDevice, error) {
	rows, err := d.Query(`SELECT id, name, created_at, last_used, revoked_at FROM app_devices ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AppDevice{}
	for rows.Next() {
		var dev AppDevice
		var created, last, revoked sql.NullString
		if err := rows.Scan(&dev.ID, &dev.Name, &created, &last, &revoked); err != nil {
			return nil, err
		}
		dev.CreatedAt = scanDeviceTime(created).Time
		dev.LastUsed = scanDeviceTime(last)
		dev.Revoked = scanDeviceTime(revoked)
		out = append(out, dev)
	}
	return out, rows.Err()
}

// RevokeDevice 吊销设备;已吊销的再点一次不报错(幂等)
func (d *DB) RevokeDevice(id int64) error {
	_, err := d.Exec(`UPDATE app_devices SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`, time.Now().UTC().Format(time.RFC3339), id)
	return err
}

// CleanupPairingCodes 定期清掉过期超过 1 小时的配对码(后台 goroutine 调)
func (d *DB) CleanupPairingCodes() {
	_, _ = d.Exec(`DELETE FROM app_pairing_codes WHERE expires_at < ?`, time.Now().Add(-time.Hour).UTC())
}
