package auth

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type memKV struct {
	mu sync.Mutex
	m  map[string][]byte
}

func newMemKV() *memKV { return &memKV{m: map[string][]byte{}} }

func (k *memKV) GetKV(key string, v interface{}) (bool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	raw, ok := k.m[key]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(raw, v)
}

func (k *memKV) SetKV(key string, v interface{}) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	k.m[key] = raw
	return nil
}

const testSetupKey = "0123456789abcdef0123456789abcdef"

func TestSetupNeedsSetupKeyAndOnlyOnce(t *testing.T) {
	a, _ := LoadAccounts(newMemKV(), testSetupKey)
	if !a.SetupRequired() {
		t.Fatal("fresh store must require setup")
	}
	if _, err := a.Setup("wrong", "admin", "password123", ""); err != ErrBadSetupKey {
		t.Fatalf("wrong setup key accepted: %v", err)
	}
	if _, err := a.Setup(testSetupKey, "admin", "short", ""); err != ErrPasswordTooShort {
		t.Fatalf("short password accepted: %v", err)
	}
	if _, err := a.Setup(testSetupKey, "a b", "password123", ""); err != ErrInvalidUsername {
		t.Fatalf("username with space accepted: %v", err)
	}
	var wg sync.WaitGroup
	results := make([]error, 4)
	for i := range results {
		wg.Add(1)
		go func(i int) { defer wg.Done(); _, results[i] = a.Setup(testSetupKey, "admin", "password123", "") }(i)
	}
	wg.Wait()
	okCount := 0
	for _, err := range results {
		if err == nil {
			okCount++
		} else if err != ErrSetupDone {
			t.Fatal(err)
		}
	}
	if okCount != 1 {
		t.Fatalf("concurrent setup created %d admins", okCount)
	}
}

