# 抢购链路与延迟测量

本优化适用于独立服务器 / Eco 抢购队列。VPS 购买使用另一条工作流,不共用这份预装购物车。预装只创建、绑定及配置购物车,不创建订单、不扣款,也不预留库存。

## 执行链路

1. 运行任务持有独立 SDK 客户端,复用该任务已缓存的时钟校准和账户出站连接池。每个任务持有自己的购物车,不同机房、不同数量不会共用一辆车。
2. 在等待库存期间完成商品、机房、OS、region、硬件和优惠码配置。优惠码明确被拒绝时保留原来的不使用该码下单及提示规则。
3. URL、正文、静态请求头、签名前缀提前构造。结账发送时只更新时间戳和 SHA1 签名,每份预构造请求只允许发送一次。
4. 同账户、同型号的并发库存查询合并,最多复用 100ms 的只读观察,保留最初检测时间。来自监控的原始库存会在目录处理、验价及消息通知之前直达队列任务的 channel 信箱。
5. 已预装的无货任务可被库存信号立即唤醒,不等下一次重试定时器。最多 64 个独立任务同时执行,覆盖单次最大 60 个任务,没有原来每 10 个任务之间的整批等待。
6. 监控已验价的配置直接整批入队,不再经本机 HTTP 和重复验价。新任务先持久化才能执行;已有任务的发送热路径不访问数据库。
7. 日志、历史记录、队列终态及通知在请求发出之后处理,不会把非必要操作放在发送之前。订单成功记录仍可靠保存,不为降低数字而取消关键结果持久化。

## 连接与生命周期

- 每账户连接池允许 64 条空闲连接,启用 TCP keepalive 和 TLS 会话缓存。HTTP/2 可在同一条连接上并行发送多个请求。
- 仅在有运行任务时,每 20 秒为相关账户发送一次无鉴权 `GET /auth/time` 轻量请求,复用同一代理及 transport。空队列不维持探活流量。
- OVH 或代理仍可能关闭连接。记录会标明是否真正复用连接,不承诺远端永不关闭连接。
- 根据官方 `order.cart.Cart.expire` 提前一分钟失效准备,最多缓存 30 分钟;未返回有效期限时使用 5 分钟保守期限。失效、凭据/代理变更和暂停会释放旧准备。
- 预装失败不计作真实下单失败,不写失败订单历史,无货期间最多每 30 秒再次尝试准备。有货后允许走完整准备作为降级路径。
- 暂停、删除和完成任务不会被迟到库存信号恢复。结账已经发出时继续等待结果;未知传输/响应错误按不确定结果处理。并发同账户、同机型的请求不会猜用其它任务的订单。

## 指标

`GET /api/queue/timings` 的 `timings` 现在以任务 ID 为键。每次实际结账增加:

```json
{
  "dispatch": {
    "wroteRequest": true,
    "detectionToWriteUs": 250,
    "connectionReused": true,
    "prewarmed": true
  }
}
```

检测时间到 `net/http/httptrace.WroteRequest` 成功回调之间的耗时以微秒记录。回调只更新内存原子变量,不做磁盘或通知操作。未观察到成功写入时 `wroteRequest=false`,不把零值当作性能结果。队列界面显示“探测→写出”,提示区说明是否预装及复用连接;日志带任务 ID 和账户 ID。

`< 1ms` 是预装命中且连接可用时的本地派发目标,不是公网延迟或抢购成功承诺。初次下单、首次自动创建任务、目录等待、准备失效、重连、操作系统调度和代理都可能使该数字变大。保留原检测时间,不会在发请求前重置起点来掩盖等待。

监控检查间隔可选择 1–3600 秒,默认仍为 5 秒。轮次按起点计算间隔,不再固定叠加“请求耗时 + 检查间隔”。OVH 的库存缓存和限流仍会影响实际探测频率。

## 部署与验证

账户必须继续使用所属的 EU / CA / US API 站点,不能为了就近切换到不兼容的站点。选择部署位置时,在候选 VPS 上测相同 API 及相同代理出口的 RTT;控制台所在机房、OVH API 站点和目标服务器的库存机房是三个不同概念。迁移服务还需要实际部署环境和访问权限,代码优化不会自动搬迁现有实例。

只用临时数据库、离线 transport 或本机假 API 做测试,不向 OVH 发真实结账:

```sh
cd server
go test -race -short ./...
go vet ./...
go test ./internal/ovh ./internal/purchase -run 'TestPreparedCheckoutLocalWriteLatency|TestQueueSignalDispatchesPrebuiltCheckoutImmediately' -count=1 -v
go test ./internal/ovh -run '^$' -bench '^BenchmarkPreparedCheckout$' -benchtime=200x
```

本机 TCP 测试的分位数用于验证代码路径,不能作为跨地区生产环境的性能证明。生产环境用实际 `dispatch` 记录检查预装命中率、连接复用及写出耗时。

# Checkout latency (English)

This path covers dedicated/Eco queue tasks. VPS purchasing is separate. Prebuilding a cart neither creates/pays for an order nor reserves stock.

Running tasks reuse their isolated SDK client, cached clock calibration and account-specific transport. Cart configuration and request serialization happen while waiting for stock. Only the current timestamp and OVH SHA1 signature are changed at dispatch. A prepared checkout can be sent once.

Concurrent stock probes for the same account and plan are coalesced, with at most 100ms observation reuse. Original detection timestamps reach waiting tasks through bounded channels before monitoring performs catalog processing, price checks or notifications. Up to 64 independent workers replace batches of 10. Validated monitor orders enter the shared queue directly, preserving durable enqueue and stop/duplicate checks.

The idle pool supports 64 connections per host, TCP keepalive and TLS session caching. Every 20 seconds, accounts with running tasks receive a lightweight unauthenticated `/auth/time` request through the same transport/proxy. Remote servers can still close connections. Prepared carts expire one minute before OVH's `expire`, capped at 30 minutes, or after 5 minutes if no expiry is available. Pausing or changing credentials/proxy invalidates preparation. Failed prebuilds do not count as failed orders and wait 30 seconds before another idle preparation attempt.

`/api/queue/timings` is keyed by task ID. Its optional `dispatch` reports successful `httptrace.WroteRequest` observation, original-detection-to-write microseconds, connection reuse and whether the cart existed before stock detection. Logs include task/account IDs. Critical result persistence is retained after dispatch; uncertain checkout errors do not authorize blind retries or assigning another concurrent task's order.

Sub-millisecond dispatch is a local warm-path target, not a public-network latency or purchase guarantee. Cold starts, newly created automatic tasks, catalog waits, expired preparation, reconnects, proxies and OS scheduling can be slower. Monitoring supports 1–3600-second intervals while keeping the 5-second default. API caching and rate limits still apply.

Deploy near the API region compatible with the account; do not switch API regions just because another endpoint is closer. Measure candidate hosts through the actual proxy route. Local TCP tests use fake APIs and do not establish real-world OVH performance. No running instance is automatically migrated by these code changes.
