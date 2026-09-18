# luogu2api —— Go 客户端 SDK

[Luogu2Api](../README.md) 服务 HTTP API 的 Go 客户端。

**整个目录就是一个自包含的包**：只用标准库，不 import 本仓库的任何其它代码，
复制到任意工程里都能直接使用。

## 用起来

### 方式一：把目录拷进你的工程（推荐）

把 `luogu2api/` 整个目录复制到你的工程里 —— 放哪个子目录、改成什么目录名都行，
包名始终是 `luogu2api`，不用改一行代码：

```go
import luogu2api "your/module/third_party/luogu2api"

sdk, err := luogu2api.NewSDK("http://127.0.0.1:8080", os.Getenv("ADMIN_TOKEN"))
if err != nil {
    return err // 地址非法或令牌为空：构造期就失败（fail fast）
}
```

目录里的 `_test.go` 同样只依赖标准库，`go build ./... && go test ./...` 都能直接跑过。

### 方式二：作为主模块的包引入

本目录属于主模块，也可以直接按模块路径导入（`go get github.com/laoin114514/luogu2api/luogu2api`）：

```go
import luogu2api "github.com/laoin114514/luogu2api/luogu2api"
```

## 覆盖的接口

只封装对外业务接口与两个公开探活接口；管理接口（`/api/v1/admin/accounts…`）**刻意不封装**
—— 导入账号、改密、启停是运维动作，走管理台（`/dashboard/`）或 curl。SDK 只提供"查数据"
的能力，不碰号池里的账号凭据。

| SDK 调用 | HTTP |
|---|---|
| `sdk.Problem.Get(ctx, "P1001")` | `GET /api/v1/problems/:pid` |
| `sdk.Problem.Search(ctx, luogu2api.SearchParams{...})` | `GET /api/v1/problems?keyword=&page=&pageSize=` |
| `sdk.Record.ListByUser(ctx, uid, luogu2api.RecordListParams{...})` | `GET /api/v1/users/:uid/records?pid=&status=&page=` |
| `sdk.Pool.Status(ctx)` | `GET /api/v1/pool/status` |
| `sdk.Health.Check(ctx)` / `sdk.Health.Live(ctx)` | `GET /healthz` / `GET /livez`（公开，不需要令牌） |

除探活外的接口都需要 `ADMIN_TOKEN`；令牌在 `NewSDK` 时注入，之后每个请求都会带
`X-Admin-Token` 请求头。`Client` 没有可变状态，可并发使用。

## 示例

```go
ctx := context.Background()

problem, err := sdk.Problem.Get(ctx, "P1001")
fmt.Println(problem.PID, problem.Title, problem.Difficulty)

result, err := sdk.Problem.Search(ctx, luogu2api.SearchParams{Keyword: "A+B", Page: 1, PageSize: 20})
fmt.Println(result.Total, result.Problems)

page, err := sdk.Record.ListByUser(ctx, 1582049, luogu2api.RecordListParams{
    PID:    "P1001",
    Status: luogu2api.RecordStatusAccepted,
    Page:   1,
})
fmt.Println(page.Count, page.TotalPages, page.Records)

status, err := sdk.Pool.Status(ctx)
fmt.Println(status.Online, status.Total)

// 号池没有在线账号时服务端返回 503 + degraded 报告：这不是 error，报告本身就是要的信息
report, err := sdk.Health.Check(ctx)
fmt.Println(report.Status, report.Luogu.Status)
```

## 错误处理

服务端返回非 0 业务码、HTTP 状态码 >= 400（探活接口名下的 503 除外），或参数在本地校验
失败时，返回 `*luogu2api.Error`（HTTP 状态码 + 业务码 + 服务端文案）：

```go
page, err := sdk.Record.ListByUser(ctx, uid, params)
switch {
case err == nil:
    // ok
case luogu2api.IsInvalidParam(err):         // 400：参数不合法（本地校验失败也是这个码）
case luogu2api.IsUnauthorized(err):         // 401：ADMIN_TOKEN 不对
case luogu2api.IsPoolExhausted(err):        // 1001（HTTP 503）：号池没有可用账号，稍后重试
case luogu2api.IsUpstreamUnauthorized(err): // 1002（HTTP 502）：所有账号登录态都失效，需人工处理
default:
    return err
}
```

网络错误、响应不是合法 JSON、context 取消等**不是** `*Error`，而是被包装的原始错误
（`errors.Is(err, context.DeadlineExceeded)` 等照常可用）；`luogu2api.CodeOf(err)` 对这类
错误返回 `-1`。

## 为什么 ctx 是每个方法的第一个参数

`Get(ctx, ...)` / `ListByUser(ctx, ...)` 这样显式传 ctx 是刻意的，SDK 不提供「构造期注入默认
ctx」或「不传 ctx 的便捷方法」：

- **作用域不匹配**：取消与超时的范围是**单次调用**，不是 client 的生命周期。构造期注入的 ctx
  一旦到期或被取消，整个 client 就永久不可用了 —— 在 handler 里写 `NewSDK(url, token, r.Context())`
  是典型的踩法（第一个请求结束，后面全部 `context canceled`）。
- **上游取消要靠按请求传播**：`sdk.Record.ListByUser(c.Request.Context(), ...)` 才能做到「客户端断开
  → 立刻停止等待洛谷」，构造期注入做不到。
- **Go 约定**：ctx 是函数的第一个参数，不放进 struct 保存；`containedctx` 这类 linter 专门查这个。

副作用只是调用方要写一行 `ctx := context.Background()`（或 `context.WithTimeout`）并复用。
另外 SDK 默认就有 30s 请求超时，所以即便调用方没设 deadline 也不会无限等待。

## 可选参数

| 参数 | 说明 |
|---|---|
| `WithHTTPClient(hc)` | 自备 `*http.Client`（代理、连接池、埋点）；SDK 不修改它 |
| `WithTimeout(d)` | 单次请求超时，默认 30s（会覆盖 client 自带的 Timeout，作用于副本） |
| `WithUserAgent(ua)` | 自定义 User-Agent，默认 `luogu2api-sdk/1.0` |
| `WithMaxResponseBytes(n)` | 单次响应读取上限，默认 16 MiB，超出直接报错而不是截断 |

## 文件

| 文件 | 内容 |
|---|---|
| `sdk.go` | `Client`、`NewSDK`、请求与统一响应体 `{code,message,data}` 解析 |
| `errors.go` | `*Error`、业务码常量与判定函数 |
| `types.go` | 响应数据形状：`Problem`、`SearchResult`、`RecordPage`、`PoolStatus`、`HealthReport` 等 |
| `problem.go` `record.go` `pool.go` `health.go` | 各业务子服务 |
| `doc.go` | 包文档 |

数据形状与服务端 README「接口」一节的 JSON 逐字段对应；字段名沿用洛谷页面结构的原始拼写
（例如题面的 `contenu`、记录的 `sourceCodeLength`）。
