package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

// 账户密码登录。
//
// 管理员只有一个(这是单人自用的控制台):用户名 + bcrypt 密码哈希存在 SQLite kv。
// 登录成功发一个随机会话令牌,浏览器放 localStorage、每次请求带 X-Auth-Token 头。
// 服务端只存令牌的 SHA-256,数据库泄露也拿不到可用的令牌。
//
// 旧版的 API 密钥(API_SECRET_KEY / 数据目录 .api-secret)不再能直接登录,
// 只在还没有管理员时作为「初始化密钥」使用:证明你是这台机器的主人,才能创建账号。
// 否则一台暴露在公网的新实例,谁先打开网页谁就成了管理员。

const (
	kvAdminKey    = "auth-admin"
	kvSessionsKey = "auth-sessions"

	SessionTTL       = 30 * 24 * time.Hour
	sessionRenewWhen = 15 * 24 * time.Hour // 剩余不足这么久时顺延,常用的设备不会被踢
	maxSessions      = 50

	MinPasswordLen = 8
	MaxPasswordLen = 72 // bcrypt 只看前 72 字节,再长就是假安全
	MaxUsernameLen = 64
)

// bcryptCost 测试里调低,否则 race 模式下每次哈希要好几百毫秒。
var bcryptCost = 12

var (
	ErrSetupDone        = errors.New("已经创建过管理员账号")
	ErrBadSetupKey      = errors.New("初始化密钥不正确")
	ErrBadCredentials   = errors.New("用户名或密码错误")
	ErrInvalidUsername  = errors.New("用户名需为 1~64 个字符,不能包含空白")
	ErrPasswordTooShort = errors.New("密码至少 8 位")
	ErrPasswordTooLong  = errors.New("密码最多 72 个字节")
)

// KV 是 *db.DB 的子集,测试里可以换成内存实现。
type KV interface {
	GetKV(key string, v interface{}) (bool, error)
	SetKV(key string, v interface{}) error
}

