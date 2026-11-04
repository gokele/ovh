package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ResolveAPIKey preserves configured keys and persists a random key for an unconfigured installation.
func ResolveAPIKey(dataDir, configured string) (string, bool, error) {
	if strings.TrimSpace(configured) != "" {
		return configured, false, nil
	}
	path := filepath.Join(dataDir, ".api-secret")
	deadline := time.Now().Add(time.Second)
	for {
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err == nil {
			key := make([]byte, 32)
			if _, err = rand.Read(key); err == nil {
				_, err = file.WriteString(hex.EncodeToString(key) + "\n")
			}
			if err == nil {
				err = file.Sync()
			}
			closeErr := file.Close()
			if err == nil {
				err = closeErr
			}
			if err != nil {
				_ = os.Remove(path)
				return "", false, fmt.Errorf("保存 API 密钥失败: %w", err)
			}
			return hex.EncodeToString(key), true, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", false, fmt.Errorf("创建 API 密钥失败: %w", err)
		}
		info, err := os.Lstat(path)
		if err != nil {
			return "", false, err
		}
		if !info.Mode().IsRegular() {
			return "", false, fmt.Errorf("API 密钥路径必须是普通文件: %s", path)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return "", false, err
		}
		value := strings.TrimSpace(string(body))
		key, err := hex.DecodeString(value)
		if err == nil && len(key) == 32 {
			if err := os.Chmod(path, 0600); err != nil {
				return "", false, err
			}
			return value, false, nil
		}
		// A concurrent first start may have created the file but not finished writing it.
		if time.Now().After(deadline) {
			return "", false, fmt.Errorf("API 密钥文件无效,请恢复 %s 或配置 API_SECRET_KEY", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
