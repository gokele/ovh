//go:build !windows

package updater

import (
	"fmt"
	"os"
	"syscall"
)

// Install 用新文件覆盖正在运行的自己。
//
// Unix 上 rename 是原子的,而且允许覆盖正在执行的文件:内核按 inode 引用可执行文件,
// 旧进程会继续用旧 inode 跑到退出为止,新进程启动时拿到的才是新文件。
// 所以这里不需要先备份再改名那一套。
func Install(tmpPath, exePath string) error {
	// 保留原来的权限位(比如有人特意设了 setgid 或更严格的 750)
	if fi, err := os.Stat(exePath); err == nil {
		_ = os.Chmod(tmpPath, fi.Mode().Perm())
	} else {
		_ = os.Chmod(tmpPath, 0o755)
	}
	if err := os.Rename(tmpPath, exePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("替换二进制失败: %w", err)
	}
	return nil
}

// Restart 用新二进制替换当前进程映像。
//
// 走 execve 而不是"起个新进程再退出":execve 保持同一个 PID、同一套 fd,
// 对 systemd / supervisor / docker 这些按 PID 管理的场景是无感的 ——
// 换成 fork+exit 的话,进程管理器会认为服务挂了,可能触发重启策略甚至告警。
//
// 调用前必须由调用方关掉监听端口和数据库:execve 之后当前进程的一切都被替换掉,
// 但已打开的 fd 默认会继承过去,不关的话新进程会撞到"端口已被占用"。
func Restart(exePath string) error {
	if err := syscall.Exec(exePath, os.Args, os.Environ()); err != nil {
		return fmt.Errorf("重启失败: %w", err)
	}
	return nil // execve 成功的话根本不会走到这里
}
