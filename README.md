# OVH 控制台

OVH 独立服务器 / VPS / Eco 系列**抢购 + 监控 + 管理**控制台。

实时检测 OVH 各数据中心库存,发现可购买的服务器时按用户配置(机房、内存、存储、带宽、vRack)自动下单。后台同时管理已购买服务器的全生命周期(重启 / 重装 / IPMI / BIOS / 启动模式 / 维护工单 / 联系人变更 / 带宽 / 防火墙 / FTP 备份 / vRack / Secondary DNS 等)。支持**多 OVH 账户**同时管理,抢购 / 监控按账户隔离。

> Go (Gin) + SQLite 后端、Vite/React + TanStack + shadcn-ui 前端、`//go:embed` 单二进制部署(自带 SQLite, 跨平台无依赖)、
> 强制 OvhCredsGate、多账户支持、双 SQLite driver(`modernc.org/sqlite` 纯 Go / `mattn/go-sqlite3` cgo, build tag 自动选)、
> 自动检测 GitHub Releases 更新。

## 下载

去 [Releases](https://github.com/gokele/ovh/releases) 拿对应平台的二进制,解压即用,**不需要装 Go、Node 或 SQLite**:

| 平台 | 文件 |
|---|---|
| Windows x64 | `ovh-server-windows-amd64.exe` |
| Linux x64 | `ovh-server-linux-amd64` |
| Linux ARM64(树莓派 / ARM 云主机) | `ovh-server-linux-arm64` |

前端已经用 `//go:embed` 嵌进二进制,跑起来直接开 `http://localhost:19998` 就是完整界面。
Linux 上记得 `chmod +x`。想自己编译见[部署方式](#部署方式)。

## v0.1.0 更新

### 在线更新：点一下自己换掉自己

仪表盘「系统版本」旁边有新版本时会出现「立即更新」按钮。点一下之后全自动：
下载 → 校验 SHA256 → 替换正在运行的二进制 → 优雅关服 → 用新版本重启 → 页面自动刷新。
不用下载文件、不用手动替换、不用自己重启。

- **强制校验 SHA256**：更新等于拿远程文件覆盖正在跑的程序，没校验就是把远程代码执行的钥匙
  交给任何能中间人的网络。校验值取自 Release 里的 `checksums.txt`，**没有这个文件就直接拒绝更新**
- Linux / macOS 走 `execve` 换进程映像，**PID 不变** —— systemd / docker 不会以为服务崩了
- Windows 先把自己改名 `.old` 再放新文件（系统不允许覆盖运行中的 exe），失败自动回滚
- 程序所在目录不可写时提前报错，而不是下完 16MB 才失败
- 支持 `OVH_UPDATE_API` 指向私有镜像

> 这个功能从 v0.1.0 起可用：更早的二进制里没有更新器。

### Java KVM 可选（[#1](https://github.com/gokele/ovh/issues/1)）

IPMI 对话框现在会先列出这台机器支持的接入方式，由你选，而不是后端替你挑：

| 方式 | 说明 |
|---|---|
| HTML5 KVM | 浏览器直接打开 |
| **Java KVM** | 下载 `.jnlp`，用 Java Web Start 打开 |
| 串口重定向 SOL（URL） | 浏览器打开 |
| 串口重定向 SOL（SSH） | 用账户里登记的 SSH 公钥 |

以前后端按固定优先级自动挑、HTML5 排第一，同时支持两种的机器永远拿不到 Java KVM。

> 新版 JDK 已经移除 Java Web Start，`.jnlp` 需要用 [OpenWebStart](https://openwebstart.com/) 打开。

## v0.0.9 更新

这一版的重点是**三个区(EU / US / CA)的正确性**。OVH 的三个站点是彼此独立的系统 ——
目录、价格、库存、购物车、账户、可用端点都不互通,之前很多地方按"只有欧区"写死了。

**抢购**
- 修复**美区下单必失败**:`region` 配置项的合法取值由 (子公司, planCode) 决定,不是按机房推。
  美区目录里 143 个 plan 的 region **只有 `united_states`**(哪怕机房是 gra/fra 这些欧洲机房),
  而老代码发的 `usa` / `apac` 在任何子公司的目录里都不存在,每一单都卡在配置这步。
  现在从官方目录解析(2 小时缓存),拉不到才退回静态兜底
- 修复 **addon 错配**:标准化会把机型后缀吃成粘连残渣,导致正确的 addon 反而落到更低优先级 ——
  选纯 2x960NVMe(€0)实际被配成混合盘(€24)。改成原始码优先的四档匹配
- 询价与下单的 addon 匹配口径统一(以前询价只做精确匹配,美区永远匹配不上,
  表现为"抢购能下单、询价说不可订购")
- `MaxRetries` 现在真的生效,且只统计**真正提交并失败**的次数(无货的空轮不计)

**监控**
- 修复**跨区订阅静默永不告警**:查错站点返回的是 HTTP 200 + 空数组,不报错也不告警。
  现在按机型归属自动选查询账户
- 检查间隔可配(5–3600 秒,越界自动夹紧),监控页点一下就能改,重启保持
- 可用性检查不再每轮现拉 10MB 目录(以前每订阅每 5 秒一次,几个订阅就能把自己打进 429)
- `comingSoon` 不再算有货(它是"即将上线尚未开售",下不了单)

**Telegram**
- webhook 补上完整校验链:`secret_token` → body 上限 → `update_id` 幂等 → 发送者授权 → 频率限制
- 「一键下单」按钮参数落库并做成**一次性 nonce**:以前只存内存,重启即失效且可无限重放下单

**其它**
- 后端询价端点 `POST /api/servers/{planCode}/price`(cart 真实询价,前端本地算不出价时兜底)
- 账户建号时校验子公司与 endpoint 同区(`zone=US` 配 `ovh-eu` 会一路走到下单才报错)
- 机房覆盖补上巴黎(`eu-west-par-a/b/c`)与多伦多(`ca-east-tor-a`),这些机房一直有货但前端看不见
- 修复两处 `ON CONFLICT` 把订阅的自动下单账户清空的缺陷

完整的端点与行为差异见下面的[多区域注意事项](#多区域eu-us-ca注意事项)。

## 技术栈

| 层 | 技术 |
|---|---|
| 前端 | Vite 5 + React 18 + TypeScript + TanStack Router + TanStack Query + shadcn-ui + Tailwind + recharts |
| 后端 | Go 1.21+ + Gin + 官方 [go-ovh](https://github.com/ovh/go-ovh) SDK |
| 持久化 | SQLite(`modernc.org/sqlite` 纯 Go / `mattn/go-sqlite3` cgo 双 driver, build tag 自动选) |
| 通知 | Telegram Bot Webhook |
| 部署 | 单二进制(前端 //go:embed 进 Go 二进制) 或前后端分开跑 |

## 项目结构

```
.
├── server/   # Go 后端 (Gin, 默认 :19998)
│   ├── main.go
│   ├── webembed_ui.go    # build tag=ui 时把 web/ 整目录 embed 进二进制
│   ├── webembed_noui.go  # 默认 build,无前端
│   └── internal/
│       ├── app/          # State 聚合
│       ├── db/           # SQLite 层 (schema.sql + 各表 CRUD)
│       ├── handlers/     # Gin handler
│       ├── monitor/      # 服务器补货监控
│       ├── vps/          # VPS 补货监控
│       ├── purchase/     # 下单流程
│       ├── price/        # OVH cart 询价
│       ├── ovh/          # 按 account_id 路由的多账户 client 工厂
│       └── ...
└── web/      # 前端 (Vite + TanStack, dev 默认 :19997)
    └── src/
        ├── routes/       # 文件路由
        ├── components/   # 共享组件 + AuthGate / OvhCredsGate
        ├── hooks/        # TanStack Query hooks
        └── lib/          # 子公司表 / OVH 数据中心常量 / utils
```

后端详细文档见 [server/README.md](server/README.md)。

## 部署方式

### 方式 A:单二进制(推荐生产)

前端 build → Vite 输出到 `server/web/` → Go `-tags ui` 触发 `//go:embed` 把整目录嵌入二进制 → 单文件部署、双击即用。

```bash
# 1) 前端构建到 server/web/
cd web
npm ci
npm run build

# 2) 编译带前端的单二进制(CGO_ENABLED=0 走纯 Go SQLite,交叉编译不需要 gcc)
cd ../server
CGO_ENABLED=0 go build -tags ui -trimpath \
  -ldflags "-s -w -X github.com/ovh-buy/server/internal/handlers.Version=$(cat ../VERSION)" \
  -o ovh-server .
./ovh-server
```

Windows 把产物名改成 `ovh-server.exe` 即可;交叉编译加 `GOOS=linux GOARCH=arm64` 这类前缀。
**不需要外部 SQLite 库,二进制自带。** 默认监听 `:19998`,浏览器打开 `http://localhost:19998` 即用,
数据库自动建在 `./data/sniper.db`。

> Release 页提供 Windows amd64 / Linux amd64 / Linux arm64 三个预编译产物,不想自己编译可以直接下。

### 方式 B:开发(前后端分开跑)

```bash
# 后端
cd server
go run .                # 默认 :19998

# 前端(另一个终端)
cd web
npm install
npm run dev             # 默认 :19997, /api/* 自动反代到 19998
```

浏览器打开 `http://localhost:19997`。


## 首次访问

打开浏览器后会依次出现两层全屏遮罩,都过了才能进主界面:

1. **AuthGate**:输入 `.env` 里设置的 `API_SECRET_KEY`(没设的话用默认值,见下)
2. **OvhCredsGate**:无任何 OVH 账户时强制弹出。填**账户名称** + OVH 子公司(Zone) + `APP KEY / APP SECRET / CONSUMER KEY`,`Endpoint` / `IAM` 自动派生。后端 `POST /api/accounts` 落 `ovh_accounts` 表并真去 OVH 验一次,通过才放行。

凭据通过后,前端立刻在后台 prefetch 三件套(服务器目录 / catalog / 可用性),用户切到服务器列表页**直接出数据,不会再走"加载中"**。

后续可在"设置 → OVH 账户"加更多账户。每个账户独立的 endpoint / 凭据 / Zone,**抢购队列、监控订阅、自动下单全部按账户隔离**。服务器控制 tab 顶部有账户切换器,可在已登录账户之间切换查看。

## 配置

`server/.env.example` → 复制成 `server/.env` 改:

```bash
API_SECRET_KEY=...               # 前端访问后端的密钥, 必须改
PORT=19998                       # 后端监听端口
LISTEN_HOST=                     # 空 = 所有网卡(IPv4+IPv6); 127.0.0.1 锁回环; 0.0.0.0 公网
ENABLE_API_KEY_AUTH=true         # 关掉的话所有 /api/* 不验证密钥, 仅本地调试用
GIN_MODE=release                 # debug | release
DEBUG=false                      # true 时启用 debug 日志

# --- Telegram Webhook 安全（都可不填，留空即用默认行为）---
TG_WEBHOOK_SECRET=               # 自定义 webhook secret_token; 留空则首次注册时自动生成并落库
TG_WEBHOOK_SECRET_OPTIONAL=false # true 时跳过 secret 校验, 仅本地调试用, 公网部署不要开
TG_ALLOWED_USER_IDS=             # 群聊场景下允许下单的 user id, 逗号分隔; 私聊不需要
```

OVH 凭据**不放 env**,通过前端 OvhCredsGate / 设置页"OVH 账户" tab 录入,落 SQLite `ovh_accounts` 表(每个账户一行,独立 endpoint / AppKey / Secret / ConsumerKey / Zone)。`.gitignore` 默认拒绝所有 `.env` 入库。

## 主要功能

### 能力总览

| 能力 | 状态 | 说明 |
|---|---|---|
| 多 OVH 账户 | ✅ | 独立 endpoint / 凭据 / Zone,抢购队列、历史、监控订阅全按 `account_id` 隔离 |
| **三区支持(EU / US / CA)** | ✅ | 子公司归属、目录站点、`region` 取值、planCode 后缀、机房集合全部按区解析,不写死欧区 |
| 抢购队列 | ✅ | 每机型 × 每机房 × 数量独立任务,可暂停/恢复,fail-fast 不退化到默认配置 |
| 服务器补货监控 | ✅ | 订阅 planCode + 机房,状态变化推 Telegram,**检查间隔 5–3600 秒可配** |
| VPS 补货监控 | ✅ | 区分 Linux / Windows 镜像,按子公司连对站点 |
| 自动下单 | ✅ | 监控触发,可指定下单账户;不指定则只通知 |
| Telegram 文本下单 | ✅ | 5 种消息格式,`plancode [机房] [数量] [配置]` |
| Telegram 一键下单按钮 | ✅ | 上架通知内嵌机房按钮,参数落库、**一次性 nonce**、防重放 |
| Telegram webhook 安全 | ✅ | secret_token → body 上限 → update_id 幂等 → 发送者授权 → 频率限制 |
| 后端询价 | ✅ | `POST /api/servers/{planCode}/price`,走 OVH cart 真实询价 |
| 已购服务器管理 | ✅ | 电源 / 重装(ZFS·软RAID·自定义分区) / IPMI / BIOS / 启动模式 / 任务 / 维护工单 |
| 已购 VPS 管理 | ✅ | 开关机 / 重装 / 快照 / 控制台 / 改密 / 反解 / 自动备份 |
| 网络与防护 | ✅ | 网卡 / OLA / MRTG 流量图 / DDoS 缓解 / 防火墙 / Backup FTP |
| 合同期(engagement) | ✅ | 服务器与 VPS 双端,销毁类操作强制二次确认 |
| 隐私模式 | ✅ | 一键打码所有 IP / MAC / 反向 DNS |
| 自动检测更新 | ✅ | 拉 GitHub Releases 比版本号,有新版显示 ✨ |
| **在线更新** | ✅ | 点一下自替换 + 自动重启,强制校验 SHA256(v0.1.0 起) |
| **控制台接入方式可选** | ✅ | HTML5 KVM / **Java KVM(.jnlp)** / SOL(URL) / SOL(SSH),由用户选 |
| 配置绑定狙击 | ❌ | 已下线 |


### 多账户
- **账户管理**:设置页"OVH 账户" tab 增删改查,每条记录有独立**名称 + Zone + endpoint + AppKey/Secret/ConsumerKey**
- **账户隔离**:抢购队列、抢购历史、监控订阅都标 `account_id`,后端 goroutine 按 account_id 取对应 OVH client 下单
- **级联清理**:删账户时关联 history / queue 自动删除,监控订阅的"自动下单账户"字段清空(订阅本身保留,只通知不下单)
- **默认账户**:其中一个标 `is_default`,新建对话框不选时自动用默认账户
- **凭据校验**:新建 / 更新账户都会真去 OVH 调一次 `/me`,结果放在响应的 `valid` 字段里(校验失败**仍会入库**,前端提示后放行,便于先进系统再到设置页修凭据)
- **子公司与 endpoint 强制同区**:`zone=US` 只能配 `ovh-us`(同区的 `kimsufi-*` / `soyoustart-*` 别名照常可用)。EU / US / CA 三个站点的目录、价格、库存、购物车完全独立,配错的话要一路走到下单才报错,所以在建账户时就拦掉

### 抢购
- **服务器列表**:卡片网格 + 实时 DC 库存灯(绿可用 / 红缺货),点击直接选配置下单
- **配置选择器**:按 OVH `addonFamilies`(CPU / 内存 / 系统盘 / 数据盘 / 带宽 / vRack)分组单选,默认值预选
- **抢购队列**:每台服务器 × 每个 DC × 数量 独立任务,**每个任务绑定到一个 OVH 账户**,可暂停 / 恢复 / 删除,按 retry interval 轮询 OVH 库存
- **fail-fast**:用户选的配置匹配不上 OVH 当前可订购的 addon → 整单失败,绝不退化到默认 HDD
- **有货判定按官方枚举白名单**:只有 `\d+H`(交付时长承诺)算有货,`comingSoon` / `unknown` 不算 —— 否则会为永远下不了单的机型反复建单
- **价格预览**:18 个 OVH subsidiary 切换比价(EUR / USD / CAD / GBP / SGD / AUD / INR / PLN ...),前端用本地 catalog 算,不走 cart 流程
- **后端询价兜底**:`POST /api/servers/{planCode}/price`(body `{datacenter, options}`,账户走 `?account=<id>`)。走 OVH cart 真实询价拿含税/不含税/币种,用于本地 catalog 算不出价(缺项 / OVH 改结构 / addon 在目标机房不可订购)时兜底,也给外部脚本一个不必复刻算价公式的入口

### 监控
- **服务器补货**:订阅 planCode + DC 组合,状态变化推 Telegram。**自动下单可选指定账户**;不选只通知不下单
- **检查间隔可配**:监控页「检查间隔」点一下就地改,合法区间 5-3600 秒(越界自动夹紧并回传实际生效值),落 `kv` 表重启保持。下限 5 秒是因为 OVH 可用性接口本身有缓存,更快只会撞限流
- **VPS 补货**:同上,针对 OVH VPS 产品线(区分 Linux / Windows 镜像)
- **历史时间线**:每个订阅完整变化记录

### 已购服务器管理
- **顶部账户切换器**:服务器控制 tab 头单独有账户下拉,切换后所有 `/server-control/*` 请求由 axios 拦截器自动追加 `?account=<id>`,无需逐 hook 改造
- **概览**:硬件信息 + 服务到期 + IP / 网卡 + MRTG 流量图
- **电源 / 系统**:重启 / 重装(含 ZFS / 软 RAID / 自定义分区)/ IPMI 控制台 / 启动模式 / SPLA Windows 解锁 / 任务列表 / BIOS / 安装进度。重装接口加了 per-service `TryLock`,防双击重复提交
- **维护**:维护记录 + 硬件更换工单(硬盘 / 内存 / 散热)+ 联系人变更(Token 邮件确认)
- **高级**(9 个 sub-tab):Burst / 防火墙 / Backup FTP / Secondary DNS / 虚拟 MAC / vRack / 可订购升级 / 附加选项 / IP 规格
- **隐私模式**:一键打码所有 IP / MAC / 反向 DNS 主机名

### 其它
- **账户管理**:余额 / 退款记录 / 邮件历史(按当前账户切换)
- **抢购历史**:订单 + 价格 + 倒计时 + OVH 订单链接直跳,每行带账户标识 chip
- **详细日志**:实时刷新,按级别 / 关键字筛选
- **自动检测更新**:仪表盘 mount 时调一次 `GET /api/version/check-update` 拉 GitHub releases 比版本号,有新版在版本号旁显示 ✨ chip 跳 release 页;后端纯被动响应,无 goroutine / 无定时

## 持久化

全部业务数据在 SQLite(`data/sniper.db`),11 张表:

| 表 | 用途 |
|---|---|
| `kv` | 单例配置(TG token / webhook secret / 服务器与 VPS 检查间隔等非账户级配置) |
| `ovh_accounts` | OVH 账户(独立 endpoint / AppKey / Secret / ConsumerKey / Zone / is_default) |
| `queue` | 抢购队列(`account_id` 关联) |
| `history` | 抢购历史(`account_id` 关联) |
| `servers` | OVH 服务器目录缓存(刷新一次写一次,2h TTL) |
| `catalogs` | OVH 公共 catalog 每个 subsidiary 一份(2h TTL),浏览页价格走它 |
| `monitor_subscriptions` | 服务器补货订阅(`auto_order_account_id` 关联) |
| `vps_subscriptions` | VPS 补货订阅(同上) |
| `server_aliases` | 服务器本地别名(account_id + service_name 复合主键,不下发 OVH) |
| `telegram_order_buttons` | TG「一键下单」按钮 UUID → 下单参数,`used_at` 做一次性 nonce |
| `telegram_updates` | TG webhook `update_id` 幂等表,防重放重复下单 |

日志仍走 JSON 文件(`data/logs/app.log.json`),不进 SQLite。

## 缓存策略

| 数据 | 后端 TTL | 前端 staleTime | 后台轮询 | 触发刷新 |
|---|---|---|---|---|
| 服务器目录 | 2h(SQLite + 内存 ServerCache) | 2h | ❌ 完全访问触发 | 缓存过期时下一次访问 / 手动刷新按钮 |
| OVH catalog(价格) | 2h(SQLite `catalogs` 表) | 2h | ❌ | 同上 |
| 实时可用性 | — | 1 分钟 | ❌(原每 60 秒轮询已关) | 同上 |

启动时不主动调 OVH,只把 SQLite 现有数据加载到内存。`ServerCache` 用 SQLite 真实 `updated_at` 重建时间戳,旧数据不会被当成"刚刷过的"。

## 安全 / 鉴权

- 后端所有 `/api/*`(除少数白名单如 `/health` / `/telegram/webhook` / `/version` / `/version/check-update`)都要求 `X-API-Key` 请求头
- 两层全屏 gate:AuthGate(API 密钥) + OvhCredsGate(至少一个 OVH 账户)
- API Key 存浏览器 localStorage,失效自动清除并要求重新输入
- OVH 凭据落 SQLite `ovh_accounts` 表,前端通过 OvhCredsGate / 设置页"OVH 账户" tab 录入
- `.gitignore` 默认拒绝所有 `.env` 文件入库(只允许 `*.env.example`),同时挡掉 `*.db` / `data/` / `logs/`
- ⚠️ **`data/sniper.db` 是明文的**:`ovh_accounts` 表直接存 AppKey / AppSecret / ConsumerKey,
  `kv` 表存 Telegram Token 与 webhook secret。备份、迁移、发日志给别人之前先想清楚 ——
  这个文件泄漏等于把 OVH 账户和机器控制权交出去

### Telegram Webhook 安全链

`/api/telegram/webhook` 在鉴权白名单里(Telegram 不可能带 `X-API-Key`),所以它自己有一条完整校验链,任何一环不过直接拒:

| 环节 | 作用 | 失败响应 |
|---|---|---|
| **secret_token** | 注册 webhook 时把随机 secret 交给 Telegram,之后每条回调都带 `X-Telegram-Bot-Api-Secret-Token` 头 —— 这是唯一能证明「请求真的来自 Telegram」的凭据 | `401 invalid_secret_token` |
| **body 上限** | 64 KB,防超大 body 打内存 | `413 body_too_large` |
| **update_id 幂等** | `telegram_updates` 表去重。Telegram 收不到 200 会重投同一条 update,没这层一次网络抖动就重复下单 | `200 {"duplicate":true}` |
| **发送者授权** | 只认 `tgChatId` 配置的那个 chat;群聊还要求 user id 在 `TG_ALLOWED_USER_IDS` 白名单里 | `403 unauthorized_actor` |
| **频率限制** | 单 chat 每 10 秒最多 8 次 | `429 rate_limited` |
| **一次性按钮** | 「一键下单」按钮的完整参数落 `telegram_order_buttons` 表,`used_at` 原子占用:**同一个按钮只能下单一次**,超 24h 作废;入队失败自动归还可重试 | `409 button_already_used` / `410 button_expired` |

按钮参数原来只存在进程内存里,重启后按钮全部失效、且可被无限次重放下单;落库同时解决了这两个问题。

**升级说明**:升级前注册的 webhook 不带 secret。为了不把现有按钮打挂,后端启动时会用同一个 URL 自动重注册一次把 secret 补上(日志可见);补上之前处于兼容模式(不校验 secret,其余各环照常生效)。也可以在设置页手动点一次「注册 Webhook」立即启用强校验。`GET /api/settings` 不会把 secret 回给前端,保存设置也不会覆盖它。

## 多区域(EU / US / CA)注意事项

三个站点是彼此独立的系统,同一个机型在不同子公司下的 planCode、价格、可下单区域都不一样:

| 项 | EU(`ovh-eu`) | US(`ovh-us`) | 说明 |
|---|---|---|---|
| planCode | `24sk202` | `24sk202-us` / `24sk202-eu` | 美区目录的机型都带后缀,拿欧区 planCode 查美区可用性会返回空 |
| `region` 配置项 | `canada` / `europe` | **`united_states`** | 下单时 `POST /order/cart/{id}/item/{id}/configuration` 要发的值 |
| 亚太机型(sgp/syd/ynm) | `canada` | — | OVH 把亚太机房归在 `canada` 这个 region 桶里,**没有** `apac` 这个取值 |
| 目录站点 | `eu.api.ovh.com` | `api.us.ovhcloud.com` | 由 `ovh.CatalogBaseURLForSubsidiary` 统一映射 |

`region` 的合法取值由 **(子公司, planCode)** 决定而不是机房:美区账户即使下单欧洲机房(`gra`/`fra`),region 也必须是 `united_states`。所以代码不做静态"机房→区域"映射,而是由 [catalog.ResolveRegion](server/internal/catalog/region.go) 从官方目录的 `configurations[].values` 里取,内存缓存 2 小时;拉不到目录时才退回 `ovh.RegionForDCInSubsidiary` 的静态兜底。[region_test.go](server/internal/catalog/region_test.go) 里有联网用例,会把两区目录里每个 (plan × 机房) 组合穷举验一遍。

## OVH API 对接

下单流程严格对齐 OVH 官方 [order-cart-examples](https://github.com/ovh/order-cart-examples):

```
POST /order/cart                         → cartId
POST /order/cart/{id}/assign
POST /order/cart/{id}/eco                → itemId
POST /order/cart/{id}/item/{itemId}/configuration × 3  (datacenter / os / region)
POST /order/cart/{id}/eco/options × N
GET  /order/cart/{id}/summary
POST /order/cart/{id}/checkout
```

价格计算 = 基础 plan 月费 + 各 addon family 选中 addon 月费累加(`ovhjk/parser/price.go` 1:1 移植到前端 `web/src/hooks/use-availability.ts`)。

## 端口

| 服务 | 端口 |
|---|---|
| Go 后端(生产单二进制 / 开发) | **19998** |
| Vite dev server(仅开发) | 19997 |
| OVH Telegram webhook 入口 | `/api/telegram/webhook`(不走 X-API-Key,改由 secret_token + 授权链校验,见上) |
