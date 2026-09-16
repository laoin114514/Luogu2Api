# API 文档

## 目录

- [Client 配置](#client-配置)
- [Cookie 管理](#cookie-管理)
- [AuthService 认证](#authservice-认证)
- [ProblemService 题目](#problemservice-题目)
- [RecordService 记录](#recordservice-记录)
- [TrainingService 题单](#trainingservice-题单)
- [UserService 用户](#userservice-用户)
- [DiscussService 讨论](#discussservice-讨论)
- [ContestService 比赛](#contestservice-比赛)
- [类型定义](#类型定义)
- [常量](#常量)
- [错误类型](#错误类型)

---

## Client 配置

```go
client, err := luogu.NewClient(opts ...ClientOption)
```

| Option | 说明 |
|--------|------|
| `WithContext(ctx)` | 设置请求 context（超时/取消），默认 `context.Background()` |
| `WithTimeout(d)` | HTTP 超时，默认 30s |
| `WithRetry(n, backoff)` | 重试次数和退避函数，默认 3 次指数退避 |
| `WithUserAgent(ua)` | 自定义 User-Agent |
| `WithCookies(data)` | 创建时注入 cookie（JSON，格式同 `ExportCookies`；空数据不注入） |

**并发安全：** `Client` 及其 Service 可被多个 goroutine 同时使用（`csrfToken` 由内部 `RWMutex` 保护，
cookie jar 自身并发安全）。配置项（`WithXxx`）只在 `NewClient` 构造期间生效，请在创建时一次性传入；
运行期需要变更的登录态用 `SetCSRF` / `ImportCookies` / `ClearCookies`。

---

## Cookie 管理

Cookie 仅保存在内存中，SDK 不读写任何文件；是否持久化、存到哪里由调用方自行决定。

```go
func (c *Client) ExportCookies() ([]byte, error)   // 导出为 JSON
func (c *Client) ImportCookies(data []byte) error  // 从 JSON 恢复
func (c *Client) ClearCookies() error              // 清空内存中的 cookie
```

```go
// 登录成功后自行保存
data, _ := client.ExportCookies()
os.WriteFile("cookies.json", data, 0600)

// 下次启动时恢复
if data, err := os.ReadFile("cookies.json"); err == nil {
    client.ImportCookies(data)
}

// 或在创建客户端时直接注入（cookie JSON 非法时 NewClient 返回错误）
client, err := luogu.NewClient(luogu.WithCookies(data))
```

---

## AuthService 认证

### RefreshCSRF

```go
func (a *AuthService) RefreshCSRF() error
```

从首页获取 CSRF token。登录前需要先调用。

---

### GetCaptcha

```go
func (a *AuthService) GetCaptcha() ([]byte, error)
```

获取验证码图片，返回 JPEG 字节。

---

### Login

```go
func (a *AuthService) Login(username, password, captcha string) (*LoginResponse, error)
```

用户名、密码、验证码登录。成功返回 `LoginResponse{UID, ClientID}`，cookie 保存在内存中（如需持久化请调用 `Client.ExportCookies`）。

---

### LoginWithSolver

```go
func (a *AuthService) LoginWithSolver(username, password string, solver CaptchaSolver) (*LoginResponse, error)
```

自动获取验证码并登录。`solver` 是 `func([]byte) (string, error)` 类型的验证码识别函数。

```go
// 手动输入验证码
client.Auth.LoginWithSolver("user", "pass", func(img []byte) (string, error) {
    os.WriteFile("captcha.jpg", img, 0644)
    fmt.Print("输入验证码: ")
    var code string
    fmt.Scan(&code)
    return code, nil
})
```

---

### Logout

```go
func (a *AuthService) Logout() error
```

登出并清空内存中的 cookie。

---

### IsAuthenticated

```go
func (a *AuthService) IsAuthenticated() bool
```

检查当前登录状态是否有效（服务端验证，**每次调用都会发起一次网络请求**）。

---

### Verify

```go
func (a *AuthService) Verify() error
```

同 `IsAuthenticated`，返回 error 形式。未登录时返回 `*UnauthorizedError`。

洛谷对未登录访问 `/user/setting` 直接返回 **401**（旧行为是 302 重定向到登录页），两种都会被识别为未登录。

---

## ProblemService 题目

### Get

```go
func (p *ProblemService) Get(pid string) (*Problem, error)
```

获取题目详情。

```go
problem, _ := client.Problem.Get("P1001")
problem.Title          // "A+B Problem"
problem.Difficulty     // 1
problem.TimeLimit()    // 1000 (ms)
problem.MemoryLimit()  // 524288 (KB)
problem.DescText()     // 题面 Markdown
problem.Tags           // []int 标签 ID
```

---

### Search

```go
func (p *ProblemService) Search(params SearchParams) (*SearchResult, error)
```

搜索题目。

```go
results, _ := client.Problem.Search(luogu.SearchParams{
    Keyword:  "排序",
    Page:     1,
    PageSize: 20,
})
results.Total    // 总结果数
results.Problems // []ProblemSummary
```

---

### GetSolutions

```go
func (p *ProblemService) GetSolutions(pid string, page int) (*SolutionList, error)
```

获取题解列表。

```go
solutions, _ := client.Problem.GetSolutions("P1001", 1)
solutions.Total      // 总题解数
solutions.Solutions  // []SolutionSummary
```

---

### GetSolutionDetail

```go
func (p *ProblemService) GetSolutionDetail(sid string) (*Solution, error)
```

获取题解详情（含完整内容）。

```go
detail, _ := client.Problem.GetSolutionDetail("p7fsb45w")
detail.Title    // 题解标题
detail.Content  // Markdown 内容
detail.Likes    // 点赞数
detail.Author.Name
```

---

### GetTranslation

```go
func (p *ProblemService) GetTranslation(pid string) ([]Translation, error)
```

获取题目翻译。

```go
translations, _ := client.Problem.GetTranslation("P1001")
// [{Language: "zh-CN", Title: "A+B Problem"}, {Language: "en", ...}]
```

---

### GetFull

```go
func (p *ProblemService) GetFull(pid string) (*Problem, []Translation, error)
```

一次请求同时获取题目详情和所有翻译，比分别调用 `Get` + `GetTranslation` 更高效。

---

## RecordService 记录

> **需要登录**：`/record/*` 页面匿名访问返回 401（`UserUnloginException`），因此 `GetList` / `GetDetail` 未登录时返回 `*UnauthorizedError`。

### GetList

```go
func (r *RecordService) GetList(params RecordListParams) (*RecordList, error)
```

获取提交记录列表。

```go
records, _ := client.Record.GetList(luogu.RecordListParams{
    User:   1582049,                  // 用户 UID（0 表示不限制）
    Problem: "P1001",                 // 题目 PID（空表示不限制）
    Status: luogu.StatusAccepted,     // 状态过滤（0 表示全部）
    Page:   1,
})
records.Count     // 总记录数
records.Records   // []RecordSummary
```

每条 `RecordSummary` 包含：

| 字段 | 类型 | 说明 |
|------|------|------|
| ID | int | 记录 ID |
| Status | RecordStatus | 评测状态 |
| Score | int | 得分 |
| Time | int | 耗时 (ms) |
| Memory | int | 内存 (KB) |
| Language | Language | 语言 |
| SourceCodeLength | int | 代码长度 (字节) |
| SubmitTime | int64 | 提交时间 (Unix) |
| Problem | ProblemRef | 题目引用 |
| User | UserInfo | 用户信息 |

---

### GetDetail

```go
func (r *RecordService) GetDetail(rid int) (*RecordDetail, error)
```

获取记录详情，包含**完整源代码**和**逐测试点的评测结果**。

```go
detail, _ := client.Record.GetDetail(240247732)
detail.SourceCode                    // 完整源代码
detail.SourceCodeLength              // 代码长度
detail.Detail.CompileResult.Success  // 编译是否成功
detail.Detail.CompileResult.Message  // 编译错误信息

for _, subtask := range detail.Detail.JudgeResult.Subtasks {
    for id, tc := range subtask.TestCases {
        tc.Status       // 测试点状态
        tc.Time         // 耗时
        tc.Memory       // 内存
        tc.Description  // "ok accepted" / "wrong answer On line 1..."
    }
}
```

---

## TrainingService 题单

### GetList

```go
func (t *TrainingService) GetList(params TrainingListParams) (*TrainingList, error)
```

获取题单列表。

```go
trainings, _ := client.Training.GetList(luogu.TrainingListParams{
    Keyword: "入门",
    Page:    1,
})
trainings.Count       // 总数
trainings.Trainings   // []TrainingSummary
```

---

### GetDetail

```go
func (t *TrainingService) GetDetail(tid int) (*TrainingDetail, error)
```

获取题单详情，包含完整题目列表和每题 AC 状态。

```go
detail, _ := client.Training.GetDetail(100)
detail.Name          // "【入门1】顺序结构"
detail.Description   // Markdown 描述
detail.ProblemCount  // 总题数
detail.MarkCount     // 收藏数

for _, p := range detail.Problems {
    p.PID           // "B2002"
    p.Name          // "Hello,World!"
    p.Difficulty    // 1
    p.Accepted      // 是否已 AC
    p.Submitted     // 是否已提交
    p.TotalSubmit   // 总提交数
    p.TotalAccepted // 总 AC 数
}
```

---

## UserService 用户

### Get

```go
func (u *UserService) Get(uid int) (*UserDetail, error)
```

获取用户详情。

```go
user, _ := client.User.Get(1582049)
user.Name       // "laoin"
user.Color      // "Blue"
user.CCFLevel   // CCF 等级
user.XCPCLevel  // XCPC 等级
user.Slogan     // 签名
```

---

### GetRanking

```go
func (u *UserService) GetRanking(page int) (*RankingList, error)
```

获取排名列表。

```go
ranking, _ := client.User.GetRanking(1)
ranking.Count  // 总人数
for _, item := range ranking.Items {
    item.Rating      // 积分
    item.User.Name   // 用户名
    item.User.Color  // 称号颜色
}
```

---

## DiscussService 讨论

### GetList

```go
func (d *DiscussService) GetList(page int) (*DiscussList, error)
```

获取讨论列表。

```go
list, _ := client.Discuss.GetList(1)
list.Count         // 帖子总数
list.Posts         // []DiscussSummary
list.PublicForums  // 公开板块列表
for _, p := range list.Posts {
    p.ID       // 帖子 ID
    p.Title    // 标题
    p.Author.Name
    p.Time     // 发帖时间
}
```

---

### GetDetail

```go
func (d *DiscussService) GetDetail(id int, page int) (*DiscussDetail, error)
```

获取讨论详情（含回帖）。

```go
detail, _ := client.Discuss.GetDetail(241461, 1)
detail.Post.Title           // 主帖标题
detail.Post.Content         // 主帖内容（Markdown）
detail.Count                // 回帖总数
detail.Forum.Name           // 所属板块
for _, r := range detail.Replies {
    r.Content                // 回帖内容
    r.Author.Name
}
```

---

## ContestService 比赛

### GetList

```go
func (c *ContestService) GetList(page int) (*ContestList, error)
```

获取比赛列表。

```go
list, _ := client.Contest.GetList(1)
list.Count     // 比赛总数
for _, c := range list.Contests {
    c.ID             // 比赛 ID
    c.Name           // 比赛名称
    c.StartTime      // 开始时间 (Unix)
    c.EndTime        // 结束时间 (Unix)
    c.Rated          // 是否 Rated
    c.Host.Name      // 主办方
}
```

---

### GetDetail

```go
func (c *ContestService) GetDetail(id int) (*ContestDetail, error)
```

获取比赛详情。

```go
detail, _ := client.Contest.GetDetail(341590)
detail.Name               // 比赛名称
detail.Description        // 比赛说明（Markdown）
detail.Joined             // 是否已报名 (0/1)
detail.TotalParticipants  // 参赛人数
detail.Difficulty         // 难度
```

---

## 类型定义

### LoginRequest

```go
type LoginRequest struct {
    Username string `json:"username"`
    Password string `json:"password"`
    Captcha  string `json:"captcha"`
}
```

### LoginResponse

```go
type LoginResponse struct {
    UID      int    `json:"uid"`
    ClientID string `json:"client_id"`
}
```

> 注意：洛谷在 JSON body 中返回 `uid=0`，真正的 session 通过 `Set-Cookie` 头下发。判断登录成功请用 `IsAuthenticated()`。

### Problem

```go
type Problem struct {
    PID        string         `json:"pid"`
    Title      string         `json:"name"`
    Difficulty int            `json:"difficulty"`
    Tags       []int          `json:"tags"`
    Samples    [][]string     `json:"samples"`
    Limits     ProblemLimits  `json:"limits"`
    Provider   UserInfo       `json:"provider"`
    Content    ProblemContent `json:"contenu"`
}
```

便利方法：`DescText()`, `InputText()`, `OutputText()`, `HintText()`, `TimeLimit()`, `MemoryLimit()`

### SearchParams

```go
type SearchParams struct {
    Keyword  string
    Page     int
    PageSize int
}
```

### Solution

```go
type Solution struct {
    ID      string   `json:"lid"`
    Author  UserInfo `json:"author"`
    Title   string   `json:"title"`
    Content string   `json:"content"`
    Likes   int      `json:"upvote"`
}
```

### UserInfo

```go
type UserInfo struct {
    UID    int    `json:"uid"`
    Name   string `json:"name"`
    Avatar string `json:"avatar"`
}
```

### RecordListParams

```go
type RecordListParams struct {
    User    int          // 用户 UID（0 = 不限）
    Problem string       // 题目 PID（空 = 不限）
    Status  RecordStatus // 状态过滤
    Page    int
}
```

### RecordSummary

```go
type RecordSummary struct {
    ID               int          `json:"id"`
    Status           RecordStatus `json:"status"`
    Score            int          `json:"score"`
    Time             int          `json:"time"`
    Memory           int          `json:"memory"`
    SourceCodeLength int          `json:"sourceCodeLength"`
    SubmitTime       int64        `json:"submitTime"`
    Language         Language     `json:"language"`
    EnableO2         bool         `json:"enableO2"`
    Problem          ProblemRef   `json:"problem"`
    User             UserInfo     `json:"user"`
}
```

### RecordDetail

```go
type RecordDetail struct {
    RecordSummary
    SourceCode string      `json:"sourceCode"`
    Detail     JudgeDetail `json:"detail"`
}
```

### JudgeDetail

```go
type JudgeDetail struct {
    CompileResult CompileResult `json:"compileResult"`
    JudgeResult   JudgeResult   `json:"judgeResult"`
}
```

### TrainingListParams

```go
type TrainingListParams struct {
    Keyword string
    Type    int
    Page    int
}
```

### TrainingSummary

```go
type TrainingSummary struct {
    ID           int      `json:"id"`
    Name         string   `json:"name"`
    Type         int      `json:"type"`
    Provider     UserInfo `json:"provider"`
    CreateTime   int64    `json:"createTime"`
    ProblemCount int      `json:"problemCount"`
    MarkCount    int      `json:"markCount"`
}
```

### TrainingDetail

```go
type TrainingDetail struct {
    TrainingSummary
    Description string            `json:"description"`
    Marked      bool              `json:"marked"`
    Problems    []TrainingProblem `json:"problems"`
}
```

### TrainingProblem

```go
type TrainingProblem struct {
    PID           string `json:"pid"`
    Type          string `json:"type"`
    Name          string `json:"name"`
    Difficulty    int    `json:"difficulty"`
    Submitted     bool   `json:"submitted"`
    Accepted      bool   `json:"accepted"`
    TotalSubmit   int    `json:"totalSubmit"`
    TotalAccepted int    `json:"totalAccepted"`
}
```

---

### UserDetail

```go
type UserDetail struct {
    UID        int    `json:"uid"`
    Name       string `json:"name"`
    Avatar     string `json:"avatar"`
    Slogan     string `json:"slogan"`
    Color      string `json:"color"`
    CCFLevel   int    `json:"ccfLevel"`
    XCPCLevel  int    `json:"xcpcLevel"`
    Background string `json:"background"`
    IsAdmin    bool   `json:"isAdmin"`
    IsBanned   bool   `json:"isBanned"`
}
```

### UserSummary

```go
type UserSummary struct {
    UID     int    `json:"uid"`
    Name    string `json:"name"`
    Avatar  string `json:"avatar"`
    Slogan  string `json:"slogan"`
    Color   string `json:"color"`
    IsAdmin bool   `json:"isAdmin"`
}
```

### RankingList / RankingItem

```go
type RankingList struct {
    Items []RankingItem
    Count int
}

type RankingItem struct {
    Rating int         `json:"rating"`
    Time   int64       `json:"time"`
    User   UserSummary `json:"user"`
}
```

### DiscussList / DiscussSummary

```go
type DiscussList struct {
    Posts        []DiscussSummary
    Count        int
    PublicForums []DiscussForum
}

type DiscussSummary struct {
    ID     int         `json:"id"`
    Title  string      `json:"title"`
    Author UserSummary `json:"author"`
    Time   int64       `json:"time"`
}
```

### DiscussDetail / DiscussPost / DiscussReply

```go
type DiscussDetail struct {
    Post    DiscussPost
    Replies []DiscussReply
    Count   int
    Forum   DiscussForum
}

type DiscussPost struct {
    ID      int         `json:"id"`
    Title   string      `json:"title"`
    Author  UserSummary `json:"author"`
    Time    int64       `json:"time"`
    Content string      `json:"content"`
    Topped  bool        `json:"topped"`
    Locked  bool        `json:"locked"`
}

type DiscussReply struct {
    ID      int         `json:"id"`
    Author  UserSummary `json:"author"`
    Time    int64       `json:"time"`
    Content string      `json:"content"`
}
```

### DiscussForum

```go
type DiscussForum struct {
    Name  string `json:"name"`
    Type  int    `json:"type"`
    Slug  string `json:"slug"`
    Color string `json:"color"`
}
```

### ContestList / ContestSummary

```go
type ContestList struct {
    Contests []ContestSummary
    Count    int
}

type ContestSummary struct {
    ID                 int         `json:"id"`
    Name               string      `json:"name"`
    StartTime          int64       `json:"startTime"`
    EndTime            int64       `json:"endTime"`
    Rated              int         `json:"rated"`
    Host               ContestHost `json:"host"`
    ProblemCount       int         `json:"problemCount"`
}
```

### ContestDetail

```go
type ContestDetail struct {
    ContestSummary
    Joined            int    `json:"joined"`
    Description       string `json:"description"`
    Difficulty        int    `json:"difficulty"`
    TotalParticipants int    `json:"totalParticipants"`
}
```

---

## 常量

### RecordStatus — 提交状态

| 常量 | 值 | 含义 | 验证 |
|------|---|------|------|
| `StatusCompiling` | 2 | 编译/等待中 | score=null |
| `StatusAccepted` | 12 | 通过 | score=100 |
| `StatusUnaccepted` | 14 | 未通过 | score<100 |

### RecordStatus — 测试点状态

| 常量 | 值 | 含义 | 验证 |
|------|---|------|------|
| `TestCaseMLEorTLE` | 4 | 资源超限 | description="" |
| `TestCaseWrongAnswer` | 6 | 答案错误 | description="wrong answer..." |
| `TestCaseAccepted` | 12 | 通过 | description="ok accepted" |

### Language — 编程语言

| 常量 | 值 | 含义 | 验证 |
|------|---|------|------|
| `LangGo` | 14 | Go | 源码匹配 |
| `LangCPP14` | 28 | C++14 | 出现频率 |

---

## 错误类型

```go
// 登录/认证失败（用户名、密码、验证码错误等）
type AuthError struct {
    Code    int
    Message string
}

// CSRF token 获取失败
type CSRFError struct{ Err error }

// 网络请求失败
type NetworkError struct{ Err error }

// 未登录或权限不足（HTTP 401/403）
type UnauthorizedError struct {
    StatusCode int    // 触发该错误的 HTTP 状态码
    Message    string // 触发该错误的操作描述，例如 "get problem P1001"
}
```

所有错误实现 `error` 接口，`CSRFError` 和 `NetworkError` 支持 `errors.Unwrap()`。

### 401 / 403 的统一映射

所有 Service 方法都经过 `checkResponse`：响应为 **401 或 403** 时返回 `*UnauthorizedError`，
其余非 200 状态码返回带原始状态码的普通错误。可用 `errors.As` 判断：

```go
_, err := client.Record.GetDetail(240247732)
var unauthorized *luogu.UnauthorizedError
if errors.As(err, &unauthorized) {
    // 未登录：需要先登录或恢复 cookie
    log.Println(unauthorized.StatusCode, unauthorized.Message)
}
```

实测需登录的端点（匿名 401）：`/record/list`、`/record/{id}`、`/problem/solution/{pid}`、
`/training/{id}`、`/user/setting`。其中 `/record/*` 即使查询他人记录也需要登录。

