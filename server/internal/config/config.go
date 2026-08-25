package config

import (
	"strings"
	"sync"

	"github.com/ovh-buy/server/internal/db"
	"github.com/ovh-buy/server/internal/secret"
	"github.com/ovh-buy/server/internal/types"
)

// Store 配置存取（线程安全）
type Store struct {
	mu  sync.RWMutex
	cfg types.Config
	db  *db.DB
}

const kvConfigKey = "config"

// secretFields kv['config'] 里需要加密落盘的字段。
// 这些和 ovh_accounts 表里的凭据是同一级别的东西:Telegram Token 能冒充你发通知、
// 甚至通过 webhook 触发下单;webhook secret 泄漏则等于 webhook 校验形同虚设。
// 老用户的 kv['config'] 里还可能残留 OVH 凭据(多账户改造之前的存法),一并处理。
func encryptConfig(c types.Config) types.Config {
	c.AppSecret = secret.Encrypt(c.AppSecret)
	c.ConsumerKey = secret.Encrypt(c.ConsumerKey)
	c.AppKey = secret.Encrypt(c.AppKey)
	c.TgToken = secret.Encrypt(c.TgToken)
	c.TgWebhookSecret = secret.Encrypt(c.TgWebhookSecret)
	return c
}

// decryptConfig 出库解密。解不开的字段留空 —— 空 token 会让 Telegram 明确报"未配置",
// 而乱码会变成 Telegram API 的 401,用户更难定位。
func decryptConfig(c types.Config) types.Config {
	dec := func(v string) string {
		out, err := secret.Decrypt(v)
		if err != nil {
			return ""
		}
		return out
	}
	c.AppKey = dec(c.AppKey)
	c.AppSecret = dec(c.AppSecret)
	c.ConsumerKey = dec(c.ConsumerKey)
	c.TgToken = dec(c.TgToken)
	c.TgWebhookSecret = dec(c.TgWebhookSecret)
	return c
}

// New 从 SQLite kv 表加载配置；不存在则使用默认值
func New(database *db.DB) *Store {
	s := &Store{
		cfg: types.DefaultConfig(),
		db:  database,
	}
	if _, err := s.db.GetKV(kvConfigKey, &s.cfg); err != nil {
		// 加载失败时退回默认值（避免阻塞启动）
		s.cfg = types.DefaultConfig()
	} else {
		s.cfg = decryptConfig(s.cfg)
	}
	// 兜底默认值
	if s.cfg.Endpoint == "" {
		s.cfg.Endpoint = "ovh-eu"
	}
	if s.cfg.Zone == "" {
		s.cfg.Zone = "IE"
	}
	if s.cfg.IAM == "" {
		s.cfg.IAM = "go-ovh-" + strings.ToLower(s.cfg.Zone)
	}
	return s
}

// Get 返回配置的副本（调用方不能修改后影响存储）
func (s *Store) Get() types.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// Set 覆盖整个配置并落盘
func (s *Store) Set(c types.Config) error {
	s.mu.Lock()
	if c.IAM == "" {
		c.IAM = "go-ovh-" + strings.ToLower(c.Zone)
	}
	s.cfg = c
	snapshot := s.cfg
	s.mu.Unlock()
	return s.db.SetKV(kvConfigKey, encryptConfig(snapshot))
}

// HasCredentials 判断是否已配置 OVH 凭据
func (s *Store) HasCredentials() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.AppKey != "" && s.cfg.AppSecret != "" && s.cfg.ConsumerKey != ""
}

// APIBaseURL 根据 endpoint 返回 OVH REST API base URL
func (s *Store) APIBaseURL() string {
	s.mu.RLock()
	ep := s.cfg.Endpoint
	s.mu.RUnlock()
	switch ep {
	case "ovh-us":
		return "https://api.us.ovhcloud.com"
	case "ovh-ca":
		return "https://ca.api.ovh.com"
	default:
		return "https://eu.api.ovh.com"
	}
}
