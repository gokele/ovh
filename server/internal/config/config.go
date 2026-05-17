package config

import (
	"strings"
	"sync"

	"github.com/ovh-buy/server/internal/db"
	"github.com/ovh-buy/server/internal/types"
)

// Store 配置存取（线程安全，对应 Python 全局 config dict）
type Store struct {
	mu  sync.RWMutex
	cfg types.Config
	db  *db.DB
}

const kvConfigKey = "config"

// New 从 SQLite kv 表加载配置；不存在则使用默认值
func New(database *db.DB) *Store {
	s := &Store{
		cfg: types.DefaultConfig(),
		db:  database,
	}
	if _, err := s.db.GetKV(kvConfigKey, &s.cfg); err != nil {
		// 加载失败时退回默认值（避免阻塞启动）
		s.cfg = types.DefaultConfig()
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
	return s.db.SetKV(kvConfigKey, snapshot)
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
