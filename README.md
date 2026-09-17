# Luogu2Api

基于 [luoguClient](https://github.com/laoin114514/luoguClient) 的洛谷 Web 服务，带 **账号号池**。

技术栈：**Gin + GORM + MySQL**。号池把多个洛谷账号的 cookie 集中管理起来：业务请求轮询选号，
后台定时任务验证登录态、失效则自动重登（验证码走外挂 OCR 服务），重登不成功的账号自动下线。

## 目录结构

```
Luogu2Api/
├── cmd/api/main.go              # 入口：配置 → 日志 → MySQL → 号池预热 → 组装 → 启停
├── Pool-Dashboard/              # pnpm + Vue 管理台（构建后由 Gin 在 /dashboard/ 托管）
├── internal/
│   ├── client/                  # 唯一接触洛谷 SDK 的包
│   │   ├── session.go           #   SessionClient 窄接口 + SDK 适配
│   │   ├── pool.go              #   号池：选号、验证、重登、错误分类、状态落库
│   │   ├── captcha.go           #   外挂 OCR 服务的 CaptchaSolver 适配
│   │   ├── problem.go           #   业务适配方法（类型别名 + GetProblem/Search）
│   │   └── record.go            #   业务适配方法（类型别名 + ListRecords）
│   ├── config/config.go         # 环境变量配置与校验（fail fast）
│   ├── handler/                 # HTTP 层：health / problem / record / pool / account
│   ├── middleware/              # 请求 ID、slog 访问日志、管理令牌校验
│   ├── model/                   # GORM 实体（account、schema_migrations）+ 实体清单 + 哨兵错误
│   ├── repository/              # 唯一使用 GORM 的包（含凭据加解密）
│   ├── response/                # 统一响应体 {code,message,data} 与业务码
│   ├── router/route.go          # 路由表
│   ├── schema/                  # 库结构版本管理：模型↔库差异、安全变更自动执行、审计
│   ├── secret/cipher.go         # AES-GCM 加解密（密码/cookie 落库前加密）
│   └── service/                 # 业务层：health / pool 扫描器 / problem / record / account
├── configs/env.example          # 环境变量样例
├── Dockerfile                   # 三段构建：pnpm 构建管理台 → Go 静态编译 → alpine 运行时
├── docker-compose.yml           # api + MySQL 一键起栈（配置直接用 configs/.env，见「容器化部署」）
├── .dockerignore                # 挡掉 node_modules、构建产物与所有 .env
├── scripts/check.ps1            # 一次跑通两个 module 的 build/vet/test
├── scripts/ocr_stub.py          # 本地联调用的假 OCR 服务
└── pkg/luoguClient/             # 洛谷 SDK —— git submodule（指向独立仓库）
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
| `model` | 表结构、状态常量、跨层哨兵错误 | 只放结构体与表名；`model.All()` 是库结构的唯一来源 |
| `schema` | 库结构校验与迁移 | 结构只由模型推导，不写迁移 SQL；破坏性变更只报告不执行 |
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

### "代码公开计划"：状态同步总是做，加入是显式 opt-in

账号在洛谷"代码公开计划"里的状态就是偏好设置里的 `openSource`（1 = 已加入）。号池在
**手上有活会话**的两个时机处理它：`completeLogin` 登录成功之后、每轮验证成功
（`handleAccount` 的 `ok` 分支）之后。`ACCOUNT_JOIN_OPEN_SOURCE` 只决定"要不要写"：

| 远端 `openSource` | `ACCOUNT_JOIN_OPEN_SOURCE` | 号池的动作 |
|---|---|---|
| 1（已加入） | 任意 | 只读同步：把 `open_source_joined` / `_at` 补成洛谷记录的值 |
| 0 / -1 | `true` | 读-改-写整份偏好把 `openSource` 置 1，再读一次确认后落库 |
| 0 / -1 | `false`（默认） | 什么都不写，只确认"远端确实未加入" |

读与写必须分开：**读偏好是幂等的只读请求**（`User.GetPreference`），也是本地这两列准确的
前提——账号可能在导入前就人工加入过，或导入后在网页上加入，默认配置下也应当如实记录；
**写偏好（加入）是不可逆动作**（洛谷限制 30 天内不能退出），只能由显式开关触发。
（早先的实现把读一起关在开关后面，于是默认配置下 `accounts.open_source_joined` 永远是 false。）

刻意**不放在导入的关键路径上**：那会让一个与号池功能无关的写接口成为导入的前置条件
（洛谷 429/维护 → 无法导入任何账号 → 补员中断），而且"导入即失败不落库"会绕过
下面那套失败分类，把一次网络抖动判成账号不可用。导入账号会立即尝试首次登录，成功就走上面
这条路径（开关开启时一并完成加入）；首次登录失败则交给扫描器后续重试。

三条硬约束：

1. **幂等**：`accounts.open_source_joined=true` 时连读都不发，一次请求都不产生；
   远端已经是 1 时只读一次偏好、不写。这一列成功一次就永久跳过。
2. **失败绝不改账号命运**：不碰 `online`/`status`/`failure_count`/`next_verify_at`，
   也不占用 `last_error`，只打一条 WARN 并保持原状，交给下一轮验证周期
   （`ACCOUNT_VERIFY_INTERVAL` + 抖动）自然重试。读失败按"状态未知"处理：不落库，
   更不会去写偏好。
3. **不在数据库事务里做远程调用**：远端成功才落库；落库失败只是下轮再确认一次
   （远端已是 1，那次只读不写）。

写成"读-改-写"而不是只发 `{"openSource":1}`：偏好更新是**全量替换**语义，省略字段会被
服务端重置成平台默认值（`codeSharingWithAi=true`、`learningMode=false`），只发一个字段会
顺手改掉账号的其它偏好。该逻辑在 SDK 的 `UserService.JoinOpenSourcePlan`，有 httptest 覆盖。

⚠️ **加入后 30 天内不能退出**（把 `openSource` 改回 `0`/`-1` 返回 HTTP 400），且账号代码会公开。
所以这是显式 opt-in，默认不做；`open_source_joined_at` 记的是洛谷返回的加入时间，
即"何时可以退出"的基准。运维可在账号 DTO 的 `openSourceJoined` / `openSourceJoinedAt` 看到进度。

### 错误分类决定账号命运（核心设计）

只有**确认 cookie 失效或账号被拒**才会把账号标记为不可用：

| 情况 | 判定 | 处置 |
|---|---|---|
| `401`（cookie 失效/过期、302→登录页） | cookie 确实失效 | 立即摘出号池并重登 |
| `403`（凭据没问题但被拒绝） | 账号被洛谷**封禁/限制** | 见下方"封禁识别"，判为 `banned` |
| 重登时验证码识别错（`CaptchaNotMatchException`） | 换一张还能试 | 换新验证码重试，计入尝试次数 |
| 网络错误 / 5xx / SDK 解析失败 | **与 cookie 无关** | **绝不改动 online/status**，只记 `last_error` 并短退避 |
| OCR 服务不可用 | 环境问题，不是账号问题 | 保持 `relogin_pending` + 短退避，**不**升级为失败 |
| 密码错误 / 账号锁定 / 需要二次验证 | 重试没意义 | 立即 `disabled`，等人工处理 |

一次重登任务最多尝试 `ACCOUNT_LOGIN_MAX_ATTEMPTS` 次（默认 5，每次换新验证码）；用尽仍失败则
`online=false` + `status=relogin_failed`，并按 `ACCOUNT_LOGIN_BACKOFF × 2^(n-1)` 退避（上限
`ACCOUNT_FAILED_RETRY`）慢速重试，而不是每 5 分钟热循环。

### 封禁识别（401 vs 403）

实测（2026-09，账号 `laoyin`）：**被封禁的账号照样能用密码登录成功**，登录响应 `locked:false`、
会话 cookie 也正常签发，但随后**所有**需要登录的接口都返回 `HTTP 403` —— `/user/setting` 403，
连它自己的公开主页 `/user/{uid}` 也 403，响应体是洛谷正常的应用页（不是 WAF 拦截页）。
而"cookie 失效"返回的是 `401`。因此判定靠状态码，且必须做**两步确认**，不能凭一次 403 就下结论：

1. 验证返回 `403` → 先按"登录态不可用"摘池，然后正常走一次重登（不改判定）；
2. 重登**登录成功**后再复核一次登录态：
   - 仍然 `403` → 凭据刚被接受却依然被拒 ⇒ **`banned`**（洛谷封禁/限制），退出自动重试；
   - `200` → 恢复 `active`（说明刚才的 403 是瞬时的，自愈）；
   - 网络类错误 → 按 `active` 处理并下一轮再验（登录已经成功，不因复核失败丢弃它）。

另外登录后拉到的用户资料里 `isBanned=true` 也会直接判为 `banned`（保留的兜底信号；封禁账号
通常连资料页都 403，取不到这个字段）。`banned` 的账号：`online=false`、不排下次验证、
不计失败次数、不参与选号，也不会被扫描器重试（否则会陷入"403→重登成功→403"的死循环）。

状态机：`new → active ⇄ relogin_pending →(尝试用尽) relogin_failed`，`disabled` 与 `banned`
都不会自动重试。人工恢复用 `PATCH /api/v1/admin/accounts/:id {"enabled":true}`：对 `disabled`
与 `banned` 账号会把状态复位成 `new`、清空失败计数与退避，让扫描器重新验证/登录一次（密码改对了、
或者封禁解除了，都用这个口子；仍被封禁则会在一轮内重新判回 `banned`）；对本来就正常的账号只做
启停，不会打断它的在线状态。

### accounts 表

登录凭据、平台档案、号池运行状态放同一张表（本项目规模下拆表的 join 成本大于收益）：

- 凭据：`username`、`password_enc`、`cookie_enc`（**AES-GCM 密文**，密钥来自 `ACCOUNT_SECRET_KEY`）
- 运行状态：`online`、`status`（`new`/`active`/`relogin_pending`/`relogin_failed`/`disabled`/`banned`）、`failure_count`、`last_error`、`next_verify_at`、`last_login_at`、`last_verified_at`、`cookie_updated_at`、`enabled`、`weight`
- 洛谷平台用户字段：`luogu_uid`（未登录为 NULL，避免唯一索引冲突）、`nickname`、`name`、`avatar`、`slogan`、`badge`、`color`、`is_admin`、`is_banned`、`ccf_level`、`xcpc_level`、`background`、`profile_json`（原始 JSON 快照，便于以后扩展字段）
- 代码公开计划（可选能力）：`open_source_joined`（是否已确认加入）、`open_source_joined_at`（洛谷返回的加入时间 = 30 天锁定期的解锁基准）

写入一律走列级 `Updates(map)`：扫描器与请求路径会并发改同一行，整行 `Save` 会丢更新。
读出的明文只存在于 `model.Account` 的 `gorm:"-"` 字段上，**任何 HTTP 响应都不含凭据**。

两个容易踩的坑（都有集成测试兜着）：

- `enabled` / `weight` 在模型上**不能写 `default`**：GORM 的 INSERT 会跳过"带 default 的零值字段"，
  一旦写成 `default:true`，`enabled=false` 的账号根本建不出来（会被数据库默认值悄悄覆盖）。
  `open_source_joined` 同理（它的零值 `false` 正是新账号要的初始值）。
- 查询里带 `OR` 时必须自己加括号，例如 `(next_verify_at IS NULL OR next_verify_at <= ?)`：
  GORM 拼接多个 `Where` 不会自动加括号，否则会退化成
  `(enabled AND status IN (...) AND next_verify_at IS NULL) OR next_verify_at <= ?`，
  后半段不带任何过滤，会把未到期甚至已停用/封禁的账号一起捞出来。

## 数据库结构与迁移

**结构的唯一来源是 `model` 包里的 GORM 模型：项目里没有任何手写的迁移 SQL 文件，也没有需要
手工维护的版本号。** 每次启动（以及 `-migrate` / `-schema-status`）都做同一件事：由模型 +
当前 dialector 推导出"期望形态"，从 `information_schema` 读回"实际形态"，两者相减。

差异分两类，处置方式刻意不同：

| 差异 | 例子 | 处置 |
|---|---|---|
| **安全**（不会丢数据） | 库不存在、表不存在、缺列、缺索引 | 开了 `DB_MIGRATE_ON_START` 或执行 `api -migrate` 时自动执行 |
| **需人工确认** | 库里多出来的列/索引、列的类型/可空性/默认值/自增不一致 | 只报告 + 给出可直接复制的 `ALTER TABLE`，**永不自动执行** |

后者不自动执行的原因：可能是收窄（`varchar(64)` → `varchar(32)` 会截断数据）、可能是别人
手工加的列、也可能是早期版本留下的痕迹。默认 `DB_SCHEMA_STRICT=true` 会让服务在存在这类
差异时**拒绝启动**——把问题挡在启动阶段（和配置 fail-fast 一个道理），而不是留到某个业务
请求上变成 `Unknown column`；确认可以忽略时设 `DB_SCHEMA_STRICT=false`。

**数据库本身也归"安全变更"管**：DSN 直接指向 `DB_NAME`，库不存在时 MySQL 在建立连接
阶段就报 `Error 1049 (42000): Unknown database`，`internal/schema` 那套"表/列/索引"的比对
根本没机会跑。因此允许改结构的两个入口会先执行 `CREATE DATABASE IF NOT EXISTS ... DEFAULT
CHARACTER SET utf8mb4`（排序规则交给服务端默认，避免在 5.7/8.0/9.x 之间挑错 collation；
连接账号需要 `CREATE` 权限）。只读的 `-schema-status` 不建库；不需要自动建库时，手工执行
「本地运行」里的 `CREATE DATABASE` 即可——库名会被拼进这条 DDL，所以 `DB_NAME` 只允许
字母、数字、下划线、`$` 与 `-`（配置校验会挡住其它字符）。

三个入口：

```bash
go run ./cmd/api                 # 启动服务：建库 + 校验 +（可选）自动补齐；结构落后就报错退出
go run ./cmd/api -migrate        # 只做结构变更（建库/建表/加列/建索引）后退出（部署流程用；仍有需人工确认的差异则退出码非 0）
go run ./cmd/api -schema-status  # 只打印差异（只读：不建库、不改结构、不写审计），不一致时退出码非 0
```

因为是"模型 vs 库现状"的现场比对，**手工执行的 DDL 不需要在代码里登记**——改完自然一致。
每次真正执行了变更、或发现了需人工处理的差异，就往 `schema_migrations` 追加一行审计
（模型指纹 + 库指纹 + 本次摘要），用来回溯"这个库经历过什么"；只读检查不写任何东西。
变更期间用 MySQL 命名锁（`GET_LOCK`）串行化，多实例同时启动不会两边同时 ALTER。

与 GORM `AutoMigrate` 的区别（也就是它被换掉的原因）：AutoMigrate 会**静默地**比对并
`ALTER`（作者踩过一次：手工 DDL 带了 `DEFAULT`，于是每次启动都想再 ALTER 一次），不删列、
不记录、也不告诉你它改了什么；`internal/schema` 只做安全子集，其余停下来要人确认，并且留痕。

## 环境变量

**配置只来自环境变量**（`config.Load()` 是纯函数，只读 `os.Getenv`）。二进制刻意
**不读** `.env`：部署侧（compose / k8s / 服务管理器）本来就负责注入环境变量，而在代码里
读 `.env` 会让"配置来源"变成依赖工作目录的隐藏输入——镜像里、挂载卷里残留一个 `.env`
就能把忘记配置的必填项**悄悄补上**，正好破坏这个项目的 fail-fast（本该启动即报错，
变成带着开发库的连接跑起来）。

两种场景各自的入口：

| 场景 | 怎么给配置 |
|---|---|
| 本地开发 | `pwsh scripts/dev.ps1`：把 `configs/.env` 导出到当前会话再 `go run`（见"本地运行"） |
| 部署 | compose 的 `env_file: configs/.env`（整份注入容器，见「容器化部署」），或 k8s Secret、服务管理器的环境变量 |

`configs/env.example` 是模板；`configs/.env` 已在 `.gitignore` 里，**不要**打进镜像
（仓库里的 `.dockerignore` 已经挡掉 `.env` / `configs/.env`；否则上面那条 fail-fast 的保证就失效了）。

必需项：

| 变量 | 说明 |
|---|---|
| `DB_HOST` / `DB_USER` / `DB_NAME` | 号池依赖 MySQL；缺任一项启动即报错。`DB_NAME` 只允许字母、数字、下划线、`$` 与 `-` |
| `ACCOUNT_SECRET_KEY` | 凭据加密密钥，`openssl rand -base64 32`（也接受 hex 的 16/24/32 字节） |
| `LUOGU_OCR_URL` | 验证码识别服务地址（SDK 不内置 OCR） |
| `LUOGU_OCR_MODE` | 入参形态：`base64`（默认，JSON `{"image_base64":"..."}`）/ `raw`（原始 JPEG 字节） |

常用项：

| 变量 | 默认值 | 说明 |
|---|---|---|
| `APP_ENV` | `dev` | `prod` 时 gin 走 release、日志输出 JSON |
| `HTTP_ADDR` | `:8080` | 监听地址 |
| `DB_MIGRATE_ON_START` | `false` | 启动时自动建库并补齐库结构（建表/加列/建索引）；关闭时只校验，落后即启动失败 |
| `DB_SCHEMA_STRICT` | `true` | 发现需人工确认的结构差异（删列/改类型）时拒绝启动 |
| `LUOGU_TIMEOUT` / `LUOGU_RETRY` | `30s` / `1` | SDK 超时与内部重试次数 |
| `ACCOUNT_SWEEP_INTERVAL` | `5m` | 扫描器 tick |
| `ACCOUNT_VERIFY_INTERVAL` | `30m` | 单账号验证间隔（带抖动） |
| `ACCOUNT_LOGIN_MAX_ATTEMPTS` | `5` | 单次重登任务的尝试次数 |
| `ACCOUNT_REQUEST_MAX_TRY` | `3` | 业务请求最多换几个账号 |
| `ACCOUNT_JOIN_OPEN_SOURCE` | `false` | 让池内账号加入洛谷"代码公开计划"（**不可逆 30 天**，见上节）；关闭时仍会只读同步远端状态 |
| `ADMIN_TOKEN` | 空 | 为空则**不注册**管理路由（fail closed） |

配置校验：缺少必填项、`DB_MAX_IDLE_CONNS > DB_MAX_OPEN_CONNS`、`ACCOUNT_VERIFY_INTERVAL <
ACCOUNT_SWEEP_INTERVAL`、抖动越界、密钥长度/编码非法、OCR 地址缺协议头，都会在启动时直接报错。

## 接口

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/healthz` | 健康检查。号池没有在线账号或数据库不可用 → **503**，便于探活摘实例 |
| GET | `/api/v1/pool/status` | 号池快照（在线/待重登/失败/停用/封禁数、最近一轮扫描统计） |
| GET | `/api/v1/problems/:pid` | 题目详情（走号池选号 + 失效换号重试） |
| GET | `/api/v1/problems?keyword=&page=&pageSize=` | 题目搜索 |
| GET | `/api/v1/users/:uid/records?pid=&status=&page=` | 指定洛谷用户的提交记录（走号池选号；`pid`/`status` 可选过滤） |
| GET | `/api/v1/admin/accounts` | 账号列表（**需 `X-Admin-Token`**） |
| POST | `/api/v1/admin/accounts` | 新增账号并尝试首次登录 |
| GET | `/api/v1/admin/accounts/:id` | 账号详情 |
| PATCH | `/api/v1/admin/accounts/:id` | `{"enabled":true/false}` 启停；启用 `disabled` 账号会复位状态交给扫描器重试 |
| PUT | `/api/v1/admin/accounts/:id/password` | `{"password":"new-password"}` 修改数据库中的密码密文；启用且非 `banned` 的账号会立即用新密码重登验证，重登失败不报错（状态在响应 DTO 里） |
| DELETE | `/api/v1/admin/accounts/:id` | 软删除并移出号池；**再次用同名 username 创建会自动复活原行**（主键不变，凭据/状态/档案全部重置，created_at 保留） |
| POST | `/api/v1/admin/accounts/:id/relogin` | 强制立即重登 |
| GET | `/dashboard/` | 号池管理台静态站点（需先构建 `Pool-Dashboard`） |

业务码：`0` 成功、`400` 参数错、`401` 令牌无效、`404` 不存在、`409` 冲突、`500` 内部错误、
`1001` 号池无可用账号（HTTP 503）、`1002` 洛谷登录态全部失效（HTTP 502）。

## 本地运行

```bash
# 1) 建库（可跳过：DB_MIGRATE_ON_START=true 或 api -migrate 会自动建库）
mysql -uroot -e "CREATE DATABASE IF NOT EXISTS luogu2api DEFAULT CHARSET utf8mb4;"

# 2) 起一个假 OCR（本地联调用，详见 scripts/ocr_stub.py 的说明）
python3 scripts/ocr_stub.py

# 3) 配置：复制样例后编辑 configs/.env（已在 .gitignore 里）
Copy-Item configs/env.example configs/.env
# 编辑 configs/.env：至少填 ACCOUNT_SECRET_KEY（openssl rand -base64 32）
# 与 LUOGU_OCR_URL；本地可用 DB_MIGRATE_ON_START=true 让它自动建库建表

# 4) 启动（把 .env 导出到当前会话，退出即失效；不会污染系统环境变量）
pwsh scripts/dev.ps1
# Windows PowerShell 5.1 亦可（脚本带 UTF-8 BOM）：
powershell -ExecutionPolicy Bypass -File scripts/dev.ps1
```

### Pool Dashboard

`Pool-Dashboard` 是一个独立的 pnpm + Vue + Element Plus 项目，构建产物不提交；Go 服务会将其同源托管在
`/dashboard/`。它使用既有管理 API 完成账号列表、导入、启停、改密、删除和强制重登：改密只提交
表单里的新密码，不会读取或显示数据库中已存储的密码密文、cookie 等凭据。首次进入会用受保护的账号
列表接口校验 `ADMIN_TOKEN`；令牌只保存在当前浏览器标签页的 `sessionStorage`，管理请求收到 `401`
会立刻清除令牌并回到登录页。

```powershell
# 首次或依赖变化后
pnpm --dir Pool-Dashboard install

# 产出 Pool-Dashboard/dist；运行中的 Go 服务会立即按 /dashboard/ 提供这些文件
pnpm --dir Pool-Dashboard build

# 开发时：Vite 自动把 /api 请求转发给本地 Go 服务（默认 127.0.0.1:8080）
pnpm --dir Pool-Dashboard dev
```

浏览器访问 `http://127.0.0.1:8080/dashboard/`，输入与后端 `ADMIN_TOKEN` 一致的令牌即可使用。
没有执行前端构建时，Go API 仍可正常启动，但 `/dashboard/` 没有可提供的页面文件。

#### Vue 组件定位（开发调试）

`http://127.0.0.1:8080/dashboard/` 是 Go 托管的 `pnpm build` 生产构建产物，Vue Devtools 不会
在这里提供组件检查或页面定位。需要调试组件时，保持 Go 服务运行后执行
`pnpm --dir Pool-Dashboard dev`，再按 Vite 终端输出的地址访问（通常是
`http://localhost:5173/dashboard/`）。Vite 会把 `/api` 同源代理到 `127.0.0.1:8080`；同时需要在
浏览器中安装并启用 Vue Devtools 扩展。不要为 Go 托管的管理台生产页强制开启 Devtools。

不用脚本也可以照旧导出环境变量（**已存在的非空变量优先于 `.env`**，脚本与 compose
的 `env_file` 都是这个优先级）：

```powershell
$env:DB_HOST="127.0.0.1"; $env:DB_USER="root"; $env:DB_PASSWORD=""; $env:DB_NAME="luogu2api"
$env:DB_MIGRATE_ON_START="true"
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
curl -s "http://127.0.0.1:8080/api/v1/users/1582049/records?page=1"
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

提交记录响应（分页字段由服务端算好，调用方不必自己数）：

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "uid": 1582049,
    "page": 1,
    "pageSize": 20,
    "totalPages": 9,
    "count": 178,
    "pageRecordCount": 20,
    "records": [
      {
        "id": 240247732,
        "status": 12,
        "score": 100,
        "time": 15,
        "memory": 1024,
        "sourceCodeLength": 42,
        "submitTime": 1750000000,
        "language": 14,
        "enableO2": true,
        "problem": { "pid": "P1001", "name": "A+B Problem", "difficulty": 1, "submitted": true, "accepted": true },
        "user": { "uid": 1582049, "name": "tester" }
      }
    ]
  }
}
```

`status` 是洛谷的评测状态码（`12` 通过、`14` 未通过/部分分、`2` 评测中；省略或 `0` 表示不按状态
过滤），`pid` 省略表示不按题目过滤。记录接口需要登录态：未登录时洛谷返回 401，号池会换号重试，
没有可用账号返回 503（业务码 `1001`）。

分页字段的口径（每页条数固定 20，见 `client.RecordListPageSize`）：

| 字段 | 含义 | 计算 |
|---|---|---|
| `count` | 符合条件的记录总数（不是本页条数） | 洛谷返回 |
| `pageSize` | 每页条数 | 固定 20 |
| `totalPages` | 总页数 | `ceil(count / pageSize)`，无记录时为 0 |
| `pageRecordCount` | 本页条数 | 末页可能不满；页码超出总页数时为 0 |

例如 `count=178`、`page=9` → `totalPages=9`、`pageRecordCount=18`（前 8 页各 20 条，
第 9 页是末页，只有 18 条）。

## 容器化部署（Docker / Compose）

一个镜像同时提供 API 与管理台（Gin 托管 `/dashboard/`），`docker-compose.yml` 再配一个 MySQL 8.4。

| 文件 | 作用 |
|---|---|
| `Dockerfile` | 三段构建：pnpm 构建 `Pool-Dashboard` → Go 静态编译 `cmd/api` → alpine 运行时（非 root，自带 HEALTHCHECK、ca-certificates、tzdata） |
| `docker-compose.yml` | `api` + `mysql`：配置直接读 `configs/.env`（env_file 整份注入），compose 只覆盖容器里必然不同的四项 |
| `.dockerignore` | 挡掉 `node_modules`、构建产物与**所有 `.env`**（配置只能由部署侧注入，这是 fail-fast 的前提） |

```bash
# 0) pkg/luoguClient 是 submodule——镜像里必须真有这份 SDK 源码，否则编译不过
git submodule update --init --recursive

# 1) 配置：容器读的就是本地开发那份 configs/.env（没有就先建一份）
cp configs/env.example configs/.env   # 至少填 DB_PASSWORD、ACCOUNT_SECRET_KEY，建议设 ADMIN_TOKEN

# 2) 起栈（首次或改了 Dockerfile/前端后加 --build）
docker compose up -d --build

# 3) 管理台 http://127.0.0.1:8080/dashboard/（ADMIN_TOKEN 留空 = 管理路由不注册）
#    探活   http://127.0.0.1:8080/healthz  （号池没有在线账号时按设计返回 503）
```

配置规则只有两条：

1. **`configs/.env` 是唯一来源**：`env_file` 把整份文件注入两个容器，所以里面加/改变量不用动
   `docker-compose.yml`；必填项缺失仍由二进制自己 fail-fast，报错信息与本地开发一致。
2. **compose 只覆盖容器里必然不同的四项**：`DB_HOST=mysql`、`DB_PORT=3306`、`HTTP_ADDR=:8080`、
   `TZ`。`.env` 里那份 `DB_HOST=127.0.0.1` 是给本地开发用的，不会带进容器。

MySQL 的 root 密码取的也是 `.env` 里的 `DB_PASSWORD`，但 compose 的 `${}` 插值**不读** `env_file`，
所以 mysql 服务在入口处做了一次改名（`export MYSQL_ROOT_PASSWORD="$DB_PASSWORD"` 后再交给官方
entrypoint，见 `docker-compose.yml` 里的注释）。不喜欢这层壳的话，那里写了另外两条等价路线
——要么在 `.env` 里再写一行同名密码，要么改成插值并给每条命令加上 `--env-file configs/.env`。

常用命令：

```bash
docker compose logs -f api                  # 看日志（APP_ENV=prod 时是 JSON）
docker compose ps                           # 状态与健康检查结果
docker compose run --rm api -migrate        # 只做结构变更（建库/建表/加列/建索引）
docker compose run --rm api -schema-status  # 只打印库结构与模型的差异（只读）
docker compose exec mysql mysql -uroot -p   # 进库排查（密码 = .env 里的 DB_PASSWORD）
docker compose down                         # 停栈；加 -v 连数据卷一起删（账号数据会没）
```

容器相关的几点差异：

- **结构迁移跟着 `configs/.env` 走**（`DB_MIGRATE_ON_START`，env.example 里是 `true`）：库不存在时
  自动建库建表，第一把就能起来。想改成"部署步骤显式迁移"就设成 `false`，改用上面的 `run --rm api -migrate`。
- **时区**：DSN 默认 `loc=Local`，镜像装了 `tzdata`；compose 把两个容器的 `TZ` 都固定成
  `Asia/Shanghai`（要改就同时改 mysql 与 api 两处——两边时区不一致，时间列会偏移）。
- **端口**：容器内固定监听 `:8080`（`environment:` 覆盖 `.env` 里的本地值），宿主端口直接改
  `docker-compose.yml` 里的 `8080:8080` 那一行；`mysql` 默认不把 3306 暴露到宿主机（需要本机客户端
  连进去时，把那两行 `ports` 的注释去掉）。
- **探活**：镜像的 `HEALTHCHECK` 打的是恒定 200 的 `/api/v1/pool/status`。`/healthz` 在号池没有
  在线账号时会返回 503（设计如此），首次部署还没导账号时会把容器判成 unhealthy，不适合当探针。
- **多架构**：`docker buildx build --platform linux/amd64,linux/arm64 .` 可直接用（Go 段交叉编译，
  Node 段跑在构建机上）；需要 BuildKit（Docker 23+ 默认开启）。
- **单实例**：扫描器与号池都在进程内，多副本会互相踢会话，本栈刻意只起一个 api；其余约束见「部署注意」。

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

结构检查与仓储层的集成测试需要真实 MySQL（未设置 `TEST_DB_DSN` 则自动跳过）：

```powershell
$env:TEST_DB_DSN="root:@tcp(127.0.0.1:3306)/luogu2api_test?charset=utf8mb4&parseTime=True&loc=Local"
go test ./internal/schema/ ./internal/repository/ -v
```

`schema` 的集成测试会真的建表、加列，再故意制造『有人手工改过库』的差异，断言只报告不执行。
类型归一化（`bool` ↔ `tinyint(1)`、整数显示宽度、可空时间列、COMMENT）写错的话，那里会立刻红。

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
- **代码公开计划是不可逆的**：开启 `ACCOUNT_JOIN_OPEN_SOURCE` 后账号会陆续加入洛谷"代码公开计划"，
  加入后 **30 天内不能退出**（洛谷直接拒绝把 `openSource` 改回 `0`/`-1`），期间账号代码公开。
  开启前请确认这是你要的；关掉开关只停止"继续加入"，**不会**把已加入的账号退出来。
  开关与状态同步无关：账号是否已加入始终会如实记在 `accounts.open_source_joined` / `_at` 上。
- **SDK 几乎没有写入能力**：`luoguClient` 的写接口只有账号偏好设置（`UpdatePreference` /
  `JoinOpenSourcePlan`，见 SDK 的 `api.md`），题目/记录/题单/讨论/比赛全是只读。
  号池只用 `GetPreference` 做只读状态同步，且只在 `ACCOUNT_JOIN_OPEN_SOURCE=true` 时用
  `JoinOpenSourcePlan` 补做"加入代码公开计划"（幂等、失败不影响可用性），
  没有对外暴露写接口——要暴露得先决定给谁用、怎么鉴权。
- **改模型即改结构**：加表/加列/加索引由 `api -migrate`（或其中一个实例开
  `DB_MIGRATE_ON_START=true`）自动执行，**不需要写迁移 SQL**；删列、改类型这类有丢数据
  风险的变更不会被自动执行——服务会打印可直接复制的 SQL 并拒绝启动（`DB_SCHEMA_STRICT`），
  人工确认后执行即可。审计记录在 `schema_migrations`。
- **部署顺序**：先跑 `api -migrate`（或让一个实例开自动迁移），再滚动升级应用；老版本先跑
  也安全——加列对旧代码是透明的。
- **已知限制**：SDK 的 `WithContext` 只在构造期生效，业务请求无法按调用方 ctx 取消，只能用
  `LUOGU_TIMEOUT` + `http.Server` 超时兜底。
- **submodule**：克隆用 `git clone --recurse-submodules`（已有工作区执行 `git submodule update --init`）；
  更新 SDK：`git -C pkg/luoguClient pull`；改 SDK：在子模块内 commit/push，再回主仓库提交指针。
- `cookies.json` 相关的旧单文件登录态已随号池移除；凭据只存在数据库（密文）。
