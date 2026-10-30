# 项目约定

本项目沿用 Go/Gin + SQLite、Vite/React、TanStack Query/Router 和 shadcn/ui。范围是已有控制台的订单付款、监控和抢购任务修复,不迁移框架、身份验证或组件库。开发验证使用临时数据库和模拟 OVH 响应,不运行带真实凭据的后端、不创建真实订单或扣款。

## 已有订单付款

- `GET /api/ovh/account/orders/:order_id/payment?account=ID` 仅预检账户、金额、状态及可用于该订单的已绑定方式。
- `POST /api/ovh/account/orders/:order_id/pay?account=ID` 接收 `confirm: true`、`paymentMethodId`、`paymentType`、`amount`、`currency`。服务端重新读取预检,仅接受 `notPaid`、未过期且金额与方式一致的订单。
- EU/CA 使用 `paymentMethods` 和 `/pay` 的 `{paymentMethod:{id}}`;US 使用 `availablePaymentMethod` 和 `{paymentMethod:{id,type}}`。签名来源为三个站点的官方 `/1.0/me.json`,纳入 schemacheck 基线。
- 每账户/订单在 SQLite kv 原子登记后才发起一次请求。网络错误、408、409、429 和 5xx 保留登记;成功仅代表已提交。结果不明确时到 OVH 控制台核对,应用不清除登记后重发。明确 4xx 拒绝允许用户重新确认。
- 新增 SDK 工作流使用 `IsolatedClientFor`,独立 SDK/HTTP client 复用账户 transport,避免 go-ovh 1.5.1 并发改写 `http.Client.Timeout`。账户的并发详情读取同样隔离实例;其余既有 SDK 调用不在此次改动范围。
- 前端采用现有 Dialog/Select/Checkbox,核对账户、含税金额和方式后显式确认,不自动重试。列表绑定服务端实际账户 ID,不会跟随提交时的全局账户切换。

## 监控与停止语义

- 库存匹配只对带宽/vRack 放宽;RAM、存储及未知选项仍严格匹配。完整用户选项保留用于询价、安装费和实际入队。
- `lastRestockAt` 从该订阅保存的历史中读取最近 `unavailable → available` 记录,不把首次有货或价格接口恢复冒充补货。没有记录不猜测时间。关闭通知也保存跳变。
- 全局手动停止监控写入 kv `monitor-manually-stopped`,重启不自动恢复。停止使旧检查快照失效,迟到的自动入队请求也必须被拒绝。显式启动/新增订阅启动后解除全局记录。
- 暂停、删除、清空抢购任务以及 Telegram `/cancel` 都保存账户/型号/机房的自动建单停止记录。闸门在 QueueMu 内的 `EnqueueAutomaticItems`,不受 `skipDuplicateCheck` 绕过;手动恢复或新建任务才解除。旧版本已有暂停任务也受到保护。
- 监控与已入队抢购任务仍是独立生命周期。暂停监控不会取消已入队任务,页面说明必须保留。已发送的 OVH 结账请求可能已经成立,不能靠取消本地请求撤销。
- 旧版本在升级前已经删除的任务没有持久停止记录,无法从空队列推断取消意图;升级后应检查仍开启的监控自动下单配置。

## 验证

- `cd server && go test -race -short ./... && go vet ./...`
- `cd server && CGO_ENABLED=0 go test -short ./internal/db ./internal/app ./internal/handlers ./internal/monitor`
- `cd web && npm test && npm run build`
- 浏览器交互测试仅使用隔离的前端模拟数据;断言打开窗口不付款、确认前禁用、切换方式重置确认、提交一次后不可重复。
