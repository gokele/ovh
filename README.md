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

## 技术栈

| 层 | 技术 |
|---|---|
| 前端 | Vite 5 + React 18 + TypeScript + TanStack Router + TanStack Query + shadcn-ui + Tailwind + recharts |
| 后端 | Go 1.21+ + Gin + 官方 [go-ovh](https://github.com/ovh/go-ovh) SDK |
| 持久化 | SQLite(`modernc.org/sqlite` 纯 Go / `mattn/go-sqlite3` cgo 双 driver, build tag 自动选),凭据字段 AES-256-GCM 加密落盘 |
| 通知 | Telegram Bot Webhook + 自定义 Webhook(钉钉 / 飞书 / Bark / 自建),多通道冗余 |
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
│       ├── notify/       # 多通道通知(Telegram / 自定义 Webhook)
│       ├── secret/       # 凭据落盘加密(AES-256-GCM)
│       ├── updater/      # 在线更新:下载 / 校验 / 自替换 / 回滚
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

# --- 数据库加密（都可不填，首次启动会自动处理）---
OVH_DB_KEY=                      # 加密数据库里 OVH 凭据和 TG token 的密钥
                                 # 没填的话首次启动自动生成一把并写回这个文件
                                 # 备份 .env 时别漏了它: 丢了就再也解不开已存的账户
OVH_ENV_FILE=                    # 配置文件自身的位置, 默认工作目录下的 .env
                                 # systemd / docker 里工作目录未必是程序所在目录,
                                 # 那种情况写绝对路径, 否则密钥可能"这次写进去下次找不到"

# --- Telegram Webhook 安全（都可不填，留空即用默认行为）---
TG_WEBHOOK_SECRET=               # 自定义 webhook secret_token; 留空则首次注册时自动生成并落库
TG_WEBHOOK_SECRET_OPTIONAL=false # true 时跳过 secret 校验, 仅本地调试用, 公网部署不要开
TG_ALLOWED_USER_IDS=             # 群聊场景下允许下单的 user id, 逗号分隔; 私聊不需要
```

OVH 凭据**不放 env**,通过前端 OvhCredsGate / 设置页"OVH 账户" tab 录入,落 SQLite `ovh_accounts` 表(每个账户一行,独立 endpoint / AppKey / Secret / ConsumerKey / Zone),**加密存储**。`.gitignore` 默认拒绝所有 `.env` 入库。

通知地址在设置页的「通知通道」里配,不走 env。Telegram 和自定义 Webhook 至少配一个 —— 只要还有一条能用,监控就继续跑。

## 主要功能

### 能力总览

| 能力 | 状态 | 说明 |
|---|---|---|
| 多 OVH 账户 | ✅ | 独立 endpoint / 凭据 / Zone,抢购队列、历史、监控订阅全按 `account_id` 隔离 |
| **全站单一账户入口** | ✅ | 只在左侧菜单栏切换,机型列表 / 可用性 / 价格 / 控制台 / 下单账户全部跟着走 |
| **三区支持(EU / US / CA)** | ✅ | 子公司归属、目录站点、`region` 取值、planCode 后缀、机房集合全部按区解析,不写死欧区 |
| 抢购队列 | ✅ | 每机型 × 每机房 × 数量独立任务,可暂停/恢复,fail-fast 不退化到默认配置 |
| 服务器补货监控 | ✅ | 订阅 planCode + 机房,状态变化推 Telegram,**检查间隔 5–3600 秒可配** |
| VPS 补货监控 | ✅ | 型号来自 OVH 实时目录(型号会整代下架,写死会让监控静默失效),区分 Linux / Windows,按子公司连对站点 |
| 服务器自动下单 | ✅ | 监控触发,可指定下单账户;不指定则只通知 |
| **VPS 自动下单** | ✅ | 同上,走 `/order/cart/{id}/vps`;系统在下单时选定,`region` 按站点解析 |
| **订阅可编辑** | ✅ | 改配置不重置库存状态和历史 —— 删了重建会让"本来就有货"被误判成补货 |
| Telegram 文本下单 | ✅ | 5 种消息格式,`plancode [机房] [数量] [配置]` |
| Telegram 一键下单按钮 | ✅ | 上架通知内嵌机房按钮,参数落库、**一次性 nonce**、防重放 |
| Telegram webhook 安全 | ✅ | secret_token → body 上限 → update_id 幂等 → 发送者授权 → 频率限制 |
| **多通道通知** | ✅ | Telegram + 自定义 Webhook,只要有一条能用监控就继续跑,全挂才停 |
| **凭据落盘加密** | ✅ | AES-256-GCM,密钥首次启动自动生成写进 `.env`,老库自动迁移 |
| **抢购耗时打点** | ✅ | 查库存 / 建车 / 绑车 / 加购 / 配置 / 选项 / 下单 逐段计时,回答"我慢在哪一步" |
| 后端询价 | ✅ | `POST /api/servers/{planCode}/price`,走 OVH cart 真实询价 |
| 已购服务器管理 | ✅ | 电源 / 重装(ZFS·软RAID·自定义分区) / IPMI / BIOS / 启动模式 / 任务 / 维护工单 |
| 已购 VPS 管理 | ✅ | 开关机 / 重装 / 快照 / 控制台 / 改密 / 反解 / 自动备份 |
| 网络与防护 | ✅ | 网卡 / OLA / MRTG 流量图 / DDoS 缓解 / 防火墙 / Backup FTP |
| 合同期(engagement) | ✅ | 服务器与 VPS 双端,销毁类操作强制二次确认 |
| 隐私模式 | ✅ | 一键打码所有 IP / MAC / 反向 DNS |
| 自动检测更新 | ✅ | 拉 GitHub Releases 比版本号,有新版显示 ✨ |
| **在线更新** | ✅ | 点一下自替换 + 自动重启,强制校验 SHA256,新版起不来自动回滚 |
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
- **价格显示**:按**当前账户所属子公司**计价(币种、税率、目录都跟着它走),前端用本地 catalog 算,不走 cart 流程。不提供跨子公司比价 —— 那个下拉曾让人误以为切换了机型目录,照着它下单会被 OVH 拒
- **耗时打点**:每一轮按 查库存 / 建购物车 / 绑定 / 加购 / 必需配置 / 硬件选项 / 下单 分段计时。抢购输了之后唯一有用的信息就是"慢在哪一步" —— 没有这串数字的话,"OVH 就是没货""我这机器网络慢""某一步卡了 8 秒"三种情况长得一模一样,而它们要采取的行动完全不同。抢购历史每行可点开看分解,队列页显示每条链路上一轮的结果和总耗时
- **后端询价兜底**:`POST /api/servers/{planCode}/price`(body `{datacenter, options}`,账户走 `?account=<id>`)。走 OVH cart 真实询价拿含税/不含税/币种,用于本地 catalog 算不出价(缺项 / OVH 改结构 / addon 在目标机房不可订购)时兜底,也给外部脚本一个不必复刻算价公式的入口

### 监控
- **服务器补货**:订阅 planCode + DC 组合,状态变化推 Telegram。**自动下单可选指定账户**;不选只通知不下单
- **检查间隔可配**:监控页「检查间隔」点一下就地改,合法区间 5-3600 秒(越界自动夹紧并回传实际生效值),落 `kv` 表重启保持。下限 5 秒是因为 OVH 可用性接口本身有缓存,更快只会撞限流
- **VPS 补货**:同上,针对 OVH VPS 产品线(区分 Linux / Windows 镜像)。型号列表来自 **OVH 实时目录**而不是写死 —— VPS 型号会整代下架(2025 代已全线退出下单目录),盯着一个停售型号的订阅永远不会响,而症状只是"一直没货",看不出问题在哪。已有订阅指向停售型号的会标「已停售」
- **VPS 自动下单**:补货时真的下单(`cart → assign → POST /vps → 必需配置 → checkout`)。系统(`vps_os`)是 VPS 下单时就要定的配置项,不是买完再装,所以放在订阅里选。只对"无货→有货"的跳变下单,抢到一台就停
- **订阅可编辑**:服务器和 VPS 订阅都能改配置,**不重置库存状态和历史**。删了重建会清空 `LastStatus`,下一轮把"本来就有货"当成补货跳变,发一条根本没发生的通知外加真下单
- **多通道通知**:Telegram + 自定义 Webhook。只要有一条通道可用监控就继续跑,全部挂掉才停 —— 以前 Telegram 一挂丢的不是一条消息,是整个监控
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
| `kv` | 单例配置(TG token / webhook secret / 通知 webhook 地址 / 服务器与 VPS 检查间隔等非账户级配置),**其中的密钥字段加密存储** |
| `ovh_accounts` | OVH 账户(独立 endpoint / AppKey / Secret / ConsumerKey / Zone / is_default),**三个凭据字段加密存储** |
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

加密的字段带 `enc:v1:` 前缀,没有前缀的一律按明文处理 —— 老库升级上来时表里全是明文,不能一律当密文去解。首次启动会就地把已有的明文迁移成密文,幂等,重复启动不会重复加密。

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
- **凭据落盘加密**:`ovh_accounts` 的 AppKey / AppSecret / ConsumerKey、`kv` 里的 Telegram Token 与 webhook secret 都是 AES-256-GCM 加密存的。密钥优先取环境变量 `OVH_DB_KEY`,没有就在首次启动时生成一把写进 `.env`(权限 0600)
- ⚠️ **加密防的是"只拿到 db 文件"那一类泄漏** —— 备份被同步到网盘、拷整个目录换机器、把 `data/` 打包发给别人排查问题。它**防不住** `.env` 和 db 一起漏出去,那种情况下加密等于没有。而 `.env` 恰恰是最容易被顺手提交、被贴进 issue 的文件
- **密钥丢了会拒绝启动**:库里有密文却找不到密钥时,程序会停下来并说明怎么办,而不是照常起来。否则表现是"账户都在但每次调 OVH 都报签名错误",没人猜得到是密钥问题,而这时候重新录入凭据会覆盖旧密文,最后一点恢复余地也没了。确实找不回来时用 `OVH_DB_KEY_RESET=1` 启动,那些账户需要重新录入

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

> **账户只在左侧菜单栏切换一次,全站跟着走。**
> 机型列表、机房红绿点、价格币种、服务器/VPS 控制台、下单账户,全部按当前账户所在站点显示。
> 之所以只留一个入口:以前列表页、下单对话框、控制台页各有一个账户选择器且互不同步,
> 而三个站点的目录互不相通(同一台机器欧区叫 `24sk602`、美区叫 `24sk602-v1-us`),
> "用 A 账户浏览、用 B 账户下单"一键就能做出来 —— 这种任务必然被拒。
> 同理机房也只列该机型在当前站点真正可选的那些,不再固定显示 16 个。


三个站点是彼此独立的系统,同一个机型在不同子公司下的 planCode、价格、可下单区域都不一样:

| 项 | EU(`ovh-eu`) | US(`ovh-us`) | 说明 |
|---|---|---|---|
| planCode | `24sk202` | `24sk202-us` / `24sk202-eu` | 美区目录的机型都带后缀,拿欧区 planCode 查美区可用性会返回空 |
| `region` 配置项 | `canada` / `europe` | **`united_states`** | 下单时 `POST /order/cart/{id}/item/{id}/configuration` 要发的值 |
| 亚太机型(sgp/syd/ynm) | `canada` | — | OVH 把亚太机房归在 `canada` 这个 region 桶里,**没有** `apac` 这个取值 |
| 目录站点 | `eu.api.ovh.com` | `api.us.ovhcloud.com` | 由 `ovh.CatalogBaseURLForSubsidiary` 统一映射 |

**VPS 也是三套独立系统,差异和独服不同**(实测公开目录):

| 项 | EU / CA 站点 | US 站点 |
|---|---|---|
| `region` 取值 | `canada` / `europe` | **只有 `united_states`** |
| 机房集合 | 11 个(含 BHS / SGP / SYD / YNM) | `vps-xxx` 只有 `US-EAST-VA` / `US-WEST-OR` |
| 买欧洲 / 加拿大机房 | 同一个商品 | 要买 **`-eu` / `-ca` 后缀的另一个商品** |

所以 VPS 下单时 `region` 不硬猜:先问购物车的 `requiredConfiguration`,它给一个取值就用那个,给多个才按机房挑(BHS/SGP/SYD/YNM→`canada`,其余→`europe`;这张表是从 OVH 自己的 `-ca` / `-eu` 变体目录里读出来的)。认不出的机房宁可不提交 `region`,让 OVH 用默认值 —— 提交一个错的会把整单打掉。

独服这边,`region` 的合法取值由 **(子公司, planCode)** 决定而不是机房:美区账户即使下单欧洲机房(`gra`/`fra`),region 也必须是 `united_states`。所以代码不做静态"机房→区域"映射,而是由 [catalog.ResolveRegion](server/internal/catalog/region.go) 从官方目录的 `configurations[].values` 里取,内存缓存 2 小时;拉不到目录时才退回 `ovh.RegionForDCInSubsidiary` 的静态兜底。[region_test.go](server/internal/catalog/region_test.go) 里有联网用例,会把两区目录里每个 (plan × 机房) 组合穷举验一遍。

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

VPS 是另一条链路(注意不是 `/eco`,必需配置项也不同):

```
POST /order/cart                         → cartId
POST /order/cart/{id}/assign
POST /order/cart/{id}/vps                → itemId   (duration / pricingMode 取自 GET /order/cart/{id}/vps)
GET  /order/cart/{id}/item/{itemId}/requiredConfiguration
POST /order/cart/{id}/item/{itemId}/configuration   (vps_datacenter 必填 / region / vps_os)
POST /order/cart/{id}/checkout
```

在售型号取自公开目录 `GET /order/catalog/public/vps?ovhSubsidiary=XX`,用 OVH 自己的 `order-funnel:show` 标记筛选 —— 不拿 planCode 正则猜代次,猜的话每次换代都得发版,而且分不出"下架了"和"正则没覆盖到"。

价格计算 = 基础 plan 月费 + 各 addon family 选中 addon 月费累加(`ovhjk/parser/price.go` 1:1 移植到前端 `web/src/hooks/use-availability.ts`)。

## 端口

| 服务 | 端口 |
|---|---|
| Go 后端(生产单二进制 / 开发) | **19998** |
| Vite dev server(仅开发) | 19997 |
| OVH Telegram webhook 入口 | `/api/telegram/webhook`(不走 X-API-Key,改由 secret_token + 授权链校验,见上) |
