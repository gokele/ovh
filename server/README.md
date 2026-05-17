# OVH 控制台 · 后端

Go (Gin) 实现的后端服务，配套 `../web/` 前端使用。

## 功能模块

- 抢购队列处理器与购买流程（OVH `/order/cart` → `assign` → `eco` → `configuration` → `options` → `checkout`）
- 服务器目录、实时可用性、价格查询
- 监控：服务器补货 / VPS 补货 / 配置绑定狙击
- 已购服务器管理（数十个 OVH 端点：BIOS / 启动模式 / 重装 / 任务 / 网络 / 维护 / 高级选项 等）
- 账户管理 + 联系人变更
- Telegram webhook 通知

## 目录结构

```
server/
├── main.go              # 入口 + Gin 路由注册
├── go.mod / go.sum
├── .env.example         # 环境变量样板
└── internal/
    ├── app/             # 共享运行状态（队列、订阅、历史等）
    ├── auth/            # API Key 中间件（含白名单、时间戳防重放）
    ├── catalog/         # OVH 目录抓取与索引
    ├── config/          # OVH 凭据 + 全局配置管理
    ├── handlers/        # HTTP handler（按资源分文件）
    ├── logger/          # 批量写盘的日志器
    ├── monitor/         # 服务器 / VPS 补货监控循环
    ├── numconv/         # OVH 数值类型转换工具
    ├── ovh/             # OVH SDK 客户端工厂 + DC 代码映射
    ├── price/           # 价格计算（base plan + addon family 月费累加）
    ├── purchase/        # 完整下单流程
    ├── sniper/          # 配置绑定狙击扫描
    ├── storage/         # JSON 文件读写
    ├── telegram/        # Telegram bot 通知
    ├── types/           # 核心数据结构
    └── vps/             # VPS 可用性查询
```

## 运行

```bash
cd server
cp .env.example .env
# 编辑 .env：OVH AppKey/AppSecret/ConsumerKey、API_KEY（前端访问凭据）等

go mod tidy
go run .
```

默认监听 `:19998`。

## 环境变量

`.env` 主要字段：

| 变量 | 说明 |
|---|---|
| `API_KEY` | 前端访问后端的 X-API-Key。前端 localStorage 里也存这个值 |
| `API_KEY_ENABLED` | 是否启用 API Key 校验（默认 true） |
| `OVH_APPLICATION_KEY` / `OVH_APPLICATION_SECRET` / `OVH_CONSUMER_KEY` | OVH API 凭据，可在前端"API 设置"页面填，也可写入 .env |
| `OVH_ENDPOINT` | `ovh-eu` / `ovh-us` / `ovh-ca`，决定 OVH API host |
| `DATA_DIR` | 持久化目录（队列、订阅、历史、缓存），默认 `./data` |
| `PORT` | HTTP 端口，默认 19998 |
| `TG_TOKEN` / `TG_CHAT_ID` | Telegram 通知（可选，前端也能配） |

## 鉴权

所有 `/api/*` 路径（除 `/api/health` 等少数白名单）都要求 `X-API-Key` 请求头。前端 [AuthGate](../web/src/components/common/AuthGate.tsx) 在挂载时探测一次 `/api/stats`，401 直接弹登录窗。

## 主要路由

```
# 通用
GET    /api/health
GET    /api/settings                      POST /api/settings
GET    /api/stats
GET    /api/logs                          DELETE /api/logs                    POST /api/logs/flush

# 服务器目录 / 可用性
GET    /api/servers
GET    /api/availability/:planCode
POST   /api/servers/:planCode/price

# 抢购队列
GET    /api/queue                         POST /api/queue
PUT    /api/queue/:id/status              DELETE /api/queue/:id               DELETE /api/queue/clear
GET    /api/purchase-history              DELETE /api/purchase-history

# 监控
GET    /api/monitor/subscriptions         POST /api/monitor/subscriptions
DELETE /api/monitor/subscriptions/:planCode
DELETE /api/monitor/subscriptions/clear
GET    /api/monitor/subscriptions/:planCode/history
GET    /api/monitor/status                POST /api/monitor/start             POST /api/monitor/stop

# VPS 监控（结构同上，前缀 /api/vps-monitor）

# 已购服务器（30+ 端点）
GET    /api/server-control/list
GET    /api/server-control/:serviceName/{hardware|serviceinfo|ips|...}
POST   /api/server-control/:serviceName/{reboot|install|spla|console|...}
PUT    /api/server-control/:serviceName/{boot-mode|burst|firewall|monitoring}

# 账户
GET    /api/ovh/account/{info|refunds|email-history|bills|...}
GET    /api/ovh/contact-change-requests

# Telegram
GET    /api/telegram/get-webhook-info
POST   /api/telegram/webhook              (OVH bot 回调，白名单)
```

## OVH 下单流程

按 OVH 官方推荐顺序（对齐 [order-cart-examples](https://github.com/ovh/order-cart-examples)）：

1. `POST /order/cart` body `{ovhSubsidiary: cfg.Zone}` → cartId
2. `POST /order/cart/{id}/assign` ← 紧跟创建之后
3. `POST /order/cart/{id}/eco` body `{planCode, pricingMode, duration: "P1M", quantity}` → itemId
4. `POST /order/cart/{id}/item/{itemId}/configuration` × 3：`dedicated_datacenter` / `dedicated_os` / `region`
5. `POST /order/cart/{id}/eco/options` × N：用户选的 addon planCode
6. `GET /order/cart/{id}/summary`
7. `POST /order/cart/{id}/checkout` body `{autoPayWithPreferredPaymentMethod, waiveRetractationPeriod}`

下单 subsidiary 由全局 `cfg.Zone`（settings.zone）决定。前端无需在每次任务里指定。

## 价格计算

`POST /eco/options` 用的 addon 价格、catalog 索引等逻辑见 `internal/price/`。客户端如需自行算价（前端已实现），公式：

```
总价 = plan 基础月费 + Σ(addon family 命中 addon 的月费)
```

其中：
- 月费条目过滤：`intervalUnit=month && interval=1 && mode=default`
- 安装费：`mode=default && capacities 含 "installation"`
- 价格单位：微欧元（÷ 1e8 得本币）

## 故障 fail-fast 策略

- `GET /eco/options` 失败 → 整单失败（避免用默认 addon 退化到错配）
- 用户指定的 addon planCode 在 OVH 列表里找不到 → 整单失败（防"选了 NVMe 下到 HDD"）
- `POST /eco/options` 单条失败 → 整单失败
