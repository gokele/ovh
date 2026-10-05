# OVH Console

> 🌐 English | [中文](README.md)

A **sniping + monitoring + management** console for OVH bare-metal servers / VPS / Eco lines.

Watches stock across OVH datacenters in real time and auto-orders purchasable servers per your configuration (datacenter, RAM, storage, bandwidth, vRack). It also manages the full lifecycle of purchased servers (reboot / reinstall / IPMI / BIOS / netboot mode / maintenance tickets / contact changes / bandwidth / firewall / FTP backup / vRack / secondary DNS, and more). Multiple OVH accounts are supported at once, with sniping and monitoring isolated per account.

> Go (Gin) + SQLite backend, Vite/React + TanStack + shadcn-ui frontend, `//go:embed` single-binary deployment (SQLite embedded, cross-platform with zero dependencies), mandatory OvhCredsGate, multi-account support, dual SQLite drivers (`modernc.org/sqlite` pure-Go / `mattn/go-sqlite3` cgo, auto-selected via build tags), **bilingual UI (Chinese/English, follows the browser, can be pinned manually)**, automatic GitHub Releases update checks, and a companion iOS app (one-time pairing codes + independent device tokens; app source not open-sourced for now).

## Download

Grab the binary for your platform from [Releases](https://github.com/gokele/ovh/releases), extract and run — **no Go, Node, or SQLite required**:

| Platform | File |
|---|---|
| Windows x64 | `ovh-server-windows-amd64.exe` |
| Linux x64 | `ovh-server-linux-amd64` |
| Linux ARM64 (Raspberry Pi / ARM cloud hosts) | `ovh-server-linux-arm64` |

The frontend is embedded into the binary via `//go:embed`; once running, open `http://localhost:19998` for the full UI. Remember `chmod +x` on Linux. To build it yourself see [Deployment](#deployment).

A Docker image is also provided (`linux/amd64` + `linux/arm64`):

```bash
docker pull ghcr.io/gokele/ovh:latest
```

## Tech Stack

| Layer | Technology |
|---|---|
| Frontend | Vite 5 + React 18 + TypeScript + TanStack Router + TanStack Query + shadcn-ui + Tailwind + recharts + **react-i18next (zh/en)** |
| Backend | Go 1.21+ + Gin + the official [go-ovh](https://github.com/ovh/go-ovh) SDK |
| Persistence | SQLite (`modernc.org/sqlite` pure-Go / `mattn/go-sqlite3` cgo dual drivers, auto-selected via build tag); credential fields encrypted at rest with AES-256-GCM |
| Notifications | Telegram Bot (long polling, no public endpoint needed) + custom webhooks (DingTalk / Feishu / Bark / self-hosted), multi-channel redundancy |
| Deployment | Single binary (frontend embedded into the Go binary via //go:embed), or run frontend and backend separately |

## Project Structure

```
.
├── server/   # Go backend (Gin, default :19998)
│   ├── main.go
│   ├── webembed_ui.go    # with build tag=ui, embeds the whole web/ dir into the binary
│   ├── webembed_noui.go  # default build, no frontend
│   └── internal/
│       ├── app/          # State aggregation
│       ├── db/           # SQLite layer (schema.sql + per-table CRUD)
│       ├── handlers/     # Gin handlers
│       ├── monitor/      # server restock monitoring
│       ├── vps/          # VPS restock monitoring
│       ├── purchase/     # ordering flow
│       ├── price/        # OVH cart price quoting
│       ├── ovh/          # multi-account client factory routed by account_id
│       ├── notify/       # multi-channel notifications (Telegram / custom webhooks)
│       ├── secret/       # at-rest credential encryption (AES-256-GCM)
│       ├── updater/      # in-place updates: download / verify / self-replace / rollback
│       └── ...
└── web/      # frontend (Vite + TanStack, dev default :19997)
    └── src/
        ├── routes/       # file-based routing
        ├── components/   # shared components + AuthGate / OvhCredsGate
        ├── hooks/        # TanStack Query hooks
        ├── i18n/         # localization: (modular) language packs + error-code translation layer + localized formatting
        └── lib/          # subsidiary tables / OVH datacenter constants / utils
```

See [server/README.md](server/README.md) for detailed backend documentation.

## Deployment

### Option A: Docker (recommended)

```bash
# Grab a docker-compose.yml, change the API_SECRET_KEY inside, then:
docker compose up -d

# Updating
docker compose pull && docker compose up -d
```

Or run directly:

```bash
docker run -d --name ovh-console \
  -p 127.0.0.1:20000:20000 \
  -v "$PWD/data:/data" \
  -e API_SECRET_KEY=your-own-random-string \
  --restart unless-stopped \
  ghcr.io/gokele/ovh:latest
```

The image is built by a GitHub Action on tag push and published to `ghcr.io/gokele/ovh`, for `linux/amd64` and `linux/arm64`.

**All data lives under the single `/data` directory**: the SQLite database, the **database encryption key** (`.env`), caches, and logs. Back up that directory and you have everything.

> ⚠️ Delete that directory = saved OVH credentials and the Telegram token become permanently undecryptable.
> The encryption key is generated automatically on first start and written into `/data/.env`, which lives on the volume; you can also provide `OVH_DB_KEY` explicitly (more controllable for migrations and k8s secrets).

On first start the container chowns `/data` to `PUID:PGID` (default 10001). Bind-mounted host directories are owned by host UIDs; without this step the non-root user inside the container cannot write and the program won't start at all. Pass `PUID`/`PGID` to align with your current host user, or run `docker run --user $(id -u):$(id -g)` (the container then skips ownership handling; you guarantee write access yourself).

**Self-update inside the container is disabled.** A new binary only lands in the container's writable layer; once the container is recreated (`compose up -d`, restart policy, host reboot) you're back to the image's old version — you'd see "update successful" and then the version number revert. The UI shows "new version available + please docker pull" instead of an update button in that case.

### Option B: Single binary

Build the frontend → Vite outputs to `server/web/` → Go with `-tags ui` triggers `//go:embed` of the whole directory into the binary → deploy a single file, double-click to run.

```bash
# 1) Build the frontend into server/web/
cd web
npm ci
npm run build

# 2) Compile the single binary with the frontend (CGO_ENABLED=0 uses pure-Go SQLite; cross-compiling needs no gcc)
cd ../server
CGO_ENABLED=0 go build -tags ui -trimpath \
  -ldflags "-s -w -X github.com/ovh-buy/server/internal/handlers.Version=$(cat ../VERSION)" \
  -o ovh-server .
./ovh-server
```

On Windows, name the output `ovh-server.exe`; for cross-compilation prefix with `GOOS=linux GOARCH=arm64` etc.
**No external SQLite library needed — the binary is self-contained.** It listens on `:19998` by default; open `http://localhost:19998`, and the database is created automatically at `./data/sniper.db`.

> The Release page provides prebuilt binaries for Windows amd64 / Linux amd64 / Linux arm64 if you'd rather not compile.

### Option C: Development (frontend and backend separately)

```bash
# Backend
cd server
go run .                # default :19998

# Frontend (another terminal)
cd web
npm install
npm run dev             # default :19997, /api/* proxied to 19998
```

Open `http://localhost:19997` in the browser.

## First Visit

Two full-screen gates appear in sequence before the main UI:

1. **AuthGate**: enter the `API_SECRET_KEY` from `.env` (or the default if unset, see below)
2. **OvhCredsGate**: forced when no OVH account exists. Fill in an **account name** + OVH subsidiary (Zone) + `APP KEY / APP SECRET / CONSUMER KEY`; `Endpoint` / `IAM` are derived automatically. The backend `POST /api/accounts` persists to the `ovh_accounts` table and actually verifies against OVH before letting you through.

After credentials pass, the frontend immediately prefetches three things in the background (server catalog / catalog / availability), so switching to the server list page **shows data directly — no "loading" state**.

Add more accounts later under "Settings → OVH Accounts". Each account has independent endpoint / credentials / Zone, and **sniping queues, monitoring subscriptions, and auto-orders are all isolated per account**. Account switching has a single site-wide entry (the left sidebar); the catalog, prices, consoles, and ordering account all follow it.

## Configuration

Copy `server/.env.example` → `server/.env` and edit:

```bash
API_SECRET_KEY=...               # key for frontend→backend access, must change
PORT=19998                       # backend listen port
LISTEN_HOST=                     # empty = all interfaces (IPv4+IPv6); 127.0.0.1 loopback only; 0.0.0.0 public
ENABLE_API_KEY_AUTH=true         # when off, no key check on /api/* — local debugging only
GIN_MODE=release                 # debug | release
DEBUG=false                      # true enables debug logging

# --- Database encryption (both optional; auto-handled on first start) ---
OVH_DB_KEY=                      # key encrypting OVH credentials and TG tokens in the DB
                                 # auto-generated and written back to this file if unset
                                 # don't miss it when backing up .env: lose it and stored accounts are gone forever
OVH_ENV_FILE=                    # location of this config file; defaults to .env in the working directory
                                 # in systemd / docker the working dir may differ from the binary dir —
                                 # use an absolute path there, or the key may be "written now, not found next start"

# --- Telegram security (both optional; empty = default behavior) ---
TG_WEBHOOK_SECRET_OPTIONAL=false # true skips secret validation — local debugging only, never on public deployments
TG_ALLOWED_USER_IDS=             # user ids allowed to order from group chats, comma-separated; not needed for private chats
```

OVH credentials do **not** go in env vars — they're entered through the frontend OvhCredsGate / the "OVH Accounts" settings tab into the SQLite `ovh_accounts` table (one row per account with independent endpoint / AppKey / Secret / ConsumerKey / Zone), **stored encrypted**. `.gitignore` rejects all `.env` files from the repo by default.

Notification endpoints are configured in the settings page under "Notification Channels", not via env. Configure at least one of Telegram / custom webhook — as long as one channel works, monitoring keeps running.

## Main Features

### Capability Overview

| Capability | Status | Notes |
|---|---|---|
| **Dark mode** | ✅ | Light / dark / follow-system; toggle in the top bar or via "Settings → Appearance"; per-browser preference |
| **Bilingual UI (zh/en)** | ✅ | Follows the browser language by default; toggle in the top bar / login page, and a manual choice is pinned thereafter. Dates / amounts follow the language, and **backend error messages are bilingual too** (stable error codes + frontend translation by code) |
| Multiple OVH accounts | ✅ | Independent endpoint / credentials / Zone; sniping queues, history, monitoring subscriptions all keyed by `account_id` |
| **Single site-wide account switcher** | ✅ | Only in the left sidebar; catalog / availability / prices / consoles / ordering account all follow |
| **Three-region support (EU / US / CA)** | ✅ | Subsidiary mapping, catalog site, `region` values, planCode suffixes, datacenter sets all resolved per region — never hardcoded to EU |
| **Live stock, direct** | ✅ | Catalog page dots hit OVH's public availability endpoint directly (per the account's site, 60-second freshness) instead of catalog data that can be up to 2 hours stale; falls back to static data with an explicit "stock unknown" notice — never masquerading as out-of-stock |
| Sniping queue | ✅ | Independent tasks per model × datacenter × quantity; pause/resume; fail-fast, never degrades to default config |
| Server restock monitoring | ✅ | Subscribe to planCode + datacenter, Telegram push on state change, **check interval configurable 5–3600 s** |
| VPS restock monitoring | ✅ | Models come from OVH's live catalog (models get retired whole generations at a time; a hardcoded list would silently stop working), Linux / Windows distinction, correct site per subsidiary |
| Server auto-order | ✅ | Triggered by monitoring; ordering account optional — without one it only notifies |
| **VPS auto-order** | ✅ | Same, via `/order/cart/{id}/vps`; OS chosen at order time, `region` resolved per site |
| **Editable subscriptions** | ✅ | Editing config does not reset stock state or history — deleting and recreating would misread "already in stock" as a restock |
| Telegram text ordering | ✅ | 5 message formats, `plancode [datacenter] [qty] [options]` |
| Telegram one-click order buttons | ✅ | Inline datacenter buttons inside stock notifications; parameters persisted, **one-time nonce**, replay-proof |
| Telegram security chain | ✅ | Sender authorization → update_id idempotency → rate limiting → one-time buttons |
| **Multi-channel notifications** | ✅ | Telegram + custom webhooks; monitoring keeps running while any one channel works |
| **Encrypted credentials at rest** | ✅ | AES-256-GCM; key auto-generated on first start into `.env`; old databases migrated automatically |
| **Sniping timing breakdown** | ✅ | Per-phase timing across stock check / cart / assign / item / options / configuration / checkout — answers "which step am I slow at" |
| Backend price quoting | ✅ | `POST /api/servers/{planCode}/price`, real quotes via the OVH cart |
| Purchased server management | ✅ | Power / reinstall (ZFS · soft RAID · custom partitions) / IPMI / BIOS / netboot mode / tasks / maintenance tickets |
| Purchased VPS management | ✅ | Start-stop / reinstall / snapshots / console / password reset / reverse DNS / auto backup |
| Network & protection | ✅ | NICs / OLA / MRTG traffic graphs / DDoS mitigation / firewall / FTP backup |
| Engagement (contract period) | ✅ | Both servers and VPS; destructive operations require double confirmation |
| Privacy mode | ✅ | One-click masking of all IPs / MACs / reverse DNS |
| **App device pairing** | ✅ | One-time pairing codes (2-minute validity, atomic redemption, exactly one winner under concurrency) + independent device tokens (SHA-256 only at rest, individually revocable); per-IP + global failure gates against brute force |
| Auto update check | ✅ | Pulls GitHub Releases and compares versions; shows a ✨ chip on new versions |
| **In-place update** | ✅ | One-click self-replace + restart, SHA256-verified, auto-rollback if the new version fails to start |
| **Choice of console access** | ✅ | HTML5 KVM / **Java KVM (.jnlp)** / SOL (URL) / SOL (SSH), user's choice |
| Config-bound sniping | ❌ | Removed |

### Multi-Account
- **Account management**: CRUD in the settings "OVH Accounts" tab; each record has an independent **name + Zone + endpoint + AppKey/Secret/ConsumerKey**
- **Account isolation**: sniping queues, order history, monitoring subscriptions all carry `account_id`; backend goroutines pick the matching OVH client per account when ordering
- **Cascading cleanup**: deleting an account removes related history / queue entries; the "auto-order account" field on monitoring subscriptions is cleared (the subscription itself survives — notify-only)
- **Default account**: one is flagged `is_default`; creation dialogs fall back to it when nothing is selected
- **Credential verification**: creating / updating an account actually calls OVH `/me`; the result is in the response's `valid` field (failed verification **still persists** — the frontend warns but lets you in, so you can fix credentials later in settings)
- **Subsidiary and endpoint must match regions**: `zone=US` only accepts `ovh-us` (same-region aliases like `kimsufi-*` / `soyoustart-*` are fine). EU / US / CA have fully independent catalogs, prices, stock, and carts; mismatched combos would only blow up at checkout, so they're blocked at account creation

### Sniping
- **Server catalog**: card grid + per-DC stock lights (green available / red out); click to configure and order
- **Live stock**: the catalog page pulls full stock state directly from OVH's public availability endpoint (per the current account's site — EU / US / CA stock is mutually invisible), reusing it within a 60-second freshness window; on failure it falls back to catalog static data and says so at the top of the page — static data can be 2 hours old, and reading it as "out of stock" loses you machines that are actually available
- **Configuration selector**: single-select groups following OVH `addonFamilies` (CPU / RAM / system storage / data storage / bandwidth / vRack), defaults pre-selected
- **Sniping queue**: independent task per server × DC × quantity, **each task bound to one OVH account**; pause / resume / delete; polls OVH stock on the retry interval
- **Fail-fast**: if the user's configuration can't match OVH's currently orderable addons, the whole order fails — never silently degrade to default HDDs
- **In-stock follows the official enum whitelist**: only `\d+H` (delivery-time commitment) counts as in stock; `comingSoon` / `unknown` don't — otherwise you'd keep ordering models that can never be ordered
- **Price display**: priced by **the current account's subsidiary** (currency, tax, catalog all follow it), computed locally from the catalog without the cart flow. No cross-subsidiary price comparison — that dropdown once made people think they'd switched catalogs, and ordering from it gets rejected by OVH
- **Timing breakdown**: each round is timed per phase — stock check / cart creation / assignment / add item / required config / hardware options / checkout. After losing a snipe, the only useful information is "which step was slow" — without those numbers, "OVH had no stock", "my network is slow", and "one step stalled for 8 seconds" all look identical while demanding completely different actions. Every history row expands into a breakdown; the queue page shows each link's last-round outcome and total time
- **Backend quoting fallback**: `POST /api/servers/{planCode}/price` (body `{datacenter, options}`, account via `?account=<id>`). Real OVH cart quotes returning tax-inclusive / tax-exclusive / currency — used when local catalog math fails (missing items / OVH schema changes / addon unavailable in the target DC) and as an entry point for external scripts that shouldn't have to replicate the price formula

### Monitoring
- **Server restock**: subscribe to planCode + DC combinations; state changes push to Telegram. **Auto-order can target a specific account**; without one, notify-only
- **Configurable check interval**: edit in place on the monitoring page; valid range 5–3600 seconds (out-of-range values are clamped and the effective value reported), persisted in the `kv` table across restarts. The 5-second floor exists because OVH's availability API itself is cached — faster just hits rate limits
- **VPS restock**: same, for the OVH VPS line (Linux / Windows images distinguished). Model lists come from the **live OVH catalog**, not hardcoded — VPS models get retired whole generations at a time (the 2025 generation has fully left the orderable catalog); a subscription watching a discontinued model never fires and the only symptom is "always out of stock". Existing subscriptions pointing at discontinued models are flagged
- **VPS auto-order**: actually orders on restock (`cart → assign → POST /vps → required config → checkout`). The OS (`vps_os`) is an order-time configuration, not something you pick after purchase, hence it's in the subscription. Orders only on the "out → in" transition and stops after one success
- **Editable subscriptions**: both server and VPS subscriptions can be edited **without resetting stock state or history**. Delete-and-recreate clears `LastStatus`; the next round would read "was in stock all along" as a restock transition and fire a notification (plus a real order) for something that never happened
- **Multi-channel notifications**: Telegram + custom webhooks. Monitoring keeps running while any one channel works; it only stops when all are down — previously a Telegram outage lost you more than messages, it lost the whole monitor
- **History timeline**: complete change log per subscription

### Purchased Server Management
- **Account isolation**: all `/server-control/*` requests automatically carry `?account=<id>` via an axios interceptor, following the account selected in the left sidebar — no per-hook changes needed
- **Overview**: hardware info + service expiry + IPs / NICs + MRTG traffic graphs
- **Power / system**: reboot / reinstall (ZFS / soft RAID / custom partitions) / IPMI console / netboot mode / SPLA Windows unlock / task list / BIOS / install progress. Reinstall has a per-service `TryLock` against double-submits
- **Maintenance**: maintenance records + hardware replacement tickets (disk / memory / cooling) + contact changes (token email confirmation)
- **Advanced** (9 sub-tabs): Burst / firewall / FTP backup / secondary DNS / virtual MAC / vRack / orderable upgrades / add-on options / IP specs
- **Privacy mode**: one-click masking of all IPs / MACs / reverse DNS hostnames

### Misc
- **Account management**: balance / refunds / email history (follows the current account)
- **Order history**: orders + prices + countdowns + direct OVH order links, account chip per row
- **Detailed logs**: live refresh, filter by level / keyword
- **Auto update check**: the dashboard calls `GET /api/version/check-update` once on mount to compare against GitHub releases; new versions show a ✨ chip next to the version number linking to the release; the backend is purely passive — no goroutines, no timers

## Persistence

All business data lives in SQLite (`data/sniper.db`), 11 tables:

| Table | Purpose |
|---|---|
| `kv` | Singleton config (TG token / notification webhook URLs / server & VPS check intervals / long-poll offset and other non-account config), **secret fields encrypted at rest** |
| `ovh_accounts` | OVH accounts (independent endpoint / AppKey / Secret / ConsumerKey / Zone / is_default), **the three credential fields encrypted** |
| `queue` | Sniping queue (`account_id` FK) |
| `history` | Order history (`account_id` FK) |
| `servers` | OVH server catalog cache (rewritten per refresh, 2h TTL) |
| `catalogs` | One OVH public catalog per subsidiary (2h TTL); catalog page prices come from here |
| `monitor_subscriptions` | Server restock subscriptions (`auto_order_account_id` FK) |
| `vps_subscriptions` | VPS restock subscriptions (same) |
| `server_aliases` | Local server aliases (account_id + service_name composite PK, never sent to OVH) |
| `telegram_order_buttons` | TG one-click-order button UUID → order parameters; `used_at` as one-time nonce |
| `telegram_updates` | Telegram `update_id` idempotency table, prevents redelivery double-orders |

Logs still go to a JSON file (`data/logs/app.log.json`), not SQLite.

Encrypted fields carry the `enc:v1:` prefix; anything without it is treated as plaintext — databases upgraded from old versions are all plaintext and must not be blindly decrypted. First start migrates existing plaintext to ciphertext in place, idempotently; repeated starts don't re-encrypt.

## Caching Strategy

| Data | Backend TTL | Frontend staleTime | Background polling | Refresh trigger |
|---|---|---|---|---|
| Server catalog | 2h (SQLite + in-memory ServerCache) | 2h | ❌ purely access-driven | next access after expiry / manual refresh button |
| OVH catalog (prices) | 2h (SQLite `catalogs` table) | 2h | ❌ | same |
| Live availability | — (frontend hits OVH's public API directly) | 60 seconds | ❌ | access-driven; falls back to catalog static data with a notice on failure |

Live stock is fetched by the **frontend**, directly from each OVH site's public `datacenter/availabilities` endpoint (no credentials, doesn't consume account quota), choosing the EU / US / CA source by the current account's site — the three sites' stock is mutually invisible, and following the wrong one means a permanently "out of stock" page. The static availability in the backend's `/api/servers` is fallback only.

Nothing calls OVH at startup; existing SQLite data is loaded into memory. `ServerCache` rebuilds timestamps from SQLite's real `updated_at`, so stale data is never mistaken for freshly refreshed.

## Security / Auth

- All backend `/api/*` endpoints (minus a small whitelist like `/health` / `/version` / `/version/check-update`) require the `X-API-Key` header
- Two full-screen gates: AuthGate (API key) + OvhCredsGate (at least one OVH account)
- The API key lives in browser localStorage; cleared automatically on expiry with a re-entry prompt
- OVH credentials go into the SQLite `ovh_accounts` table via the OvhCredsGate / the "OVH Accounts" settings tab
- `.gitignore` rejects all `.env` files by default (only `*.env.example` allowed), plus `*.db` / `data/` / `logs/`
- **Encrypted credentials at rest**: AppKey / AppSecret / ConsumerKey in `ovh_accounts` and the Telegram token in `kv` are AES-256-GCM encrypted. The key comes from `OVH_DB_KEY` if set, otherwise it's generated on first start and written to `.env` (mode 0600)
- ⚠️ **The encryption defends against the "db file leaked alone" class** — backups synced to cloud drives, copying the whole directory to another machine, zipping `data/` to share for troubleshooting. It does **not** defend against `.env` and the db leaking together, in which case encryption is moot. And `.env` is precisely the file most likely to be committed by accident or pasted into an issue
- **Refuses to start when the key is missing**: if ciphertext exists but the key can't be found, the program halts and explains what to do — otherwise you'd see "accounts are all there but every OVH call fails signature checks", nobody would guess it's a key problem, and re-entering credentials would overwrite the old ciphertext, destroying the last chance of recovery. If it's truly unrecoverable, start with `OVH_DB_KEY_RESET=1` and re-enter those accounts

### App Device Pairing (built into the backend, used by the iOS app)

The companion iOS app (source not open-sourced for now) connects via one-time pairing codes and never holds the `API_SECRET_KEY`:

- **Pairing flow**: the web UI's "Settings → App Pairing" generates an 8-character code (2-minute validity) → the app enters it (or follows a deep link) → the backend exchanges it for a **device token**; all subsequent requests use `Authorization: Bearer <device token>`
- **One-time guarantee**: redemption is a single atomic `UPDATE` (WHERE unused and unexpired); under 50 concurrent attempts on the same code exactly one succeeds. Redemption and device creation share one transaction — failures roll back, so a code is never wasted
- **Token security**: 32 random bytes; only its SHA-256 is stored. Lose your phone → revoke that one device on the web; other devices stay connected
- **Anti brute force**: per-IP 5 failures → 5-minute lock, plus a global 30-failures-per-minute gate (against distributed attempts that rotate IPs); every pairing success and failure is audit-logged

### Localization Implementation Notes

- All UI copy goes through `react-i18next`; language packs are split by module under `web/src/i18n/locales/` (Chinese is the source of truth; the English pack is structurally enforced against it by TypeScript types — a missing translation is a compile error)
- **Bilingual backend messages**: the backend returns a stable error `code` alongside `error` / `message` (content-addressed, so identical copy shares one code across endpoints); the frontend translates by code into the current language. Codes absent (dynamically composed messages, OVH originals) pass through verbatim
- Dates / relative times / amounts go through `Intl` + date-fns locales — no format mismatches across languages
- Adding a language = adding one language-pack file; no code changes

### Telegram Ingestion & Security Chain

Telegram messages arrive exclusively via **long polling** (`getUpdates`): the program pulls from `api.telegram.org` itself. No public domain or certificate needed — machines behind home broadband / NAT / without domains can all use one-click ordering.

> Webhooks (Telegram pushing to you) were supported earlier and have been removed. They require a public HTTPS domain + trusted certificate, ports restricted to 443/80/88/8443, the callback endpoint placed in the auth whitelist (Telegram can't send `X-API-Key`), a `secret_token` to prove origin, and a compatibility mode for "secret not yet registered" on old deployments. Long polling has no inbound endpoint — the forged-origin problem disappears along with that entire apparatus. On upgrade the backend automatically calls `deleteWebhook`; no manual steps.

The remaining validation chain:

| Stage | Effect | Failure response |
|---|---|---|
| **Sender authorization** | Only the configured `tgChatId` chat; group chats additionally require the user id in `TG_ALLOWED_USER_IDS` | `403 unauthorized_actor` |
| **update_id idempotency** | Dedup via the `telegram_updates` table. The offset is only confirmed on the **next** `getUpdates`; crash / self-update restart before advancing it means the update is redelivered — without this, one version upgrade could double-order | `200 {"duplicate":true}` |
| **Rate limiting** | Max 8 messages per 10 seconds per chat | `429 rate_limited` |
| **One-time buttons** | One-click-order button parameters persist to `telegram_order_buttons`; `used_at` is claimed atomically: **one order per button**, void after 24h; failed enqueue returns the claim for retry | `409 button_already_used` / `410 button_expired` |

Button parameters used to live only in process memory — a restart killed every button and they could be replayed indefinitely; persisting them fixed both.

**Single-instance constraint**: only one process may poll updates for a given Bot Token. Two instances kick each other off — buttons work intermittently, messages drop at random. The backend detects this and says so explicitly in logs and the settings page.

## Multi-Region (EU / US / CA) Notes

> **Accounts switch once, in the left sidebar; the whole site follows.**
> Catalog, datacenter stock lights, price currency, server/VPS consoles, ordering account — everything renders per the current account's site.
> There's only one entry point because the old UI had separate unsynced selectors on the catalog page, order dialog, and console pages — and the three sites' catalogs are mutually invisible (the same machine is `24sk602` in EU and `24sk602-v1-us` in US), so "browse with account A, order with account B" was one click away, and such tasks always get rejected.
> Likewise, datacenters listed are only those actually selectable for that model on the current site — never a fixed list of 16.

The three sites are independent systems; the same model has different planCodes, prices, and orderable regions per subsidiary:

| Item | EU (`ovh-eu`) | US (`ovh-us`) | Notes |
|---|---|---|---|
| planCode | `24sk202` | `24sk202-us` / `24sk202-eu` | US catalog models all carry suffixes; querying US availability with an EU planCode returns empty |
| `region` config value | `canada` / `europe` | **`united_states`** | Sent via `POST /order/cart/{id}/item/{id}/configuration` at order time |
| APAC models (sgp/syd/ynm) | `canada` | — | OVH buckets APAC datacenters under `canada`; there is **no** `apac` value |
| Catalog site | `eu.api.ovh.com` | `api.us.ovhcloud.com` | Mapped centrally by `ovh.CatalogBaseURLForSubsidiary` |

**VPS is also three independent systems, differing differently from bare metal** (from the public catalogs):

| Item | EU / CA sites | US site |
|---|---|---|
| `region` values | `canada` / `europe` | **only `united_states`** |
| Datacenter set | 11 (incl. BHS / SGP / SYD / YNM) | `vps-xxx` only has `US-EAST-VA` / `US-WEST-OR` |
| Buying EU / CA datacenters | same product | requires the separate **`-eu` / `-ca`-suffixed product** |

So VPS orders never guess `region`: the cart's `requiredConfiguration` is consulted first — one value means use it; multiple means pick by datacenter (BHS/SGP/SYD/YNM→`canada`, else→`europe`; that table was read out of OVH's own `-ca` / `-eu` variant catalogs). Unrecognized datacenters submit no `region` at all and let OVH default — submitting a wrong one kills the whole order.

On bare metal, valid `region` values are determined by **(subsidiary, planCode)** rather than datacenter: a US account ordering a European datacenter (`gra`/`fra`) still requires `united_states`. So the code does no static "datacenter → region" mapping; [catalog.ResolveRegion](server/internal/catalog/region.go) reads it from the official catalog's `configurations[].values` with a 2-hour in-memory cache, falling back to `ovh.RegionForDCInSubsidiary`'s static table only when the catalog can't be fetched. [region_test.go](server/internal/catalog/region_test.go) has online test cases exhaustively validating every (plan × datacenter) combination across both regions' catalogs.

## OVH API Integration

The ordering flow follows OVH's official [order-cart-examples](https://github.com/ovh/order-cart-examples) exactly:

```
POST /order/cart                         → cartId
POST /order/cart/{id}/assign
POST /order/cart/{id}/eco                → itemId
POST /order/cart/{id}/item/{itemId}/configuration × 3  (datacenter / os / region)
POST /order/cart/{id}/eco/options × N
GET  /order/cart/{id}/summary
POST /order/cart/{id}/checkout
```

VPS is a different chain (note: not `/eco`, and different required configurations):

```
POST /order/cart                         → cartId
POST /order/cart/{id}/assign
POST /order/cart/{id}/vps                → itemId   (duration / pricingMode taken from GET /order/cart/{id}/vps)
GET  /order/cart/{id}/item/{itemId}/requiredConfiguration
POST /order/cart/{id}/item/{itemId}/configuration   (vps_datacenter required / region / vps_os)
POST /order/cart/{id}/checkout
```

Orderable models come from the public catalog `GET /order/catalog/public/vps?ovhSubsidiary=XX`, filtered by OVH's own `order-funnel:show` flag — no planCode regex generation-guessing. Guessing means a release every generation change, and you can't tell "discontinued" from "regex missed it".

Price calculation = base plan monthly fee + the selected addon per family summed (`ovhjk/parser/price.go` ported 1:1 into the frontend at `web/src/hooks/use-availability.ts`).

## Ports

| Service | Port |
|---|---|
| Go backend (production single binary / dev) | **19998** |
| Vite dev server (dev only) | 19997 |

## FAQ & Troubleshooting

Everything below was actually hit at some point; look up by symptom.

### The UI turned English / how do I change language

UI language **follows the browser by default**: English browsers (or English-first system languages) get English, Chinese browsers get Chinese. The language button in the top bar (also on the login page) switches manually; **after a manual switch the choice is pinned** and no longer follows the browser. To restore "follow the browser", set your browser language back and clear the site's localStorage (delete the `ovh-lang` key).

### Keep getting permission errors / `This call has not been granted`

The token was requested without full **Rights**. `GET`-only tokens can view but not buy — ordering, config changes, reinstalls all fail.

Re-apply at the corresponding site's `/createToken/` with these four rights:

```
GET     /*
POST    /*
PUT     /*
DELETE  /*
```

Choose **Unlimited** validity; anything else expires and you redo it.
Source: [OVHcloud API first steps](https://docs.ovhcloud.com/en/guides/manage-and-operate/api/first-steps) —
"In order to allow all OVHcloud APIs for an HTTP method, put an asterisk (`*`) into the field".

### Credentials filled in but verification fails / persistent 401

Two possibilities:

1. **Token requested on the wrong site**. EU / US / CA tokens are mutually incompatible; an EU token on a US account never logs in. Pick the subsidiary in the UI first, then follow the link to apply — the link follows the subsidiary.
2. **Consumer Key not activated or expired**. After applying, OVH gives an authorization link that must be opened to confirm; expired ones need re-applying.

### Model not found / "not found on any of the three sites"

- First check planCode **case**. It's case-sensitive and phone keyboards auto-capitalize. The program does a case-insensitive lookup against the catalog and tells you the correct spelling.
- Then check the account's region. US models carry `-us` / `-eu` / `-ca` suffixes; EU and CA don't.

### Monitoring subscription created but never triggers

Most likely a **mistyped datacenter code**. Check the "orderable in …" hint under the input before filling. Case-insensitive; half-width and full-width commas both work — but typos produce no warning. In that case the log has a "specified datacenters … none in OVH's returned list" entry; search "datacenter" on the logs page.

### Can't select options when ordering via Telegram

Early parsers required **half-width** commas between options; full-width `，` from Chinese IMEs collapsed the whole string into one token and dropped it. Half-width/full-width commas, enumeration commas, and spaces all work now. Format:

```
24ska01 gra 2 ram-64g,softraid-2x960ssd
model   DC  qty options (multiple allowed)
```

Datacenter, quantity, and options are **order-independent**; `@accountname` can go anywhere too.

### Locked yourself out of the Telegram bot

If the admin whitelist (chat ID) on the settings page was separated by full-width commas, the whole whitelist silently failed. Both comma styles are accepted now, but old configs should be re-saved once on the settings page.

### "Over the limit" when creating tasks

Max 20 units per datacenter, 60 tasks per creation, 500 tasks in the queue.
Each task is a **real order attempt** — hitting these numbers almost always means a typo in quantity. If you genuinely need more, create in batches or clean finished tasks from the queue first.

### Locked out of the server

Server console → Power & system → **one-click rescue mode**. It collapses OVH's four steps into one button: switch netboot to rescue → set the password email → reboot.

The step people miss manually is the **final reboot** — netboot changed but no reboot means the machine is still on the old system and you'll be puzzled why SSH won't connect. The reboot here is sent automatically.

Inside: after ~3–5 minutes the root password arrives at the email you set (empty = the OVH account's contact email); SSH in and mount the disk to fix things. The rescue system is an independent temporary Linux booted from the network — **it does not touch disk data**. When done, use the same entry to click "exit rescue mode" — otherwise every reboot lands in rescue again.

### "Too many consecutive key errors" and locked out

Since v0.1.32, 10 consecutive wrong keys from one source triggers a 5-minute refusal — this blocks brute force (this service can order servers with your OVH account; with no friction, anyone who can reach the port gets thousands of guesses per second).

The key is `API_SECRET_KEY` in the backend `.env`; the default is `123456` if never set (strongly recommended to change; the startup log warns in red). Wait out the cooldown and enter the correct one — **a correct entry resets the counter immediately**. Rate limits are per source IP; someone else's mistakes won't lock you out.

### Logs seem to be missing a chunk when stopping / restarting the container

v0.1.31 and earlier: the program didn't handle SIGTERM, which is exactly what `docker compose down` / `up -d` sends. Go's default is immediate termination; in-memory logs not yet flushed were **lost entirely** (in testing: the log file wasn't even created). Since v0.1.32 it drains in-flight requests, flushes logs, and closes the DB cleanly before exiting.

### Where are the database / config, and how to back up

- SQLite: `sniper.db` in the working directory (the mounted volume under Docker)
- Encryption key: `OVH_DB_KEY` in `.env`

**Back up both together**. Only backing up `sniper.db` and losing `OVH_DB_KEY` means stored OVH credentials and Telegram tokens are undecryptable forever — you'd have to revoke and re-enter them from the OVH console.
