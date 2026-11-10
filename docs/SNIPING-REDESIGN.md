# 抢购 / 预售 / 多账户 / TG 重构设计

> 状态:2026-11-10 已实施第 0-5 期,见文末「实施记录」。未提交、未发版。
> 依据:2026-11-10 下载的 EU/CA/US 官方 `/1.0/order.json` 与 EU `/1.0/dedicated/server.json`;
> 现有代码 `server/internal/purchase`、`catalog`、`handlers/telegram_*`、`web/src/routes/servers.tsx`。
> 约束不变:不跑真实凭据、不真实下单;结账登记、结果不明时暂停、停止记录这些保护全部保留。

## 1. 现在到底哪里有问题

| # | 现象 | 代码里的原因 | 处理 |
|---|---|---|---|
| A1 | 有货却没下单(手动任务) | 任务只在自己的 `retryInterval` 到点查库存,手动任务默认 60 秒;没被监控订阅覆盖的型号要等将近一分钟才发现补货 | **已修**:`purchase/stock_watch.go` 按「账户+型号」每 2 秒公共探测,结果直接投递到所有同型号任务 |
| A2 | 有货却没下单(任务忙时) | 任务在建车/预装时收到的信号,等它回到等待时已超过 1 秒,被直接丢弃,任务继续睡满重试间隔 | **已修**:过期信号也立即唤醒并重新查库存,只有新鲜信号才直接复用 |
| A3 | 发现补货慢 | 库存接口 `GET /dedicated/server/datacenter/availabilities` 官方标注 `noAuthentication=true`,我们却每次签名(要时钟校准、SHA1),也占用了鉴权调用额度 | 第 1 期:改为不签名调用,并带 `datacenters=` 过滤缩小响应 |
| A4 | 结账前多一次 DB 读 | 每轮开头 `GetKV(queue-checkout)`,紧接着结账前 `ClaimQueueOrder` 又原子检查同一个键 | 第 1 期:同一任务会话内首次确认后不再重复读;登记(写)保留,这是防重复下单的底线 |
| B1 | 预售逻辑难以判断对错 | 预售状态分散在 5 个文件:空车 `emptyCart`、半成品车 `partialCart`、成品车 `preparedCart`、报价快照 `saleSnapshot`、`pinnedFQN` 绕过库存;每处都有「等目录」分支,同一种 OVH 4xx 在不同位置有不同含义 | 第 2 期:改成单一状态机(见 §3) |
| B2 | 预售校验不走官方预检 | 现在用自写 `orderterm.VerifyCart` + 报价快照拼凑「能不能下」;官方有 `GET /order/cart/{cartId}/checkout`(PRODUCTION,「Get prices and contracts information」,不创建订单) | 第 2 期:用它做结账前的最终预检 |
| B3 | 预售开售信号只有一条 | 只靠每账户一辆空车轮询 `GET /order/cart/{id}/eco`(需鉴权,车过期要重建) | 第 2 期:加入官方免鉴权信号做先导,鉴权车只做最终确认 |
| C1 | 多账户下单要先切全局账户 | 抢购对话框的下单账户 = 侧栏全局账户,不能在对话框里选,也不能一次给多个账户下 | 第 3 期 |
| C2 | 跨区账户误下 | 型号目录分区(EU/CA/US),对话框只按全局账户查库存,不提示其他账户能不能买 | 第 3 期:按账户分区直接标注可买/不可买 |

## 2. 官方接口依据(三区一致)

| 用途 | 接口 | 状态 | 鉴权 |
|---|---|---|---|
| 实时库存(按 FQN/机房) | `GET /dedicated/server/datacenter/availabilities` 参数 planCode、datacenters、memory、storage、systemStorage、gpu、server | PRODUCTION | **免鉴权** |
| 库存枚举 | `1H-low/1H-high/24H/72H/120H/240H/480H/720H/1440H/2160H` 可下单;`comingSoon`、`unavailable`、`unknown` 不可 | — | — |
| 官网格式化目录 | `GET /order/catalog/formatted/eco` 参数 ovhSubsidiary、planCodes、datacenters、availability… 返回 `availabilityByDatacenter`(available/comingSoon/unavailable)、`availabilityFqns` | PRODUCTION | **免鉴权** |
| 公共目录 | `GET /order/catalog/public/eco` | PRODUCTION | 免鉴权 |
| 账户可售报价 | `GET /order/cart/{cartId}/eco`、`GET /order/cart/{cartId}/eco/options?planCode=` 返回 `prices[]`(duration、pricingMode、price、capacities…) | PRODUCTION | 需鉴权 |
| 建车/加购 | `POST /order/cart`、`POST /order/cart/{id}/assign`、`POST /order/cart/{id}/eco`、`POST …/eco/options`、`POST …/item/{itemId}/configuration`、`GET …/requiredConfiguration` | PRODUCTION | 需鉴权 |
| 结账预检 | `GET /order/cart/{cartId}/checkout`(价格与合同,不下单) | PRODUCTION | 需鉴权 |
| 结账 | `POST /order/cart/{cartId}/checkout` | PRODUCTION | 需鉴权 |

