# syntax=docker/dockerfile:1

# ─────────────────────────────────────────────────────────────────────────────
# OVH 控制台镜像
#
# 三段式：装前端依赖 → 构建前端 → 构建 Go（带 -tags ui 把前端塞进二进制）。
# 最终镜像只有一个静态二进制，不含 node、不含 Go 工具链。
#
# 为什么能做到静态且无 CGO：数据库层有两套驱动，CGO_ENABLED=0 时走
# modernc.org/sqlite 这个纯 Go 实现。所以多架构构建不需要 QEMU、
# 不需要交叉编译器，直接用 Go 自己的交叉编译。
# ─────────────────────────────────────────────────────────────────────────────

# ---- 1. 前端依赖（单独一层，package.json 没变就不会重装）----
FROM --platform=$BUILDPLATFORM node:22-alpine AS webdeps
WORKDIR /app/web
COPY web/package.json web/package-lock.json* ./
# 有 lock 就用 npm ci（可复现），没有就退回 install
RUN if [ -f package-lock.json ]; then npm ci; else npm install; fi

# ---- 2. 构建前端 ----
FROM --platform=$BUILDPLATFORM node:22-alpine AS webbuild
WORKDIR /app/web
COPY --from=webdeps /app/web/node_modules ./node_modules
COPY web/ ./
# vite.config 里 outDir 指向 ../server/web，所以产物落在 /app/server/web
RUN npm run build

# ---- 3. 构建后端 ----
FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine AS gobuild
WORKDIR /app/server
# 依赖单独一层：go.mod/go.sum 没变就命中缓存
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
# 前端产物必须在 go build 之前就位 —— //go:embed all:web 是编译期读的
COPY --from=webbuild /app/server/web ./web
COPY VERSION /app/VERSION

ARG TARGETOS
ARG TARGETARCH
# CGO_ENABLED=0：纯 Go SQLite，静态链接，交叉编译不需要 gcc
# -tags ui：触发 //go:embed，把前端打进二进制
# 版本号注入的路径必须和 build.sh 一致，否则自更新会拿 "dev" 去比较版本
RUN VERSION="$(cat /app/VERSION)" && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -tags ui -trimpath \
      -ldflags "-s -w -X github.com/ovh-buy/server/internal/handlers.Version=${VERSION}" \
      -o /out/ovh-server .

# ---- 4. 运行时 ----
FROM alpine:3.20

# ca-certificates：要访问 OVH / Telegram / GitHub 的 HTTPS
# tzdata：日志和「检测时间」用的是本地时区，没有它容器里全是 UTC
# su-exec：entrypoint 以 root 修完 /data 归属后用它降权
RUN apk add --no-cache ca-certificates tzdata su-exec

COPY --from=gobuild /out/ovh-server /usr/local/bin/ovh-server

# 启动脚本直接写在这里，不单独放一个文件 —— 它只服务于这个镜像，
# 散在仓库根目录只会让人以为那是个能单独跑的东西。
COPY <<'ENTRYPOINT_EOF' /usr/local/bin/docker-entrypoint.sh
#!/bin/sh
# 容器启动收尾：把 /data 的归属弄对，再降权运行。
#
# 为什么需要这一步：
# bind mount（-v /宿主/路径:/data）进来的目录归属是**宿主机**那边的 uid，
# 而容器里跑的是非 root 用户。不处理的话第一件事就是
# "mkdir /data/cache: permission denied"，程序根本起不来 ——
# 而这个目录里装着数据库、加密密钥和日志，写不进去就等于数据全丢。
#
# 用 PUID/PGID 覆盖默认 uid，和常见的自建服务镜像一个习惯；
# 也可以直接 docker run --user，那样这段会自动跳过。
set -e

APP=/usr/local/bin/ovh-server
DATA="${DATA_DIR:-/data}"