func TestLoginSessionPersistsAndPasswordChangeRevokesOthers(t *testing.T) {
	kv := newMemKV()
	a, _ := LoadAccounts(kv, testSetupKey)
	first, err := a.Setup(testSetupKey, "Admin", "password123", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Login("admin", "wrong-pass", ""); err != ErrBadCredentials {
		t.Fatal("wrong password accepted")
	}
	if _, err := a.Login("nobody", "password123", ""); err != ErrBadCredentials {
		t.Fatal("wrong username accepted")
	}
	second, err := a.Login("admin", "password123", "") // 用户名不区分大小写
	if err != nil {
		t.Fatal(err)
	}
	// 重启后会话仍然有效
	b, _ := LoadAccounts(kv, testSetupKey)
	if u, ok := b.ValidateSession(second); !ok || u != "Admin" {
		t.Fatal("session lost across restart")
	}
	if _, ok := b.ValidateSession("forged"); ok {
		t.Fatal("unknown token accepted")
	}
	if err := b.ChangePassword(first, "wrong", "newpassword1"); err != ErrBadCredentials {
		t.Fatal("password changed without the current one")
	}
	if err := b.ChangePassword(first, "password123", "newpassword1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := b.ValidateSession(first); !ok {
		t.Fatal("current session must survive its own password change")
	}
	if _, ok := b.ValidateSession(second); ok {
		t.Fatal("other sessions must be revoked after password change")
	}
	if _, err := b.Login("admin", "password123", ""); err == nil {
		t.Fatal("old password still works")
	}
	b.Logout(first)
	if _, ok := b.ValidateSession(first); ok {
		t.Fatal("logout did not revoke the session")
	}
}

func TestSessionExpiresAndRenews(t *testing.T) {
	a, _ := LoadAccounts(newMemKV(), testSetupKey)
	now := time.Now()
	a.now = func() time.Time { return now }
	tok, _ := a.Setup(testSetupKey, "admin", "password123", "")
	now = now.Add(SessionTTL - time.Hour) // 快到期:使用一次就顺延
	if _, ok := a.ValidateSession(tok); !ok {
		t.Fatal("session expired early")
	}
	now = now.Add(SessionTTL - 2*time.Hour)
	if _, ok := a.ValidateSession(tok); !ok {
		t.Fatal("active session was not renewed")
	}
	now = now.Add(SessionTTL + time.Hour)
	if _, ok := a.ValidateSession(tok); ok {
		t.Fatal("expired session accepted")
	}
}

func TestSeedFromEnvCreatesAndResets(t *testing.T) {
	a, _ := LoadAccounts(newMemKV(), "")
	if created, _, err := a.SeedFromEnv("ops", "password123", false); err != nil || !created {
		t.Fatal("env seed did not create the admin", err)
	}
	tok, _ := a.Login("ops", "password123", "")
	if created, reset, _ := a.SeedFromEnv("ops", "other-password", false); created || reset {
		t.Fatal("env seed must not overwrite an existing admin without reset")
	}
	if _, reset, err := a.SeedFromEnv("ops", "other-password", true); err != nil || !reset {
		t.Fatal("reset failed", err)
	}
	if _, ok := a.ValidateSession(tok); ok {
		t.Fatal("reset must revoke sessions")
	}
	if _, err := a.Login("ops", "other-password", ""); err != nil {
		t.Fatal("new password does not work")
	}
	// 没有初始化密钥时网页初始化不可用
	if a.CheckSetupKey("") {
		t.Fatal("empty setup key accepted")
	}
}

func TestMiddlewareRetiresAPIKeyAndRequiresSetup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ResetAuthFailures()
	defer ResetAuthFailures()
	a, _ := LoadAccounts(newMemKV(), testSetupKey)
	r := gin.New()
	r.Use(Middleware(Config{Accounts: a, Enabled: true, WhitelistPaths: DefaultWhitelist()}))
	api := r.Group("/api")
	RegisterRoutes(api, a, true)
	api.GET("/stats", func(c *gin.Context) { c.Status(200) })

	call := func(method, path string, body any, headers map[string]string) (int, map[string]any) {
		var buf bytes.Buffer
		if body != nil {
			_ = json.NewEncoder(&buf).Encode(body)
		}
		req := httptest.NewRequest(method, path, &buf)
		req.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		out := map[string]any{}
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}

	if code, out := call("GET", "/api/stats", nil, map[string]string{"X-API-Key": testSetupKey}); code != 401 || out["code"] != "SETUP_REQUIRED" {
		t.Fatalf("before setup the API key must not log in: %d %v", code, out)
	}
	if _, out := call("GET", "/api/auth/status", nil, nil); out["setupRequired"] != true {
		t.Fatal("status must report setup required")
	}
	code, out := call("POST", "/api/auth/setup", map[string]string{"setupKey": testSetupKey, "username": "admin", "password": "password123"}, nil)
	if code != 200 || out["token"] == "" {
		t.Fatalf("setup failed: %d %v", code, out)
	}
	tok := out["token"].(string)
	if code, _ := call("GET", "/api/stats", nil, map[string]string{SessionHeader: tok}); code != 200 {
		t.Fatal("session token rejected")
	}
	if code, out := call("GET", "/api/stats", nil, map[string]string{"X-API-Key": testSetupKey}); code != 401 || out["code"] != "API_KEY_RETIRED" {
		t.Fatalf("API key must be retired after setup: %d %v", code, out)
	}
	if code, _ := call("POST", "/api/auth/setup", map[string]string{"setupKey": testSetupKey, "username": "evil", "password": "password123"}, nil); code != 409 {
		t.Fatal("second setup must be refused")
	}
	if code, _ := call("POST", "/api/auth/login", map[string]string{"username": "admin", "password": "nope-nope"}, nil); code != 403 {
		t.Fatal("bad login must be refused")
	}
	if code, out := call("POST", "/api/auth/login", map[string]string{"username": "admin", "password": "password123"}, nil); code != 200 || out["token"] == "" {
		t.Fatal("good login refused")
	}
	if code, _ := call("POST", "/api/auth/logout", nil, map[string]string{SessionHeader: tok}); code != 200 {
		t.Fatal("logout failed")
	}
	if code, out := call("GET", "/api/stats", nil, map[string]string{SessionHeader: tok}); code != 401 || out["code"] != "INVALID_SESSION" {
		t.Fatal("logged-out session still works")
	}
}

func TestMain(m *testing.M) {
	bcryptCost = 4 // bcrypt.MinCost
	m.Run()
}
