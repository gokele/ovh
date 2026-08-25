package db

import (
	"encoding/json"
	"fmt"

	"github.com/ovh-buy/server/internal/secret"
)

// EncryptExistingSecrets 把老库里已有的明文凭据就地加密。
//
// 升级路径必须是无痛的:用户从 v0.1.x 升上来时,ovh_accounts 里三个凭据字段、
// kv['config'] 里的 Telegram Token 都还是明文。加解密层本身对明文是兼容的
// (没有 enc:v1: 前缀就当明文读),所以**不迁移也能正常跑** —— 但那样明文会
// 一直躺在磁盘上,加密就等于没开。
//
// 迁移是幂等的:已经是密文的跳过。加密不可用时直接返回,什么都不做。
// 单条失败不中断整体:宁可少加密一条,也不能因为一条坏数据让用户启动不了。
func (db *DB) EncryptExistingSecrets() (migrated int, err error) {
	if !secret.Enabled() {
		return 0, nil
	}

	// ---- 1. ovh_accounts 的三个凭据列 ----
	type row struct {
		ID          string `db:"id"`
		AppKey      string `db:"app_key"`
		AppSecret   string `db:"app_secret"`
		ConsumerKey string `db:"consumer_key"`
	}
	var rows []row
	if err := db.Select(&rows, `SELECT id, app_key, app_secret, consumer_key FROM ovh_accounts`); err != nil {
		return 0, fmt.Errorf("读取账户失败: %w", err)
	}
	for _, r := range rows {
		if secret.IsEncrypted(r.AppKey) && secret.IsEncrypted(r.AppSecret) && secret.IsEncrypted(r.ConsumerKey) {
			continue // 已经加密过
		}
		// 只加密还是明文的那几列,避免把密文再加密一层
		enc := func(v string) string {
			if secret.IsEncrypted(v) {
				return v
			}
			return secret.Encrypt(v)
		}
		if _, uerr := db.Exec(
			`UPDATE ovh_accounts SET app_key = ?, app_secret = ?, consumer_key = ? WHERE id = ?`,
			enc(r.AppKey), enc(r.AppSecret), enc(r.ConsumerKey), r.ID,
		); uerr != nil {
			// 记下来但继续:一条坏数据不该让整个启动流程失败
			err = fmt.Errorf("加密账户 %s 失败: %w", r.ID, uerr)
			continue
		}
		migrated++
	}
	// ---- 2. kv['config'] 里的 Telegram Token / webhook secret ----
	// 这块是整条 JSON 存的,所以只能读出来、改字段、写回去。
	// config.Store 保存时会自动加密,但那要等用户下次改设置才会触发 ——
	// 这里主动走一遍,免得 token 明文一直躺在库里。
	var raw string
	if gerr := db.Get(&raw, `SELECT value FROM kv WHERE key = 'config'`); gerr == nil && raw != "" {
		if updated, changed := encryptConfigJSON(raw); changed {
			if _, uerr := db.Exec(`UPDATE kv SET value = ? WHERE key = 'config'`, updated); uerr != nil {
				if err == nil {
					err = fmt.Errorf("加密 kv[config] 失败: %w", uerr)
				}
			} else {
				migrated++
			}
		}
	}

	return migrated, err
}

// encryptConfigJSON 把 config JSON 里的敏感字段就地加密,返回 (新 JSON, 是否有改动)
func encryptConfigJSON(raw string) (string, bool) {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return raw, false
	}
	changed := false
	for _, k := range []string{"appKey", "appSecret", "consumerKey", "tgToken", "tgWebhookSecret"} {
		v, _ := m[k].(string)
		if v == "" || secret.IsEncrypted(v) {
			continue
		}
		m[k] = secret.Encrypt(v)
		changed = true
	}
	if !changed {
		return raw, false
	}
	out, err := json.Marshal(m)
	if err != nil {
		return raw, false
	}
	return string(out), true
}
