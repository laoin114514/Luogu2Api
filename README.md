# Luogu2Api

基于 [luoguClient](https://github.com/laoin114514/luoguClient) 的洛谷 Web 服务，带 **账号号池**。

技术栈：**Gin + GORM + MySQL**。号池把多个洛谷账号的 cookie 集中管理起来：业务请求轮询选号，
后台定时任务验证登录态、失效则自动重登（验证码走外挂 OCR 服务），重登不成功的账号自动下线。

## 目录结构

```
Luogu2Api/
├── cmd/api/main.go              # 入口：配置 → 日志 → MySQL → 号池预热 → 组装 → 启停
├── internal/
│   ├── client/                  # 唯一接触洛谷 SDK 的包
│   │   ├── session.go           #   SessionClient 窄接口 + SDK 适配
│   │   ├── pool.go              #   号池：选号、验证、重登、错误分类、状态落库
│   │   ├── captcha.go           #   外挂 OCR 服务的 CaptchaSolver 适配
│   │   └── problem.go           #   业务适配方法（类型别名 + GetProblem/Search）
│   ├── config/config.go         # 环境变量配置与校验（fail fast）
│   ├── handler/                 # HTTP 层：health / problem / pool / account
│   ├── middleware/              # 请求 ID、slog 访问日志、管理令牌校验
│   ├── model/                   # GORM 实体（problem、account）+ 迁移清单 + 哨兵错误
│   ├── repository/              # 唯一使用 GORM 的包（含凭据加解密）
│   ├── response/                # 统一响应体 {code,message,data} 与业务码
│   ├── router/route.go          # 路由表
│   ├── secret/cipher.go         # AES-GCM 加解密（密码/cookie 落库前加密）
│   └── service/                 # 业务层：health / pool 扫描器 / problem / account
├── configs/env.example          # 环境变量样例
├── scripts/check.ps1            # 一次跑通两个 module 的 build/vet/test
├── scripts/ocr_stub.py          # 本地联调用的假 OCR 服务
└── pkg/luoguClient/             # 洛谷 SDK —— git submodule（独立仓库、独立发版）
```

## 分层与依赖方向

```
cmd/api ──► router ──► handler ──► service ──► repository (GORM/MySQL)
                                      └──────► client     (洛谷 SDK + 号池)
                   middleware / response（横切）
                   config / model / secret（被各层依赖，不依赖上层）
```

| 层 | 职责 | 约束 |
|---|---|---|
| `handler` | 参数解析、调用 service、渲染响应、错误→HTTP 映射 | 不写业务逻辑，不出现 GORM/SQL |
| `service` | 业务编排、上游错误翻译成业务语义 | 只依赖自己声明的窄接口，不依赖 gin/GORM/SDK |
| `repository` | 数据访问 | **唯一**直接使用 GORM/MySQL 的包 |
| `client` | 外部服务访问 + 号池 | **唯一**直接使用洛谷 SDK 的包 |
| `model` | 表结构、状态常量、跨层哨兵错误 | 只放结构体与表名；`model.All()` 是迁移唯一来源 |
| `secret` | 凭据加解密 | 无业务依赖，可独立测试 |

## 号池工作原理

### 一号一 client（重要）

`client.Pool` 里**每个账号持有一个长期存活的 `sdk.Client`**，绝不在请求路径上给共享 client
反复 `ImportCookies`——SDK 的 cookie jar 是 client 级可变状态，并发注入不同账号会让在途请求
"变成"另一个账号。同一账号内的并发是安全的（身份不变）。

### 选号与容错

业务请求 `Pick` 按 **轮询** 从"启用 + 在线 + active"的账号中选一个；命中 `401/403` 时立刻把该账号
摘出号池，**换下一个账号重试**（最多 `ACCOUNT_REQUEST_MAX_TRY` 个），只有池子空了才返回 503。
摘出的账号会在后台异步重登恢复，请求本身不需要等它。

### 定时扫描

`ACCOUNT_SWEEP_INTERVAL`（默认 5 分钟）触发一轮扫描，但**每轮只处理到期的账号**：
`next_verify_at` 为空或已过期。正常账号的验证间隔是 `ACCOUNT_VERIFY_INTERVAL`（默认 30 分钟）
并带 `±ACCOUNT_VERIFY_JITTER` 抖动，避免整池在同一时刻一起打洛谷。扫描有重入保护，
上一轮没跑完时下一个 tick 直接跳过。

### 错误分类决定账号命运（核心设计）

只有**确认 cookie 失效**才会把账号标记为不可用：

| 情况 | 判定 | 处置 |
|---|---|---|
| `UnauthorizedError`（401/403/302→login） | cookie 确实失效 | 立即摘出号池并重登 |
| 重登时验证码识别错（`CaptchaNotMatchException`） | 换一张还能试 | 换新验证码重试，计入尝试次数 |
| 网络错误 / 5xx / SDK 解析失败 | **与 cookie 无关** | **绝不改动 online/status**，只记 `last_error` 并短退避 |
| OCR 服务不可用 | 环境问题，不是账号问题 | 保持 `relogin_pending` + 短退避，**不**升级为失败 |
| 密码错误 / 账号锁定 / 需要二次验证 | 重试没意义 | 立即 `disabled`，等人工处理 |

一次重登任务最多尝试 `ACCOUNT_LOGIN_MAX_ATTEMPTS` 次（默认 5，每次换新验证码）；用尽仍失败则
`online=false` + `status=relogin_failed`，并按 `ACCOUNT_LOGIN_BACKOFF × 2^(n-1)` 退避（上限
`ACCOUNT_FAILED_RETRY`）慢速重试，而不是每 5 分钟热循环。

状态机：`new → active ⇄ relogin_pending →(尝试用尽) relogin_failed`，`disabled` 不会自动重试。
人工恢复用 `PATCH /api/v1/admin/accounts/:id {"enabled":true}`：对 `disabled` 账号会把状态复位成
`new`、清空失败计数与退避，让扫描器重新验证/登录一次（否则修好密码后账号也永远回不了池子）；
对本来就正常的账号则只做启停，不会打断它的在线状态。

### accounts 表

登录凭据、平台档案、号池运行状态放同一张表（本项目规模下拆表的 join 成本大于收益）：

- 凭据：`username`、`password_enc`、`cookie_enc`（**AES-GCM 密文**，密钥来自 `ACCOUNT_SECRET_KEY`）
- 运行状态：`online`、`status`、`failure_count`、`last_error`、`next_verify_at`、`last_login_at`、`last_verified_at`、`cookie_updated_at`、`enabled`、`weight`
- 洛谷平台用户字段：`luogu_uid`（未登录为 NULL，避免唯一索引冲突）、`nickname`、`name`、`avatar`、`slogan`、`badge`、`color`、`is_admin`、`is_banned`、`ccf_level`、`xcpc_level`、`background`、`profile_json`（原始 JSON 快照，便于以后扩展字段）

写入一律走列级 `Updates(map)`：扫描器与请求路径会并发改同一行，整行 `Save` 会丢更新。
读出的明文只存在于 `model.Account` 的 `gorm:"-"` 字段上，**任何 HTTP 响应都不含凭据**。

## 环境变量

见 `configs/env.example`。必填项：

| 变量 | 说明 |
|---|---|
| `DB_HOST` / `DB_USER` / `DB_NAME` | 号池依赖 MySQL；缺任一项启动即报错 |
| `ACCOUNT_SECRET_KEY` | 凭据加密密钥，`openssl rand -base64 32`（也接受 hex 的 16/24/32 字节） |
| `LUOGU_OCR_URL` | 验证码识别服务地址（SDK 不内置 OCR） |
| `LUOGU_OCR_MODE` | 入参形态：`base64`（默认，JSON `{"image_base64":"..."}`）/ `raw`（原始 JPEG 字节） |

常用项：

| 变量 | 默认值 | 说明 |
|---|---|---|
| `APP_ENV` | `dev` | `prod` 时 gin 走 release、日志输出 JSON |
| `HTTP_ADDR` | `:8080` | 监听地址 |
| `DB_AUTO_MIGRATE` | `false` | 建表用，建议仅开发环境开启 |
| `LUOGU_TIMEOUT` / `LUOGU_RETRY` | `30s` / `1` | SDK 超时与内部重试次数 |
| `ACCOUNT_SWEEP_INTERVAL` | `5m` | 扫描器 tick |
| `ACCOUNT_VERIFY_INTERVAL` | `30m` | 单账号验证间隔（带抖动） |
| `ACCOUNT_LOGIN_MAX_ATTEMPTS` | `5` | 单次重登任务的尝试次数 |
| `ACCOUNT_REQUEST_MAX_TRY` | `3` | 业务请求最多换几个账号 |
| `ADMIN_TOKEN` | 空 | 为空则**不注册**管理路由（fail closed） |

配置校验：缺少必填项、`DB_MAX_IDLE_CONNS > DB_MAX_OPEN_CONNS`、`ACCOUNT_VERIFY_INTERVAL <
ACCOUNT_SWEEP_INTERVAL`、抖动越界、密钥长度/编码非法、OCR 地址缺协议头，都会在启动时直接报错。

## 接口

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/healthz` | 健康检查。号池没有在线账号或数据库不可用 → **503**，便于探活摘实例 |
| GET | `/api/v1/pool/status` | 号池快照（在线/待重登/失败/停用数、最近一轮扫描统计） |
| GET | `/api/v1/problems/:pid` | 题目详情（走号池选号 + 失效换号重试） |
| GET | `/api/v1/problems?keyword=&page=&pageSize=` | 题目搜索 |
| GET | `/api/v1/admin/accounts` | 账号列表（**需 `X-Admin-Token`**） |
| POST | `/api/v1/admin/accounts` | 新增账号并尝试首次登录 |
| GET | `/api/v1/admin/accounts/:id` | 账号详情 |
| PATCH | `/api/v1/admin/accounts/:id` | `{"enabled":true/false}` 启停；启用 `disabled` 账号会复位状态交给扫描器重试 |
| DELETE | `/api/v1/admin/accounts/:id` | 软删除并移出号池 |
| POST | `/api/v1/admin/accounts/:id/relogin` | 强制立即重登 |

业务码：`0` 成功、`400` 参数错、`401` 令牌无效、`404` 不存在、`409` 冲突、`500` 内部错误、
`1001` 号池无可用账号（HTTP 503）、`1002` 洛谷登录态全部失效（HTTP 502）。

## 本地运行

```bash
# 1) 建库
mysql -uroot -e "CREATE DATABASE IF NOT EXISTS luogu2api DEFAULT CHARSET utf8mb4;"

# 2) 起一个假 OCR（本地联调用，详见 scripts/ocr_stub.py 的说明）
python3 scripts/ocr_stub.py

# 3) 配置并启动
$env:DB_HOST="127.0.0.1"; $env:DB_USER="root"; $env:DB_PASSWORD=""; $env:DB_NAME="luogu2api"
$env:DB_AUTO_MIGRATE="true"
$env:ACCOUNT_SECRET_KEY=(openssl rand -base64 32)
$env:LUOGU_OCR_URL="http://127.0.0.1:9898/ocr"
$env:ADMIN_TOKEN="dev-token"
go run ./cmd/api
```

```bash
# 4) 导入账号（会立刻尝试登录；失败也不报错，交给扫描器重试）
curl -s -X POST http://127.0.0.1:8080/api/v1/admin/accounts \
  -H "X-Admin-Token: dev-token" -H "Content-Type: application/json" \
  -d '{"username":"your-luogu-user","password":"your-password","nickname":"小号1"}'

# 5) 观察号池与业务接口
curl -s http://127.0.0.1:8080/healthz
curl -s http://127.0.0.1:8080/api/v1/pool/status
curl -s http://127.0.0.1:8080/api/v1/problems/P1001
```

健康检查响应：

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "status": "ok",
    "db": { "status": "ok" },
    "luogu": {
      "status": "authenticated",
      "total": 3,
      "online": 2,
      "reloginPending": 1,
      "reloginFailed": 0,
      "disabled": 0,
      "lastSweepAt": "2026-03-01T12:00:00Z"
    }
  }
}
```

## 校验

```powershell
pwsh scripts/check.ps1
# Windows PowerShell 5.1 亦可（脚本带 UTF-8 BOM）：
powershell -ExecutionPolicy Bypass -File scripts/check.ps1
```

等价手动命令：

```bash
go build ./... && go vet ./... && go test -race ./...
(cd pkg/luoguClient && go test -race ./...)     # 嵌套 module，./... 覆盖不到
```

仓储层集成测试需要真实 MySQL（未设置则自动跳过）：

```powershell
$env:TEST_DB_DSN="root:@tcp(127.0.0.1:3306)/luogu2api_test?charset=utf8mb4&parseTime=True&loc=Local"
go test ./internal/repository/ -v
```

## 部署注意

- **单实例假设**：扫描器与号池都在进程内。多实例部署会同时重登同一账号、互相踢掉会话，
  需要引入 DB 租约/选主（`service.PoolService` 是预留的接入点）。
- **密钥轮换**：密文带 `v1:` 版本前缀，便于平滑换算法；换 `ACCOUNT_SECRET_KEY` 前必须先用
  旧密钥解密、再用新密钥重新加密（否则所有凭据都解不开，且启动时会明确报"解密失败"）。
- **风控**：同 IP 多账号、固定 UA、高频登录都是洛谷的风控特征。请保持
  `ACCOUNT_VERIFY_CONCURRENCY=1`、给 `ACCOUNT_VERIFY_INTERVAL` 留足抖动，不要为了"更快发现掉线"
  把验证间隔调到分钟级。
- **凭据不能进日志**：日志只记 `account_id/username/status/错误类型`。管理接口返回 DTO，
  永不含 `password`/`cookie`；`ADMIN_TOKEN` 必须设置，否则管理路由不注册（避免裸奔的号池管理入口）。
- **SDK 无提交能力**：当前 `luoguClient` 只支持读取（题目/记录/题单/讨论/比赛），号池只服务只读接口。
- **已知限制**：SDK 的 `WithContext` 只在构造期生效，业务请求无法按调用方 ctx 取消，只能用
  `LUOGU_TIMEOUT` + `http.Server` 超时兜底。
- **submodule**：`git clone --recursive`（CI 里 `submodules: true`），否则 `pkg/luoguClient` 为空、
  构建失败；更新 SDK：`git submodule update --remote pkg/luoguClient` 后提交新的指针。
- `cookies.json` 相关的旧单文件登录态已随号池移除；凭据只存在数据库（密文）。
