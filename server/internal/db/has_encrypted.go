package db

import (
	"strings"

	"github.com/ovh-buy/server/internal/secret"
)

// HasEncryptedSecrets 数据库里还有没有解不开的密文。
//
// 用途只有一个,但很要紧:密钥是这次启动新生成的、而库里已经躺着密文,
// 就说明原来那把钥匙丢了(配置文件被覆盖、换了机器只拷了数据库、
// 从别处恢复了一份 data/)。这种情况下那批凭据永远解不开了。
//
// 不做这个检查的话,程序会照常起来,账户列表也照常显示 ——
// 只是每次调 OVH 都报签名错误。没有人能从"Invalid signature"猜到是密钥问题,
// 而这时候如果用户重新录入凭据,旧密文就被覆盖,最后一点恢复的可能也没了。
func (d *DB) HasEncryptedSecrets() (bool, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(*) FROM ovh_accounts
		WHERE app_key LIKE 'enc:v1:%' OR app_secret LIKE 'enc:v1:%' OR consumer_key LIKE 'enc:v1:%'`).Scan(&n)
	if err != nil {
		return false, err
	}
	if n > 0 {
		return true, nil
	}

	// 账户表可能是空的,但全局配置里的 Telegram token 一样是加密存的
	var raw string
	err = d.QueryRow(`SELECT value FROM kv WHERE key = 'config'`).Scan(&raw)
	if err != nil {
		// 没有 config 这一行 = 全新的库,那就是真的没有密文
		return false, nil
	}
	return strings.Contains(raw, secret.EncPrefix), nil
}
