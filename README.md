# OVH 控制台

OVH 独立服务器 / VPS / Eco 系列**抢购 + 监控 + 管理**控制台。

实时检测 OVH 各数据中心库存，发现可购买的服务器时按用户配置（机房、内存、存储、带宽、vRack）自动下单。后台同时管理已购买服务器的全生命周期（重启 / 重装 / IPMI / BIOS / 启动模式 / 维护工单 / 联系人变更 / 带宽 / 防火墙 / FTP 备份 / vRack / Secondary DNS 等）。

## 技术栈

| 层 | 技术 |
|---|---|
| 前端 | Vite 5 + React 18 + TypeScript + TanStack Router + TanStack Query + shadcn-ui + Tailwind + recharts |
| 后端 | Go 1.21+ + Gin + 官方 [go-ovh](https://github.com/ovh/go-ovh) SDK |
| 通知 | Telegram Bot Webhook |
| 部署 | 前后端独立编译，可同源 / 反代部署 |

## 项目结构

```
.
├── server/   # Go 后端 (Gin, 默认 :19998)
└── web/      # 前端 (Vite + TanStack, dev 默认 :19997)
```

后端详细文档见 [server/README.md](server/README.md)。

## 快速开始

### 1. 启动后端

```bash
cd server
cp .env.example .env
# 编辑 .env：填 OVH AppKey/AppSecret/ConsumerKey + API_KEY（前端用的访问密钥）
go mod tidy
go run .
```

后端起在 `:19998`，会自动创建 `server/data/` 持久化目录。

### 2. 启动前端

```bash
cd web
npm install
npm run dev
```

前端起在 `:19997`，`/api/*` 已通过 Vite proxy 转发到后端。

### 3. 首次访问

1. 浏览器打开 http://localhost:19997
2. 弹出"需要 API 密钥"对话框 → 填 `server/.env` 里设置的 `API_KEY`
3. 验证通过后进入仪表盘
4. 去 **API 设置** 页填入 OVH `AppKey / AppSecret / ConsumerKey`（如果 .env 没填）

## 主要功能

### 抢购
- **服务器列表**：卡片网格 + 实时 DC 库存灯（绿可用 / 红缺货），点击直接选配置下单
- **配置选择器**：按 OVH `addonFamilies`（CPU / 内存 / 系统盘 / 数据盘 / 带宽 / vRack）分组单选，默认值预选
- **抢购队列**：每台服务器 × 每个 DC × 数量 独立任务，可暂停/恢复/删除，按 retry interval 轮询 OVH 库存
- **fail-fast**：用户选的配置匹配不上 OVH 当前可订购的 addon → 整单失败，绝不退化到默认 HDD
- **价格预览**：18 个 OVH subsidiary 切换比价（EUR / USD / CAD / GBP / SGD / AUD / INR / PLN ...）

### 监控
- **服务器补货**：订阅 planCode + DC 组合，状态变化推 Telegram，可选自动下单（auto-order）
- **VPS 补货**：同上，针对 OVH VPS 产品线（区分 Linux / Windows 镜像）
- **历史时间线**：每个订阅完整变化记录（available ↔ unavailable + 配置切换）

### 已购服务器管理
- **概览**：硬件信息 + 服务到期 + IP / 网卡 + MRTG 流量图（每张网卡独立曲线）
- **电源 / 系统**：重启 / 重装（含 ZFS / 软RAID / 自定义分区）/ IPMI 控制台 / 启动模式切换 / SPLA Windows 解锁 / 任务列表 / BIOS / 安装进度
- **维护**：维护记录 + 硬件更换工单（硬盘 / 内存 / 散热）+ 联系人变更（admin / tech / billing + Token 邮件确认）
- **高级**（9 个 sub-tab）：Burst / 防火墙 / Backup FTP / Secondary DNS / 虚拟 MAC / vRack / 可订购升级 / 附加选项 / IP 规格
- **隐私模式**：一键打码所有 IP / MAC / 反向 DNS 主机名

### 其它
- **账户管理**：余额 / 退款记录 / 邮件历史
- **抢购历史**：订单 + 价格 + 倒计时 + OVH 订单链接直跳
- **详细日志**：实时刷新，按级别 / 关键字筛选，自动滚动到底部
- **API 设置**：OVH 凭据 + Telegram Bot 配置 + 缓存清理 + Webhook 信息

## 安全 / 鉴权

- 后端所有 `/api/*`（除少数白名单如 `/health` / `/telegram/webhook`）都要求 `X-API-Key` 请求头
- 前端启动时 `AuthGate` 探测 `/api/stats`，401 全屏盖一层登录窗，所有路由 / 点击都被拦截
- API Key 存浏览器 localStorage，密钥失效自动清除并要求重新输入
- OVH 凭据 + Telegram Token 通过 `.env` 或前端"设置"页录入，落到 `server/data/config.json`
- `.gitignore` 默认拒绝所有 `.env` 文件入库（只允许 `*.env.example`）

## OVH API 对接

下单流程严格对齐 OVH 官方 [order-cart-examples](https://github.com/ovh/order-cart-examples)：

```
POST /order/cart                         → cartId
POST /order/cart/{id}/assign
POST /order/cart/{id}/eco                → itemId
POST /order/cart/{id}/item/{itemId}/configuration × 3  (datacenter / os / region)
POST /order/cart/{id}/eco/options × N
GET  /order/cart/{id}/summary
POST /order/cart/{id}/checkout
```

价格计算 = 基础 plan 月费 + 各 addon family 选中 addon 月费累加。

## 端口

| 服务 | 端口 |
|---|---|
| Go 后端 | 19998 |
| Vite dev server | 19997（生产由 Go 同源 serve） |
| OVH Telegram webhook 入口 | `/api/telegram/webhook`（无需鉴权） |