`/order/cart/{id}/baremetalServers`、`/dedicated` 都是 BETA,本设计不用。
格式化目录的数据有多大延迟官方没写,所以只用作「先导信号」,永远不能单凭它下单。

## 3. 预售新设计:一个任务一台状态机

```
 Watching ──(先导信号或定时)──▶ Listed ──▶ Built ──▶ Armed ──(触发)──▶ Submitted
    ▲                            │          │         │                   │
    └──────── 价格/选项消失 ◀──────┴──────────┴─────────┘          结果不明 → Uncertain(暂停)
```

| 状态 | 进入条件(只认官方接口) | 本状态做什么 | 出错怎么走 |
|---|---|---|---|
| Watching | 新建预售任务 | 等信号,不建车 | — |
| Listed | 账户车 `GET …/eco` 出现该 planCode,且所选周期有价格;`…/eco/options` 中所选每个选项都有同币种、同周期价格 | 读取报价,确定 duration/pricingMode | 价格消失 → 回 Watching |
| Built | Listed 后建车、assign、加基础商品、按顺序设 `dedicated_datacenter → dedicated_os → region`、加选项、可选优惠码 | 一辆车只对应一个(账户, 型号, 机房, 硬件, 周期)意图 | 明确 4xx → 销毁车、回 Watching,不写失败历史;网络错误 → 退避重试 |
| Armed | `GET …/checkout` 预检通过:商品、选项、周期与意图一致,金额有效 | 生成一次性 PreparedCall,定期续检(车过期前 1 分钟重建) | 预检不一致 → 回 Built 重建 |
| Submitted | 触发条件满足 + 结账登记成功 | 发出 `POST …/checkout` 一次 | 明确拒绝 → 释放登记回 Watching;408/409/429/5xx/网络 → Uncertain,暂停等人工核对 |

**触发条件**(用户在任务上二选一,默认第一种):

1. **开售即下**:到达 Armed 即结账(OVH 会显示「预订」,交付按官方排期)。这是现在的行为。
2. **有货再下**:Armed 后等库存接口在目标机房出现可下单枚举值才结账。

**信号源分层**:
- 先导(免鉴权、便宜):格式化目录的 `availabilityByDatacenter` 从 comingSoon 变化、库存接口 FQN 首次出现 —— 只用来把轮询从 15 秒提到 2 秒。
- 确认(鉴权):账户车的 `/eco` 报价。每账户一辆发现车,所有任务共享。
- 预检(鉴权):`GET …/checkout`,只对自己的车。

代码结构:新建 `purchase/preorder/`,包含 `state.go`(状态机与转移表)、`signals.go`(三层信号)、`build.go`(建车,复用现有 `prepareCart` 中与普通抢购共用的步骤)。`PurchaseServer` 对预售任务只调用状态机的 `Step()`,不再在主流程里穿插预售分支。现有 `emptyCart/partialCart/pinnedFQN` 删除。

## 4. 下单提速(普通抢购)

1. 已完成:共享 2 秒库存探测,以及过期信号也唤醒(A1/A2)。
2. 库存接口免签名,并加 `datacenters` 过滤(A3)。
3. 会话内不重复读结账登记(A4)。
4. 补货信号到达时如果车还没预装好,同时触发「预装」和「再查一次库存」,不再串行。
5. 不动的部分:结账登记写库、结账请求只发一次、结果不明暂停、PreparedCall 一次性。

## 5. 多账户快速选择

**抢购对话框**
- 顶部是「下单账户」多选芯片,每个芯片显示账户名、区域旗标,以及该型号在该账户分区的实时状态(可买 / 即将开售 / 不同区不可买)。不同区的账户置灰并说明原因。
- 默认选中:上次在该型号用过的账户 → 全局账户 → 默认账户。
- 选多个账户时,按账户扇出任务,共用一个 `OrderGroup` 上限,做到「总共只买 N 台,哪个账户先抢到算哪个」(复用现有组配额与结账登记)。
- 键盘:数字键 1-9 切换对应账户,回车提交。

**监控/队列页**
- 账户筛选芯片(全部 / 各账户),任务行显示账户芯片,点击直接筛选。

**TG**
- `/buy`、`/watch` 在有多个可用账户时,第一步给出账户按钮(只列同区账户,上次用过的排第一,另有「全部同区账户」)。
- `/use 账户` 设置本会话默认账户,之后省略这一步。

