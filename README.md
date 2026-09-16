# Luogu2Api

基于 [luoguClient](https://github.com/laoin114514/luoguClient) 的洛谷 Web 服务。

技术栈：**Gin + GORM + MySQL**，按 Go 常规分层组织。当前只提供健康检查接口（`/healthz`），
业务接口待设计完成后再在 `internal/router/route.go` 注册。

## 目录结构

```
Luogu2Api/
├── cmd/
│   └── api/main.go              # 入口：加载配置 → 初始化基础设施 → 组装依赖 → 启停服务
├── internal/
│   ├── client/luogu.go          # 外部依赖：洛谷 SDK 适配（唯一接触 SDK 的包）
│   ├── config/config.go         # 配置加载与校验（环境变量）
│   ├── handler/health.go        # HTTP 处理层：解析请求、调用 service、渲染响应
│   ├── middleware/middleware.go # 请求 ID、slog 访问日志
│   ├── model/problem.go         # GORM 实体 + AutoMigrate 注册
│   ├── repository/mysql.go      # GORM/MySQL 初始化、连接池、迁移
│   ├── repository/health.go     # 数据访问：数据库 ping
│   ├── response/response.go     # 统一响应体 {code,message,data}
│   ├── router/route.go          # 路由表（当前仅 /healthz）
│   └── service/health.go        # 业务层：汇总依赖状态
├── configs/env.example          # 环境变量样例
├── scripts/check.ps1            # 一次跑通两个 module 的 build/vet/test
└── pkg/luoguClient/             # 洛谷 SDK —— git submodule（独立仓库、独立发版）
```

## 分层与依赖方向

```
cmd/api  ──►  router  ──►  handler  ──►  service  ──►  repository (GORM/MySQL)
                                            └────────►  client     (洛谷 SDK)
                     middleware / response（横切，被各层复用）
                     config / model（被各层依赖，不依赖上层）
```

| 层 | 职责 | 约束 |
|---|---|---|
| `handler` | 参数解析、调用 service、渲染响应 | 不写业务逻辑，不出现 GORM/SQL |
| `service` | 业务编排、事务边界 | 只依赖自己声明的窄接口（如 `DBPinger`、`LuoguSession`），不依赖 gin/GORM |
| `repository` | 数据访问 | **唯一**直接使用 GORM/MySQL 的包 |
| `client` | 外部服务访问 | **唯一**直接使用洛谷 SDK 的包 |
| `model` | 表结构 | 只放结构体与 `TableName()`；`model.All()` 是迁移的唯一来源 |
| `router` | 路由表 | 只做注册与依赖注入 |

新增一个业务接口的推荐路径：`model` 加实体 → `repository` 加查询 → `service` 加业务方法
→ `handler` 加处理器 → `router` 注册 `r.Group("/api/v1")`。

## 环境变量

见 `configs/env.example`。常用项：

| 变量 | 默认值 | 说明 |
|---|---|---|
| `APP_ENV` | `dev` | `prod` 时 gin 走 release、日志输出 JSON |
| `LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error` |
| `HTTP_ADDR` | `:8080` | 监听地址 |
| `DB_HOST` | 空 | **留空即不启用数据库**；填了则必须同时提供 `DB_USER`、`DB_NAME` |
| `DB_AUTO_MIGRATE` | `false` | 建议仅开发环境开启 |
| `LUOGU_COOKIE_FILE` | `cookies.json` | 登录态文件，已在 `.gitignore` 中忽略 |

配置校验：设置了 `DB_HOST` 却缺少 `DB_USER`/`DB_NAME`、或 `DB_MAX_IDLE_CONNS > DB_MAX_OPEN_CONNS`
都会在启动时直接报错（fail fast）。

## 本地运行

```bash
# 无数据库模式（health 中 db 显示 disabled）
go run ./cmd/api

# 有 MySQL：先建库
mysql -uroot -e "CREATE DATABASE IF NOT EXISTS luogu2api DEFAULT CHARSET utf8mb4;"

$env:DB_HOST="127.0.0.1"; $env:DB_USER="root"; $env:DB_PASSWORD=""; $env:DB_NAME="luogu2api"; $env:DB_AUTO_MIGRATE="true"
go run ./cmd/api
```

## 健康检查

```bash
curl -s http://127.0.0.1:8080/healthz
```

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "status": "ok",
    "db": { "status": "ok" },
    "luogu": { "status": "anonymous", "cookieFile": "cookies.json" }
  }
}
```

| 字段 | 取值 | 含义 |
|---|---|---|
| `status` | `ok` / `degraded` | `degraded` 时 HTTP 返回 **503**，便于负载均衡摘除实例 |
| `db.status` | `ok` / `error` / `disabled` | `disabled` = 未配置 MySQL（本地开发），不算故障 |
| `luogu.status` | `authenticated` / `anonymous` / `unavailable` | 只读本地 cookie 状态，**不做网络请求**（避免健康检查打爆洛谷） |

需要在业务里确认洛谷登录态时，用 `client.Luogu.VerifyLogin()`（会发一次网络请求）。

## 校验

```powershell
pwsh scripts/check.ps1
# Windows PowerShell 5.1 亦可（脚本带 UTF-8 BOM）：
powershell -ExecutionPolicy Bypass -File scripts/check.ps1
```

等价手动命令：

```bash
go build ./... && go vet ./... && go test ./...
(cd pkg/luoguClient && go test -race ./...)     # 嵌套 module，./... 覆盖不到
```

## 部署注意

- **submodule**：`git clone --recursive`（CI 里 `submodules: true`），否则 `pkg/luoguClient` 为空、构建失败；
  更新 SDK：`git submodule update --remote pkg/luoguClient` 后提交新的指针。
- `pkg/luoguClient` 是独立的嵌套 module，`go build ./...` **不会**构建/测试它，CI 需单独跑一遍。
- 想彻底摆脱 submodule：`go mod vendor` 后提交 `vendor/`（实测约 0.5 MB），构建改为 `go build -mod=vendor`。
- `GOWORK=off` 可确认构建不依赖任何本地 workspace 文件。
- `cookies.json` 含会话凭据，已在 `.gitignore` 中忽略，**不要提交**。
