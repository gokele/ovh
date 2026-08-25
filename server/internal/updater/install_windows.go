//go:build windows

package updater

import (
	"fmt"
	"os"
	"os/exec"
)

// Install Windows 不允许覆盖正在运行的 exe(文件被映射时加了共享写锁),
// 所以只能先把自己改名让路 —— 改名对运行中的 exe 是允许的 ——
// 再把新文件放到原来的位置。旧文件留到下次启动时由 CleanupStale 删掉。
func Install(tmpPath, exePath string) error {
	backup := exePath + ".old"
	_ = os.Remove(backup)
	if err := os.Rename(exePath, backup); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("重命名当前程序失败(是否被杀毒软件锁定?): %w", err)
	}
	if err := os.Rename(tmpPath, exePath); err != nil {
		// 放不回去就把自己改回来,别让用户落得一个没有可执行文件的目录
		_ = os.Rename(backup, exePath)
		os.Remove(tmpPath)
		return fmt.Errorf("替换二进制失败,已回滚: %w", err)
	}
	return nil
}

// Restart Windows 没有 execve,只能起一个新进程再让自己退出。
// 新进程继承当前工作目录与环境变量;端口由调用方在此之前关闭。
func Restart(exePath string) error {
	cmd := exec.Command(exePath, os.Args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动新版本失败: %w", err)
	}
	// 不 Wait:让新进程脱离,当前进程随后退出
	os.Exit(0)
	return nil
}