## 6. TG 交互增强

- 补货通知带「立即抢 · 机房」按钮,一键建任务(仍需二次确认数量;程序不自动付款)。
- `/queue` 每个任务带「暂停 / 恢复 / 删除」按钮,操作后**编辑原消息**,不再刷新消息。
- 抢购成功消息带「打开订单 / 付款页」链接和「暂停同组其余任务」按钮。
- 结果不明消息带「我已核对:有订单 / 没有订单,恢复任务」按钮,分别对应保持暂停和恢复。
- 注册命令菜单补上 `/buy`、`/use`、`/pause`、`/resume`。
- 所有写操作按钮继续经过 `IsAuthorizedActor` 与限流;按钮回调数据不超过 64 字节,用短令牌。

## 7. 「你中奖了」卡片

现状:两块大黑底等宽代码块并排,字段多为「—」,信息密度低,和页面风格不一致。

改为:
- 左侧一张自绘 SVG 插画(礼盒打开、芯片升级,跟随明暗主题配色),右侧标题「你中奖了!」加一句「实际交付优于订购:CPU」。
- 下方一张「订购 → 实际」对照表,只列有差异的字段,升级的字段高亮并标「↑ 升级」;没有数据的字段折叠,不显示一排「—」。
- 保留现有彩纸动画,只播放一次,并支持减少动态效果的系统设置。

## 8. 分期与验证

| 期 | 内容 | 风险 |
|---|---|---|
| 0 | A1/A2(已完成,待补测试) | 低 |
| 1 | A3/A4 + §4 提速 | 低,不改语义 |
| 2 | 预售状态机(§3) | **高**,替换真实结账路径;只用模拟 OVH 响应做全流程测试 |
| 3 | 多账户选择(§5) | 中,涉及组配额 |
| 4 | TG(§6) | 中 |
| 5 | 中奖卡片(§7) | 低,纯前端 |

每期验证:`go test -race -short ./... && go vet ./...`、无 CGO 核心测试、`npm test && npm run build`;前端改动用模拟数据在浏览器检查。不跑真实凭据,不真实下单。

## 9. 实施记录(2026-11-10)

| 期 | 已做 | 有意没做 |
|---|---|---|
| 0 | `purchase/stock_watch.go` 每账户/型号 2 秒共享探测;过期信号也唤醒任务 | — |
| 1 | `ovh.GetAvailabilities` 免签名读库存(4 处调用);同一任务会话不重复读结账登记 | 库存请求加 `datacenters` 过滤:共享探测按型号合并多个机房,过滤反而要多发请求 |
| 2 | `purchase/preorder.go` 收拢预售判断;新增「有货再下」(`preorderWaitStock`,入库、接口、界面) | `GET /order/cart/{id}/checkout` 硬闸门:无法用真实账户确认它对预售车的返回,贸然加在结账路径上可能让预售永远不触发;先保留现有逐项 `VerifyCart`。格式化目录先导信号同理暂不接入 |
| 3 | `account_ids` 多账户入队,共享数量上限(按机房模式每机房一份);对话框账户多选、同区限制、数字键、记住上次选择 | — |
| 4 | `/queue` 暂停/恢复按钮并原地编辑消息;结果不明通知带「确认没有订单,恢复任务」;状态与预售方式中文显示 | `/use` 默认账户:现有 `/watch` 流程已逐次选账户,补货通知已有按账户的一键下单按钮 |
| 5 | 中奖卡片重做:自绘 SVG 插画、只列差异部件、涨幅标签、一次性动效、减少动态时关闭 | — |

## 10. 参考:coolci/OVH-BUY 的下单思路

本项目与它同源(相同的任务/配置狙击/TG 下单结构)。它的 `purchase_server` 是每轮串行一条龙:
查库存 → 建车 → `/eco` 加购(固定 P1M) → 静态表推 region(含 `usa`/`apac`) → 配置 → 选项 → assign → summary → checkout,
带 `waiveRetractationPeriod: true`。队列每秒扫描、每批 10 个并发,配置狙击 60 秒轮询。

和本项目的差别(本项目的做法已在前几个版本修正):
- 它把 `comingSoon` 当有货(只排除 unavailable/unknown);本项目只认 `\d+H` 可下单枚举。
- 它每次都从零建车;本项目等货期间预装购物车,发现库存后只剩结账一跳。
- 它的 region 静态表在美区会发出目录里不存在的值;本项目按目录/`requiredConfiguration` 取。
- 它结账时放弃 14 天撤回权;本项目有测试禁止发送该字段。
- 它没有结账登记,网络错误后会重试结账,可能重复下单;本项目结果不明即暂停。

