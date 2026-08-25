-- SQLite schema for OVH 控制台 server
-- 设计原则：
--   1. 列式字段用普通列（便于索引 / WHERE / ORDER BY）
--   2. 复杂嵌套（数组 / map / 对象）用 TEXT 列存 JSON
--   3. bool 用 INTEGER 0/1
--   4. 时间字段保持原 JSON 的字符串格式（ISO8601）以便兼容前端
--   5. 所有 CREATE 都用 IF NOT EXISTS，启动时无脑跑一遍

-- ===========================================
-- kv: 单例数据（Config / monitor & vps 全局状态）
-- ===========================================
CREATE TABLE IF NOT EXISTS kv (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

-- ===========================================
-- ovh_accounts: OVH 多账户凭据
-- 每条记录代表一个 OVH 账户;is_default=1 的那条用于未指定账户时 fallback
-- queue / history / config_sniper_tasks 的 account_id 引用这里
-- ===========================================
CREATE TABLE IF NOT EXISTS ovh_accounts (
  id           TEXT PRIMARY KEY,
  name         TEXT NOT NULL,
  endpoint     TEXT NOT NULL,
  zone         TEXT NOT NULL,
  app_key      TEXT NOT NULL,
  app_secret   TEXT NOT NULL,
  consumer_key TEXT NOT NULL,
  iam          TEXT NOT NULL,
  is_default   INTEGER NOT NULL DEFAULT 0,
  created_at   TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ovh_accounts_default ON ovh_accounts(is_default);

-- ===========================================
-- queue: 抢购队列任务
-- ===========================================
CREATE TABLE IF NOT EXISTS queue (
  id                     TEXT PRIMARY KEY,
  plan_code              TEXT NOT NULL,
  datacenter             TEXT NOT NULL,
  options                TEXT NOT NULL DEFAULT '[]', -- JSON 数组
  status                 TEXT NOT NULL,
  created_at             TEXT NOT NULL,
  updated_at             TEXT NOT NULL,
  retry_interval         INTEGER NOT NULL DEFAULT 60,
  retry_count            INTEGER NOT NULL DEFAULT 0,
  failure_count          INTEGER NOT NULL DEFAULT 0, -- 真正提交并失败的次数(无货轮次不计)
  max_retries            INTEGER NOT NULL DEFAULT 0,
  last_check_time        REAL    NOT NULL DEFAULT 0,
  quick_order            INTEGER NOT NULL DEFAULT 0,
  priority               INTEGER NOT NULL DEFAULT 0,
  from_telegram          INTEGER NOT NULL DEFAULT 0,
  config_sniper_task_id  TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_queue_status     ON queue(status);
CREATE INDEX IF NOT EXISTS idx_queue_plan_code  ON queue(plan_code);

-- ===========================================
-- history: 抢购历史记录
-- ===========================================
CREATE TABLE IF NOT EXISTS history (
  id              TEXT PRIMARY KEY,
  task_id         TEXT NOT NULL DEFAULT '',
  plan_code       TEXT NOT NULL,
  datacenter      TEXT NOT NULL,
  options         TEXT NOT NULL DEFAULT '[]', -- JSON
  status          TEXT NOT NULL,
  order_id        TEXT NOT NULL DEFAULT '',
  order_url       TEXT NOT NULL DEFAULT '',
  error_message   TEXT,                       -- nullable
  purchase_time   TEXT NOT NULL,
  attempt_count   INTEGER NOT NULL DEFAULT 0,
  expiration_time TEXT NOT NULL DEFAULT '',
  retraction_time TEXT NOT NULL DEFAULT '',   -- 撤销权截止日(billing.Order.retractionDate),与 expiration_time 语义不同
  price           TEXT                        -- JSON nullable (PriceInfo)
);
CREATE INDEX IF NOT EXISTS idx_history_status        ON history(status);
CREATE INDEX IF NOT EXISTS idx_history_purchase_time ON history(purchase_time DESC);
CREATE INDEX IF NOT EXISTS idx_history_task_id       ON history(task_id);
CREATE INDEX IF NOT EXISTS idx_history_plan_code     ON history(plan_code);

-- ===========================================
-- servers: OVH 服务器目录缓存（refresh-from-OVH 整块覆盖式 upsert）
-- 数据本身就是 OVH 接口返回的 catalog，结构复杂且字段稳定，整块 JSON 存即可
-- ===========================================
CREATE TABLE IF NOT EXISTS servers (
  plan_code  TEXT PRIMARY KEY,
  data       TEXT NOT NULL,   -- 完整 ServerPlan JSON
  updated_at INTEGER NOT NULL -- Unix epoch ms
);

-- ===========================================
-- monitor_subscriptions: 服务器补货监控订阅
-- ===========================================
CREATE TABLE IF NOT EXISTS monitor_subscriptions (
  plan_code           TEXT PRIMARY KEY,
  datacenters         TEXT NOT NULL DEFAULT '[]',  -- JSON []string
  notify_available    INTEGER NOT NULL DEFAULT 1,
  notify_unavailable  INTEGER NOT NULL DEFAULT 0,
  last_status         TEXT NOT NULL DEFAULT '{}',  -- JSON map[string]string
  created_at          TEXT NOT NULL,
  history             TEXT NOT NULL DEFAULT '[]',  -- JSON []HistoryEntry
  server_name         TEXT NOT NULL DEFAULT '',
  auto_order          INTEGER NOT NULL DEFAULT 0,
  quantity            INTEGER NOT NULL DEFAULT 1
);

-- ===========================================
-- vps_subscriptions: VPS 补货监控订阅
-- ===========================================
CREATE TABLE IF NOT EXISTS vps_subscriptions (
  id                  TEXT PRIMARY KEY,
  plan_code           TEXT NOT NULL,
  ovh_subsidiary      TEXT NOT NULL DEFAULT '',
  datacenters         TEXT NOT NULL DEFAULT '[]',  -- JSON []string
  monitor_linux       INTEGER NOT NULL DEFAULT 0,
  monitor_windows     INTEGER NOT NULL DEFAULT 0,
  notify_available    INTEGER NOT NULL DEFAULT 1,
  notify_unavailable  INTEGER NOT NULL DEFAULT 0,
  last_status         TEXT NOT NULL DEFAULT '{}',  -- JSON map
  history             TEXT NOT NULL DEFAULT '[]',  -- JSON []
  created_at          TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_vps_plan_code ON vps_subscriptions(plan_code);

-- ===========================================
-- catalogs: OVH 公开 catalog 每个 subsidiary 一份
-- 用途：浏览页"价格"显示。catalog 单份 2-5MB，直连 OVH 要 1-3s，缓存到本地后毫秒级返回
-- ===========================================
CREATE TABLE IF NOT EXISTS catalogs (
  subsidiary TEXT PRIMARY KEY,
  data       TEXT NOT NULL,   -- 完整 catalog JSON
  updated_at INTEGER NOT NULL -- Unix epoch ms
);

-- (旧:config_sniper_tasks 表已删除,功能下线。老数据库残留的该表 / config_sniper_task_id 列保留不动,
--  无害,无人读写。)

-- ===========================================
-- server_aliases: 服务器本地别名(纯本地,不下发 OVH)
-- 用途:服务器控制 tab 选择器 / 详情页把技术 service_name 显示成用户取的友好名
-- account_id + service_name 复合主键,避免不同账户同 service_name 互相串
-- ===========================================
CREATE TABLE IF NOT EXISTS server_aliases (
  account_id   TEXT NOT NULL,
  service_name TEXT NOT NULL,
  alias        TEXT NOT NULL,
  updated_at   TEXT NOT NULL,
  PRIMARY KEY (account_id, service_name)
);
CREATE INDEX IF NOT EXISTS idx_server_aliases_account ON server_aliases(account_id);

-- ===========================================
-- telegram_order_buttons: TG「一键下单」按钮 UUID → 下单参数
-- callback_data 受 Telegram 64 字节限制,只能塞 UUID,完整参数存这里。
-- 必须持久化:原来只在内存 messageUUIDCache 里,进程重启后按钮一点就 400。
-- used_at > 0 表示已消费 —— 一次性 nonce,防止同一条按钮被重放下单。
-- ===========================================
CREATE TABLE IF NOT EXISTS telegram_order_buttons (
  id          TEXT PRIMARY KEY,
  plan_code   TEXT NOT NULL,
  datacenter  TEXT NOT NULL,
  options     TEXT NOT NULL DEFAULT '[]',  -- JSON []string
  config_info TEXT NOT NULL DEFAULT '{}',  -- JSON object
  -- account_id: 触发这条通知的订阅所用的 OVH 账户。
  -- planCode 是分区的(EU / US / CA 三份目录基本不重合),按钮不记账户的话
  -- webhook 回调只能落到"默认账户"上 —— 多账户跨区时等于把欧区机型下到美区账户,
  -- OVH 只会返回空库存而不报错,用户看到的是"排队中"却永远下不出去。
  -- 空串 = 老数据/解析不出账户,回调时退回默认账户(与加列前行为一致)。
  account_id  TEXT NOT NULL DEFAULT '',
  created_at  REAL NOT NULL,               -- unix 秒
  used_at     REAL NOT NULL DEFAULT 0      -- >0 已消费
);
CREATE INDEX IF NOT EXISTS idx_tg_buttons_created ON telegram_order_buttons(created_at);

-- ===========================================
-- telegram_updates: webhook update_id 幂等表
-- Telegram 在未收到 200 时会重投同一条 update,没有这张表就会重复下单。
-- ===========================================
CREATE TABLE IF NOT EXISTS telegram_updates (
  update_id    INTEGER PRIMARY KEY,
  processed_at REAL NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_tg_updates_processed ON telegram_updates(processed_at);
