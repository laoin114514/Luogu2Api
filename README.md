# LuoguClient

洛谷 (luogu.com.cn) 平台的 Go Client，覆盖认证、题目、记录、题单等核心功能。

写入能力只有账号偏好设置：`User.GetPreference` / `User.UpdatePreference`
以及建立在它们之上的 `User.JoinOpenSourcePlan`（幂等加入"代码公开计划"），其余接口均为只读。

## 安装

```bash
go get github.com/laoin114514/luoguClient
```

## 快速开始

```go
package main

import (
    "fmt"
    luogu "github.com/laoin114514/luoguClient"
)

func main() {
    client, _ := luogu.NewClient()

    // 登录（首次需要手动输入验证码）
    if !client.Auth.IsAuthenticated() {
        client.Auth.RefreshCSRF()
        client.Auth.LoginWithSolver("username", "password", mySolver) // mySolver 需自行实现，Client 不内置 OCR
        // 登录响应体不含 uid，自身 UID 从 _uid cookie 读取；OCR 识别会偶发出错，建议失败重试
        fmt.Println(client.UID())
    }

    // 获取题目
    problem, _ := client.Problem.Get("P1001")
    fmt.Println(problem.Title) // A+B Problem

    // 搜索题目
    results, _ := client.Problem.Search(luogu.SearchParams{
        Keyword: "排序", Page: 1,
    })

    // 查看提交记录
    records, _ := client.Record.GetList(luogu.RecordListParams{
        User:   1582049,
        Status: luogu.StatusAccepted,
        Page:   1,
    })

    // 获取题单
    list, _ := client.Training.GetList(luogu.TrainingListParams{Page: 1})
    detail, _ := client.Training.GetDetail(list.Trainings[0].ID)

    // 偏好设置：先读后写（省略字段会被服务端重置成默认值，别手写半个对象）
    pref, _ := client.User.GetPreference()
    pref.AcceptPromotion = false
    updated, _ := client.User.UpdatePreference(*pref)
    fmt.Println(updated.LearningMode, updated.OpenSource)

    // 加入"代码公开计划"：幂等（读-改-写，不误伤其它偏好）。
    // ⚠️ 不可逆：加入后 30 天内不能退出，只在明确需要时调用。
    joinTime, _ := client.User.JoinOpenSourcePlan()
    fmt.Println(joinTime) // Unix 秒；+30 天即解锁时间
}
```

## Cookie 管理

Cookie 只保存在内存中，Client 不读写任何文件。是否持久化、存到哪里（文件、数据库、Redis 等）由调用方自行决定。

```go
client, _ := luogu.NewClient()

// 从调用方自己的存储恢复登录态
if data, err := os.ReadFile("cookies.json"); err == nil {
    client.ImportCookies(data)
}

// 或者创建时直接注入
client, _ := luogu.NewClient(luogu.WithCookies(data))

// 登录成功后导出，由调用方自行保存
data, _ := client.ExportCookies()
os.WriteFile("cookies.json", data, 0600)

// 清空内存中的 cookie（例如登出后）
client.ClearCookies()
```

## Context 控制

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
client, _ := luogu.NewClient(luogu.WithContext(ctx))
client.Problem.Get("P1001") // 5 秒超时自动中断
```

## 内置常量

```go
// 提交状态（通过实测抓包验证）
luogu.StatusCompiling   // 2  编译中
luogu.StatusAccepted    // 12 通过
luogu.StatusUnaccepted  // 14 未通过

// 测试点状态
luogu.TestCaseMLEorTLE    // 4  资源超限
luogu.TestCaseWrongAnswer // 6  答案错误
luogu.TestCaseAccepted    // 12 通过

// 编程语言
luogu.LangGo    // 14
luogu.LangCPP14 // 28

// 偏好设置：代码公开范围
luogu.OpenSourcePrivacyProtection // -1 完全隐私保护
luogu.OpenSourceDisabled          // 0  不公开代码
luogu.OpenSourceEnabled           // 1  加入代码公开计划

// 偏好设置：私信接收范围
luogu.MessageReceiveAdminOnly // 0 仅限管理员
luogu.MessageReceiveFollowing // 1 关注的人与管理员
luogu.MessageReceiveAnyone    // 2 所有人（拉黑的用户除外）
```

## 项目结构

```
luoguClient/
├── client.go        # Client 核心、HTTP 请求、配置项、页面 JSON 解析
├── auth.go          # AuthService 认证
├── problem.go       # ProblemService 题目
├── record.go        # RecordService 提交记录
├── training.go      # TrainingService 题单
├── user.go          # UserService 用户 / 排名 / 偏好设置
├── discuss.go       # DiscussService 讨论
├── contest.go       # ContestService 比赛
├── types.go         # 所有公开类型
├── constants.go     # 状态/语言常量
├── errors.go        # 错误类型
├── cookiestore.go   # Cookie 导出/导入（仅内存存储）
├── retry.go         # 重试逻辑
├── api.md           # 完整 API 文档
└── example/main.go  # 使用示例
```

`Client` 及其 Service 可并发使用；配置项（`WithXxx`）只在 `NewClient` 构造期间生效。

**验证码需要自己识别**：Client 只定义 `CaptchaSolver` 函数类型（不内置 OCR），
`example/main.go` 的做法是把验证码图片存成 `captcha.jpg` 后手动输入。

## 错误类型

```go
&luogu.AuthError{Code: 400, Message: "login failed"}
&luogu.CSRFError{Err: ...}
&luogu.NetworkError{Err: ...}
&luogu.UnauthorizedError{StatusCode: 401, Message: "get record list"}
&luogu.APIError{StatusCode: 400, Message: "加入代码公开计划未满 30 天，不能退出"}
```

`CSRFError` 与 `NetworkError` 支持 `errors.Unwrap()`；`AuthError`、`UnauthorizedError`
与 `APIError` 不包装底层错误。
需要登录的接口（`/record/*`、`/problem/solution/*`、`/training/{id}`、`/user/setting` 等）在未登录时返回 401，
Client 统一转换成 `*luogu.UnauthorizedError`，可用 `errors.As` 判断；详见 `api.md`。

写接口的业务拒绝（HTTP 400）转换成 `*luogu.APIError`，保留洛谷的 `errorType` 与中文文案；
目前只有 `User.UpdatePreference` 会产生该错误。

## 许可证

MIT