## 11. 第二轮核对与提速(2026-11-10)

接口用法:对照 EU/US 官方 `order.json`,建车 `order.cart.Creation`(仅 ovhSubsidiary 必填)、加购 `GenericProductCreation`、选项 `GenericOptionCreation`(含 itemId)、配置 `ItemConfigurationCreation`(label/value)、优惠码 `CouponCreation`、结账 `order.cart.Checkout`(两个字段均非必填,不发 waiveRetractationPeriod)全部与代码一致,未发现错误用法。

| 改动 | 省下的等待 |
|---|---|
| `orderterm.VerifyCart` 同层项目并行读取(先依次签名,再并发发送) | 预售开售时 N 个选项的 N 次串行读取变为 1 次 |
| 预售非月付:建车时直接用销售探测几秒内的 `/eco` 报价确定周期与计价方式 | 1 次往返 |
| `readSaleSnapshot` 同时读取基础与选项报价 | 1 次往返 |
| 共享库存探测跟随监控间隔(最快 1 秒,不慢于 2 秒) | 平均发现延迟最多减半 |

未改:配置项与选项写入仍串行(同一购物车并发写的行为没有官方保证,失败即丢单);结账前登记仍写库。

## 12. 第三轮:结账结果判定、耗时与 TG 操作(2026-11-10)

**结账结果判定(稳)**
- 删除自动付款:独服与 VPS 结账恒为 `autoPayWithPreferredPaymentMethod:false`;旧页面带 `autoPay:true` 返回 `AUTO_PAY_REMOVED`,`false` 照常接受。
- 「未发出」与「不明」分开:依据 `httptrace.WroteRequest`,请求未完整写出就失败(代理/DNS/建连/写入错误)时 OVH 不可能收到,释放登记、同一辆购物车重新签名,2 秒内或下一次库存信号重试;写出过或拿到任何 HTTP 响应(含超时、5xx、429)仍按不明处理并暂停。
- 不明结果自动核对(只读):`GET /me/order?date.from=` → `/details` → `/details/{id}/extension`(EU/CA,BETA;US 无此接口),按型号 + `dedicated_datacenter` + 周期匹配。只有恰好一笔未记到其他任务名下的订单才把任务标为已下单;多笔、US、读不到或 5 分钟内未找到时保持暂停并把看到的订单号告诉用户。用户在核对期间恢复任务即放弃核对。明细与扩展的读取并发执行。

**抢购耗时**
- 历史记录新增 发现库存→结账写出(微秒)、是否预装/复用连接、从建任务到抢到;网页「抢购历史」与 TG 成功通知同时显示。

**TG**
- `/menu` 按钮菜单(总览/任务/订阅/最近订单/抢购速度/监控开关),原地翻页;`/subs` 每条可暂停/恢复/删除;`/queue` 可删除单个任务与全部暂停;`/recent` 未付款订单带订单页链接;`/speed`、`/monitor on|off`、`/pauseall`。删除需二次确认;长目标用 30 分钟短令牌,按钮数据不超过 64 字节。

**并发原则**:只读查询并发(购物车项目核对、订单核对、销售报价、库存探测);同一购物车的写入(加购、配置、选项、结账)保持串行。

## 13. 第四轮:探测扩展性与限流处理(2026-11-10)

实测(本机到 EU,公开接口无凭据):单型号库存 2 字节、约 1.0–1.5 秒往返;全量库存未压缩 4.3 MB(20–25 秒),gzip 后 138 KB(约 2 秒)。Go 默认已请求 gzip,本项目 transport 未关闭压缩。按型号探测仍是最优形态。

| 改动 | 原因 |
|---|---|
| 库存探测按「大区 + 型号」合并,结果投递给同区所有账户;失败时轮换同区下一个账户出站 | 库存接口免鉴权,同区账户数据相同;多账户一起抢时请求不再按账户翻倍,单个坏代理不阻断探测 |
| 固定节拍发出探测,最多 2 个请求重叠,按发出时间丢弃迟到的旧结果 | 往返约 1 秒时,原来「等回复再等间隔」的周期是 3 秒;现在回到 2 秒(或监控间隔),请求速率不变 |
| 查库存的暂时性错误(网络/429/5xx)不再写失败历史,2 秒内或下一次信号重试 | 查库存不是下单;以前会把任务在历史里标成失败并覆盖上一次真实结果 |
| 结账 429 且为 EU/CA 账户:结账后满 1 分钟账户内没有任何新订单才自动恢复任务 | 抢货高峰 429 常见,原来一律暂停到人工处理;仍不把 429 直接当拒单,其它不明结果(超时、5xx)维持暂停 |
