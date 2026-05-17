package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Paths 数据/缓存/日志目录
type Paths struct {
	DataDir  string
	CacheDir string
	LogsDir  string
}

// DefaultPaths 从环境变量读取，否则用当前工作目录下的相对路径。
// 默认全部落在 ./data 下：
//   data/         SQLite (sniper.db)
//   data/cache/   OVH catalog 等缓存文件
//   data/logs/    运行日志
//
// 行为：在哪儿执行命令，data/ 就出现在哪儿（Windows / Linux / macOS 一致）。
// 想固定到别的位置就设 DATA_DIR / CACHE_DIR / LOGS_DIR 环境变量。
func DefaultPaths() Paths {
	dataDir := envOr("DATA_DIR", "data")
	return Paths{
		DataDir:  dataDir,
		CacheDir: envOr("CACHE_DIR", filepath.Join(dataDir, "cache")),
		LogsDir:  envOr("LOGS_DIR", filepath.Join(dataDir, "logs")),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// EnsureDirs 创建必要的目录
func (p Paths) EnsureDirs() error {
	for _, d := range []string{p.DataDir, p.CacheDir, p.LogsDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", d, err)
		}
	}
	return nil
}

// File 返回数据目录下的文件绝对路径
func (p Paths) File(name string) string {
	return filepath.Join(p.DataDir, name)
}

// CacheFile 返回缓存目录下的文件绝对路径
func (p Paths) CacheFile(name string) string {
	return filepath.Join(p.CacheDir, name)
}

// LogFile 返回日志目录下的文件绝对路径
func (p Paths) LogFile(name string) string {
	return filepath.Join(p.LogsDir, name)
}

// 文件级互斥：避免多 goroutine 同时写同一文件
var fileLocks sync.Map

func lockFor(path string) *sync.Mutex {
	v, _ := fileLocks.LoadOrStore(path, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// ReadJSON 读 JSON 文件到 v；文件不存在或为空时不报错且不修改 v
func ReadJSON(path string, v interface{}) error {
	m := lockFor(path)
	m.Lock()
	defer m.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, v)
}

// WriteJSON 原子写 JSON 文件（先写 tmp 再 rename，避免崩溃半写）
func WriteJSON(path string, v interface{}) error {
	m := lockFor(path)
	m.Lock()
	defer m.Unlock()

	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// FileExists 是否存在
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
