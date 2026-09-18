// Package luogu2api 是 Luogu2Api 服务 HTTP API 的 Go 客户端（SDK）。
//
// 它只封装 /api/v1 下对外暴露的业务接口与两个公开探活接口：
//
//	GET /api/v1/problems/:pid                          题目详情  → Client.Problem.Get
//	GET /api/v1/problems?keyword=&page=&pageSize=     题目搜索  → Client.Problem.Search
//	GET /api/v1/users/:uid/records?pid=&status=&page=  提交记录  → Client.Record.ListByUser
//	GET /api/v1/pool/status                            号池快照  → Client.Pool.Status
//	GET /healthz、GET /livez                           探活      → Client.Health.Check / Live
//
// 管理接口（/api/v1/admin/accounts…：导入账号、改密、启停、软删除、强制重登）刻意不封装：
// 它们对应的是运维动作，继续由管理台（/dashboard/）或 curl 完成。SDK 的使用者拿到的是
// "查数据"的能力，不需要也不应该经手号池里的账号凭据。
//
// # 用法
//
//	sdk, err := luogu2api.NewSDK("http://127.0.0.1:8080", os.Getenv("ADMIN_TOKEN"))
//	if err != nil {
//		return err // 地址非法或令牌为空：构造期就失败（fail fast）
//	}
//
//	problem, err := sdk.Problem.Get(ctx, "P1001")
//	if err != nil {
//		return err
//	}
//	fmt.Println(problem.PID, problem.Title)
//
// 令牌在 NewSDK 时注入，之后每个请求都会带 X-Admin-Token 请求头（/healthz、/livez 本来
// 不需要令牌，SDK 也会带上，服务端忽略）。Client 与它的子服务都没有可变状态，可并发使用。
//
// ctx 刻意做成每个方法的第一个参数（而不是构造期注入）：取消与超时的作用域是单次调用，
// 构造期注入会让 client 的寿命和第一个 ctx 绑死（典型踩法：在 handler 里传 r.Context()，
// 首个请求结束后续调用全部 context canceled），也做不到「调用方断开就停止上游请求」。
//
// # 独立、可复制
//
// 这个包只用标准库，不依赖本仓库的任何其它代码：把 luogu2api/ 目录整个复制进别的工程就能
// 直接 import 使用（目录名随意，包名始终是 luogu2api，不需要改任何一行）。留在本仓库里时
// 也可以按主模块路径 github.com/laoin114514/luogu2api/luogu2api 引入。
//
// # 错误
//
// 服务端返回非 0 业务码、HTTP 状态码 >= 400（探活接口名下的 503 除外），或参数在本地校验
// 失败时，SDK 返回 *Error；网络错误、响应不是合法 JSON、context 取消等则是被包装过的原始
// 错误，errors.Is / errors.As 仍然可用。判定的细节见 errors.go。
//
// # 数据形状
//
// types.go 里的类型如实镜像服务端响应（字段名沿用洛谷页面结构与 pkg/luoguClient 的命名），
// 调用方因此不需要再依赖 pkg/luoguClient 或 internal/*——这个包只使用标准库。
package luogu2api
