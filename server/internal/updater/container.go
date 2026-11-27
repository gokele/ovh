package updater

import (
	"os"
	"strings"
)

// InContainer 当前是不是跑在容器里。
//
// 为什么要判这个：容器里的自更新是**错的**，而且错得很隐蔽。
//
//  1. 它会把新二进制写进容器的可写层，而不是镜像。容器一重建
//     （docker compose up -d、重启策略拉起、宿主机重启）就回到镜像里的旧版本 ——
//     用户"更新成功"了好几次，版本号却一直在变回去。
//  2. 本镜像的进程是降权跑的，多半根本写不进 /usr/local/bin，
//     于是更新会以一句看不懂的权限错误收场。
//  3. 就算写成功了，execve 换掉的是 PID 1，容器的健康检查和重启策略
//     会看到一个"换了个人"的主进程。
//
// 容器的更新方式只有一个：拉新镜像、重建容器。数据在卷上，不受影响。
func InContainer() bool {
	return inContainer(os.Getenv("OVH_IN_CONTAINER"), detectContainerRuntime)
}

func inContainer(flag string, detect func() bool) bool {
	// 我们自己的镜像里显式设了这个 —— 最可靠
	if v := strings.TrimSpace(flag); v != "" && v != "0" && !strings.EqualFold(v, "false") {
		return true
	}
	return detect()
}

func detectContainerRuntime() bool {
	// Docker 会在容器根目录放这个文件
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	// podman / containerd 等
	if _, err := os.Stat("/run/.containerenv"); err == nil {
		return true
	}
	// 最后看 cgroup。注意 cgroup v2 下这一条经常什么都看不出来，
	// 所以它只是兜底，不是主要判据。
	if b, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		s := string(b)
		for _, needle := range []string{"docker", "containerd", "kubepods", "lxc"} {
			if strings.Contains(s, needle) {
				return true
			}
		}
	}
	return false
}

// ContainerUpdateHint 容器里该怎么更新。给接口和界面用同一份说法。
const ContainerUpdateHint = "检测到运行在容器里，自更新已停用。\n\n" +
	"容器的更新方式是拉新镜像再重建容器：\n" +
	"  docker compose pull && docker compose up -d\n" +
	"或者：\n" +
	"  docker pull ghcr.io/gokele/ovh:latest\n" +
	"  docker rm -f ovh-console && docker run -d ... ghcr.io/gokele/ovh:latest\n\n" +
	"数据在挂载的卷上，重建容器不会丢。\n\n" +
	"为什么不能在容器里自更新：新二进制只会写进容器的可写层，" +
	"容器一重建就回到镜像里的旧版本 —— 你会看到「更新成功」之后版本号又变回去。"
