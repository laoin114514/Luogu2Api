# syntax=docker/dockerfile:1
#
# Luogu2Api 镜像：一个进程同时提供 API 与管理台（Gin 在 /dashboard/ 托管 Vite 产物）。
#
# 分三段构建：
#   1) dashboard：Node + pnpm 构建 Pool-Dashboard
#   2) builder  ：Go 静态编译 cmd/api（CGO_ENABLED=0，跑在 alpine 上不需要 glibc）
#   3) 运行时   ：alpine + 二进制 + 静态文件，非 root 运行
#
# 构建与运行都只认环境变量：二进制刻意不读 .env（见 README「环境变量」），
# 所以镜像里不放、也不该放配置文件，配置由 compose / docker run 注入。
#
#   docker build -t luogu2api .
#   docker compose up -d --build      # 常规用法，见 docker-compose.yml
#
# 前置条件：
#   * pkg/luoguClient 是 git submodule（go.mod 用 replace 指向它），
#     克隆后先 git submodule update --init --recursive，否则编译这一层就过不去。
#   * 需要 BuildKit（Docker 23+ 默认开启）：--platform 与 cache mount 都依赖它。

############################
# 1) 管理台：Vite + Vue3
############################
FROM --platform=$BUILDPLATFORM node:22-alpine AS dashboard

# corepack 按 package.json 的 packageManager 字段拉取 pnpm（当前 11.24.0，要求 Node >= 22.13），
# 版本跟着仓库走，不用在 Dockerfile 里再写一遍。
RUN corepack enable

WORKDIR /src/Pool-Dashboard

# 先只拷依赖清单：改源码不会让 pnpm install 这层缓存失效
COPY Pool-Dashboard/package.json Pool-Dashboard/pnpm-lock.yaml ./
RUN --mount=type=cache,id=pnpm-store,target=/root/.local/share/pnpm/store \
    pnpm install --frozen-lockfile

# 拷源码构建。vite.config.ts 的 base 固定为 /dashboard/，产物落在 Pool-Dashboard/dist，
# 由 Go 进程按同源方式托管（前端调管理接口因此不需要跨域配置）。
COPY Pool-Dashboard/ ./
RUN pnpm build

############################
# 2) Go 静态编译
############################
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

# 交叉编译目标：BuildKit 会自动填 TARGETOS/TARGETARCH（docker build --platform linux/arm64 ...），
# 这里的默认值只是给非 BuildKit 场景兜底。
# 镜像 tag 用 1.26（跟着补丁号走，保证 >= go.mod 要求的 1.26.2）；要完全可复现就钉成 golang:1.26.2-alpine。
ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /src

# 依赖清单。因为 replace 指向本地子模块，go mod download 也需要读到它的 go.mod/go.sum。
COPY go.mod go.sum ./
COPY pkg/luoguClient/go.mod pkg/luoguClient/go.sum ./pkg/luoguClient/
RUN --mount=type=cache,id=gomod,target=/go/pkg/mod \
    go mod download

# 全部源码（含 submodule 里的 SDK 源码）
COPY . .

RUN --mount=type=cache,id=gomod,target=/go/pkg/mod \
    --mount=type=cache,id=gobuild,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/luogu2api ./cmd/api

############################
# 3) 运行时
############################
FROM alpine:3.22

LABEL org.opencontainers.image.title="Luogu2Api" \
      org.opencontainers.image.description="洛谷 Web API + 账号号池（Gin + GORM + MySQL）" \
      org.opencontainers.image.source="https://github.com/laoin114514/luogu2api"

# ca-certificates：访问洛谷与 OCR 服务都要 HTTPS
# tzdata：DSN 默认 loc=Local，没有时区数据的话时间列会按 UTC 落库
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 10001 app \
    && adduser -S -D -H -u 10001 -G app -h /app app

WORKDIR /app

# WORKDIR 必须与二进制找静态目录的方式一致：main.go 用的是 cwd 下的
# Pool-Dashboard/dist（filepath.Abs 相对路径），所以两个 COPY 的目标路径不能改。
COPY --from=builder /out/luogu2api /app/luogu2api
COPY --from=dashboard /src/Pool-Dashboard/dist /app/Pool-Dashboard/dist

# 容器内监听地址固定 :8080：要换端口请改宿主机映射（compose 的 HTTP_PORT）。
# 覆盖 HTTP_ADDR 的话记得同步下面的 HEALTHCHECK。
ENV HTTP_ADDR=:8080 \
    TZ=Asia/Shanghai

USER app
EXPOSE 8080

# 探活只看 HTTP 服务本身：/healthz 在"号池没有在线账号"时按设计返回 503（degraded），
# 首次部署还没导账号时会把容器判成 unhealthy；/api/v1/** 又全部要 ADMIN_TOKEN，
# 探针既拿不到也不该拿令牌。所以用同样公开、但恒定 200 的存活探针 /livez；
# DB 与号池的真实健康看 /healthz 与日志。
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/livez || exit 1

# 参数直接透传：-migrate（只做结构变更）、-schema-status（只打印差异）、-addr（改监听地址）
ENTRYPOINT ["/app/luogu2api"]