type adminRecord struct {
	Username     string    `json:"username"`
	PasswordHash string    `json:"passwordHash"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type session struct {
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
	UserAgent string    `json:"userAgent,omitempty"`
}

// Accounts 管理员账号与会话。并发安全。
type Accounts struct {
	kv       KV
	setupKey string

	mu       sync.Mutex
	admin    *adminRecord
	sessions map[string]session // key = sha256(token)
	now      func() time.Time
}

// dummyHash 用来在用户名不存在时也跑一遍 bcrypt,不让响应时间泄露用户名对不对。
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("timing-equaliser"), bcryptCost)

// LoadAccounts 从 kv 读取管理员与会话。setupKey 是旧 API 密钥,只用于首次创建账号。
func LoadAccounts(kv KV, setupKey string) (*Accounts, error) {
	a := &Accounts{kv: kv, setupKey: setupKey, sessions: map[string]session{}, now: time.Now}
	var admin adminRecord
	ok, err := kv.GetKV(kvAdminKey, &admin)
	if err != nil {
		return nil, err
	}
	if ok && admin.Username != "" && admin.PasswordHash != "" {
		a.admin = &admin
	}
	var sessions map[string]session
	if ok, err := kv.GetKV(kvSessionsKey, &sessions); err != nil {
		return nil, err
	} else if ok && sessions != nil {
		now := a.now()
		for k, s := range sessions {
			if s.ExpiresAt.After(now) && a.admin != nil && s.Username == a.admin.Username {
				a.sessions[k] = s
			}
		}
	}
	return a, nil
}

// SetupRequired 还没有管理员账号。
func (a *Accounts) SetupRequired() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.admin == nil
}

// Username 当前管理员用户名(未创建时为空)。
func (a *Accounts) Username() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.admin == nil {
		return ""
	}
	return a.admin.Username
}

func ValidateUsername(u string) error {
	if u == "" || utf8.RuneCountInString(u) > MaxUsernameLen || strings.ContainsAny(u, " \t\r\n") {
		return ErrInvalidUsername
	}
	return nil
}

func ValidatePassword(p string) error {
	if utf8.RuneCountInString(p) < MinPasswordLen {
		return ErrPasswordTooShort
	}
	if len(p) > MaxPasswordLen {
		return ErrPasswordTooLong
	}
	return nil
}

// CheckSetupKey 常量时间比较初始化密钥。
func (a *Accounts) CheckSetupKey(key string) bool {
	return a.setupKey != "" && subtle.ConstantTimeCompare([]byte(strings.TrimSpace(key)), []byte(a.setupKey)) == 1
}

// Setup 首次创建管理员。必须提供正确的初始化密钥;成功后直接登录,返回会话令牌。
func (a *Accounts) Setup(setupKey, username, password, userAgent string) (string, error) {
	username = strings.TrimSpace(username)
	if err := ValidateUsername(username); err != nil {
		return "", err
	}
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	if !a.CheckSetupKey(setupKey) {
		return "", ErrBadSetupKey
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.admin != nil { // 两个人同时提交:后到的那个必须失败
		return "", ErrSetupDone
	}
	rec := &adminRecord{Username: username, PasswordHash: string(hash), UpdatedAt: a.now()}
	if err := a.kv.SetKV(kvAdminKey, rec); err != nil {
		return "", err
	}
	a.admin = rec
	a.sessions = map[string]session{}
	return a.newSessionLocked(userAgent)
}

// SeedFromEnv 用环境变量里的用户名密码创建管理员(还没有管理员时),
// 或在 reset=true 时覆盖现有密码并踢掉所有会话(忘记密码的恢复路径)。
// 返回 created/reset 表示实际做了什么。
func (a *Accounts) SeedFromEnv(username, password string, reset bool) (created, didReset bool, err error) {
	username = strings.TrimSpace(username)
	if username == "" && password == "" {
		return false, false, nil
	}
	if err := ValidateUsername(username); err != nil {
		return false, false, err
	}
	if err := ValidatePassword(password); err != nil {
		return false, false, err
	}
	a.mu.Lock()
	exists := a.admin != nil
	a.mu.Unlock()
	if exists && !reset {
		return false, false, nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return false, false, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	rec := &adminRecord{Username: username, PasswordHash: string(hash), UpdatedAt: a.now()}
	if err := a.kv.SetKV(kvAdminKey, rec); err != nil {
		return false, false, err
	}
	a.admin = rec
	a.sessions = map[string]session{}
	_ = a.persistLocked()
	return !exists, exists, nil
}

// Login 校验用户名密码,成功返回新会话令牌。
func (a *Accounts) Login(username, password, userAgent string) (string, error) {
	username = strings.TrimSpace(username)
	a.mu.Lock()
	admin := a.admin
	a.mu.Unlock()
	if admin == nil {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return "", ErrBadCredentials
	}
	userOK := subtle.ConstantTimeCompare([]byte(strings.ToLower(username)), []byte(strings.ToLower(admin.Username))) == 1
	hash := []byte(admin.PasswordHash)
	if !userOK {
		hash = dummyHash
	}
	if err := bcrypt.CompareHashAndPassword(hash, []byte(password)); err != nil || !userOK {
		return "", ErrBadCredentials
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.admin == nil || a.admin.PasswordHash != admin.PasswordHash { // 期间密码被改了
		return "", ErrBadCredentials
	}
	return a.newSessionLocked(userAgent)
}

// ChangePassword 校验旧密码后改密,并踢掉除当前会话外的所有会话。
func (a *Accounts) ChangePassword(currentToken, oldPassword, newPassword string) error {
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}
	a.mu.Lock()
	admin := a.admin
	a.mu.Unlock()
	if admin == nil || bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(oldPassword)) != nil {
		return ErrBadCredentials
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcryptCost)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	rec := &adminRecord{Username: admin.Username, PasswordHash: string(hash), UpdatedAt: a.now()}
	if err := a.kv.SetKV(kvAdminKey, rec); err != nil {
		return err
	}
	a.admin = rec
	keep := hashToken(currentToken)
	for k := range a.sessions {
		if k != keep {
			delete(a.sessions, k)
		}
	}
	return a.persistLocked()
}

// ValidateSession 返回令牌对应的用户名。快到期的会话会自动顺延。
func (a *Accounts) ValidateSession(token string) (string, bool) {
	if token == "" {
		return "", false
	}
	k := hashToken(token)
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.sessions[k]
	if !ok || a.admin == nil || s.Username != a.admin.Username {
		return "", false
	}
	now := a.now()
	if !s.ExpiresAt.After(now) {
		delete(a.sessions, k)
		_ = a.persistLocked()
		return "", false
	}
	if s.ExpiresAt.Sub(now) < sessionRenewWhen {
		s.ExpiresAt = now.Add(SessionTTL)
		a.sessions[k] = s
		_ = a.persistLocked()
	}
	return s.Username, true
}

// Logout 作废一个会话。
func (a *Accounts) Logout(token string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	k := hashToken(token)
	if _, ok := a.sessions[k]; ok {
		delete(a.sessions, k)
		_ = a.persistLocked()
	}
}

func (a *Accounts) newSessionLocked(userAgent string) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf)
	now := a.now()
	for k, s := range a.sessions {
		if !s.ExpiresAt.After(now) {
			delete(a.sessions, k)
		}
	}
	// 会话数封顶:淘汰最早创建的,别让反复登录把 kv 撑大
	for len(a.sessions) >= maxSessions {
		var oldestK string
		var oldest time.Time
		for k, s := range a.sessions {
			if oldestK == "" || s.CreatedAt.Before(oldest) {
				oldestK, oldest = k, s.CreatedAt
			}
		}
		delete(a.sessions, oldestK)
	}
	if len(userAgent) > 200 {
		userAgent = userAgent[:200]
	}
	a.sessions[hashToken(token)] = session{Username: a.admin.Username, CreatedAt: now, ExpiresAt: now.Add(SessionTTL), UserAgent: userAgent}
	if err := a.persistLocked(); err != nil {
		delete(a.sessions, hashToken(token))
		return "", err
	}
	return token, nil
}

func (a *Accounts) persistLocked() error {
	return a.kv.SetKV(kvSessionsKey, a.sessions)
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
