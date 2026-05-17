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

-- ===========================================
-- config_sniper_tasks: 配置绑定狙击任务
-- ===========================================
CREATE TABLE IF NOT EXISTS config_sniper_tasks (
  id                TEXT PRIMARY KEY,
  api1_plan_code    TEXT NOT NULL,
  bound_config      TEXT NOT NULL DEFAULT '{}',  -- JSON
  match_status      TEXT NOT NULL,               -- matched / pending_match / completed
  matched_api2      TEXT NOT NULL DEFAULT '[]',  -- JSON []string
  known_plancodes   TEXT NOT NULL DEFAULT '[]',  -- JSON []string
  enabled           INTEGER NOT NULL DEFAULT 1,
  last_check        TEXT,                        -- nullable
  created_at        TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sniper_match_status ON config_sniper_tasks(match_status);