if [ "$(id -u)" = "0" ]; then
  PUID="${PUID:-10001}"
  PGID="${PGID:-10001}"

  # 组/用户可能已存在（重启同一个容器），存在就沿用
  if ! getent group "$PGID" >/dev/null 2>&1; then
    addgroup -g "$PGID" ovh 2>/dev/null || true
  fi
  if ! getent passwd "$PUID" >/dev/null 2>&1; then
    adduser -D -H -u "$PUID" -G "$(getent group "$PGID" | cut -d: -f1)" ovh 2>/dev/null || true
  fi

  mkdir -p "$DATA"
  # 只在归属不对时才 chown：数据多了之后递归 chown 很慢，
  # 每次启动都来一遍会让重启变成几十秒。
  CURRENT_UID="$(stat -c %u "$DATA" 2>/dev/null || echo -1)"
  if [ "$CURRENT_UID" != "$PUID" ]; then
    echo "[entrypoint] 把 $DATA 的归属改成 $PUID:$PGID（首次挂载或 PUID 变了）"
    chown -R "$PUID:$PGID" "$DATA" || {
      echo "[entrypoint] ⚠️ chown 失败。如果 $DATA 是只读挂载或网络文件系统，"
      echo "[entrypoint]    请改用 docker run --user $(id -u):$(id -g)，或把宿主目录 chown 成 $PUID:$PGID"
    }
  fi

  exec su-exec "$PUID:$PGID" "$APP" "$@"
fi

# 已经是非 root（用了 --user）：不做任何归属处理，直接跑。
# 这时候宿主目录的写权限得由调用方保证 —— 我们没有能力改。
if [ ! -w "$DATA" ]; then
  echo "[entrypoint] ⚠️ $DATA 当前用户($(id -u):$(id -g))写不进去。"
  echo "[entrypoint]    数据库、加密密钥、日志都要写在这里，起不来是必然的。"
  echo "[entrypoint]    把宿主目录 chown 成这个 uid，或者去掉 --user 让容器自己处理。"
fi
exec "$APP" "$@"
ENTRYPOINT_EOF
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

# 所有持久化数据都在这一个目录下：SQLite、缓存、日志。
# 一个卷就够，不需要分别挂。
ENV DATA_DIR=/data

# ⚠️ 这一行很关键，不要删。
#
# 数据库加密密钥在没有 OVH_DB_KEY 时会自动生成，而生成逻辑是**优先写 .env**，
# 只有写不进去才退回 <DATA_DIR>/.dbkey。容器里工作目录的 .env 是可写的，
# 于是密钥会落在镜像层里 —— 容器一重建就没了。
# 而密钥丢了程序会拒绝启动（库里有密文却解不开，照常启动只会表现为
# 「账户都在但每次调 OVH 都报签名错误」，那时候重新录入凭据会覆盖旧密文，
# 最后一点恢复余地也没了）。
# 把 .env 指到卷上，密钥就跟着数据一起持久化。
ENV OVH_ENV_FILE=/data/.env

ENV PORT=20000
# 容器内监听所有网卡由 Docker 的端口映射来收口；
# 真正的访问控制靠 API_SECRET_KEY（见 README，别用默认值）
ENV LISTEN_HOST=0.0.0.0
ENV TZ=Asia/Shanghai

# 告诉程序自己跑在容器里 —— 据此停用自更新。
#
# 容器里的自更新是错的：新二进制只会写进容器的可写层，容器一重建
# （compose up -d、重启策略拉起、宿主机重启）就回到镜像里的旧版本，
# 用户会看到「更新成功」之后版本号又变回去。
# 容器的更新方式是拉新镜像重建，数据在卷上不受影响。
ENV OVH_IN_CONTAINER=1

# 以 root 启动，entrypoint 修完 /data 的归属后自己降到 PUID:PGID（默认 10001）。
#
# 不在这里写 USER 的原因：bind mount 进来的宿主目录归属是宿主机那边的 uid，
# 容器里的非 root 用户写不进去 —— 第一件事就是 permission denied，
# 而那个目录装着数据库、密钥和日志。
# 想固定 uid 就传 PUID/PGID，或者直接 docker run --user（那样 entrypoint 会跳过这段）。
WORKDIR /data
VOLUME ["/data"]
EXPOSE 20000

# /api/health 不需要鉴权（在白名单里），适合做健康检查
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD wget -qO- "http://127.0.0.1:${PORT}/api/health" >/dev/null 2>&1 || exit 1

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
