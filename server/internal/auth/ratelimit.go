package auth

import (
	"sync"
	"time"
)

// 密钥错误的限流。
//
// 为什么需要:这个服务能用你的 OVH 账户下单、能重装服务器,而单二进制部署时
// LISTEN_HOST 默认是空(监听所有网卡)。在这之前,端口可达的人可以**不限次数**
// 地猜 API 密钥 —— 一秒几千次,没有任何阻力,日志里也只是多几行 401。
//
// 同项目的 telegram/actor.go 早就有同样形状的限流器,只是鉴权这条路上一直没有。
//
// 设计取舍:
//   - 按客户端 IP 计数。Gin 的 ClientIP() 在 SetTrustedProxies 配好后给的是真实来源。
//   - **成功即清零**:打错一次密钥的正常用户不该被后续惩罚。
//   - 窗口内超限就一直拒到窗口结束,不做指数退避 —— 够用,且行为好解释,
//     能给用户一句确定的"X 秒后再试"。
//   - 定期清理过期条目,避免被大量来源 IP 撑大内存。
const (
	// MaxAuthFailures 一个窗口内允许的密钥错误次数。
	// 给到 10 次是照顾手打密钥的人(尤其手机上),而对爆破来说
	// 10 次/5 分钟已经把可行性打没了。
	MaxAuthFailures = 10
	// AuthFailureWindow 计数窗口,同时也是超限后的冷却时长。
	AuthFailureWindow = 5 * time.Minute
	// maxTrackedIPs 跟踪的 IP 上限,超过就先清理过期条目。
	maxTrackedIPs = 4096
)

type failBucket struct {
	count       int
	windowStart time.Time
}

var (
	failMu   sync.Mutex
	failByIP = map[string]*failBucket{}
)

// authBlocked 这个 IP 是否已被挡下。第二个返回值是还要等多久。
func authBlocked(ip string, now time.Time) (bool, time.Duration) {
	if ip == "" {
		ip = "unknown"
	}
	failMu.Lock()
	defer failMu.Unlock()
	b, ok := failByIP[ip]
	if !ok || now.Sub(b.windowStart) >= AuthFailureWindow {
		return false, 0
	}
	if b.count >= MaxAuthFailures {
		return true, AuthFailureWindow - now.Sub(b.windowStart)
	}
	return false, 0
}

// recordAuthFailure 记一次密钥错误。
func recordAuthFailure(ip string, now time.Time) {
	if ip == "" {
		ip = "unknown"
	}
	failMu.Lock()
	defer failMu.Unlock()
	if len(failByIP) >= maxTrackedIPs {
		for k, v := range failByIP {
			if now.Sub(v.windowStart) >= AuthFailureWindow {
				delete(failByIP, k)
			}
		}
	}
	b, ok := failByIP[ip]
	if !ok || now.Sub(b.windowStart) >= AuthFailureWindow {
		failByIP[ip] = &failBucket{count: 1, windowStart: now}
		return
	}
	b.count++
}

// clearAuthFailures 密钥正确时清零 —— 打错一次的正常用户不该被后面的请求连坐。
func clearAuthFailures(ip string) {
	if ip == "" {
		ip = "unknown"
	}
	failMu.Lock()
	delete(failByIP, ip)
	failMu.Unlock()
}

// ResetAuthFailures 仅供测试:清空全局状态,避免用例之间互相污染。
func ResetAuthFailures() {
	failMu.Lock()
	failByIP = map[string]*failBucket{}
	failMu.Unlock()
}
