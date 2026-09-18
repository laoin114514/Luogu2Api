package luogu2api

import "time"

// 本文件定义服务端响应的数据形状，与 README「接口」一节一一对应。
//
// 字段名沿用洛谷页面结构与 pkg/luoguClient 的原始拼写（例如题目详情里的 contenu、
// 记录里的 sourceCodeLength），这样把 curl 的响应与类型定义对照时不用做心算。

// --- 题目 ---

// Problem 题目详情（GET /api/v1/problems/:pid），对应洛谷题目页 lentille-context 里的
// problem 对象。
type Problem struct {
	// PID 题目编号，例如 P1001
	PID string `json:"pid"`
	// Title 题目标题（服务端字段名沿用洛谷的 name）
	Title string `json:"name"`
	// Difficulty 难度：0 暂无评定、1 入门、2 普及−、3 普及/提高−、4 普及+/提高、
	// 5 提高+/省选−、6 省选/NOI−、7 NOI/NOI+/CTSC
	Difficulty int `json:"difficulty"`
	// Tags 算法标签 ID（洛谷的数字标签，不是文案）
	Tags []int `json:"tags"`
	// Samples 样例：每项是 [输入, 输出]
	Samples [][]string `json:"samples"`
	// Limits 时空限制
	Limits ProblemLimits `json:"limits"`
	// Provider 出题人
	Provider UserInfo `json:"provider"`
	// Content 题面内容（服务端字段名沿用洛谷的 contenu）
	Content ProblemContent `json:"contenu"`
}

// ProblemContent 题面内容（Markdown 文本）
type ProblemContent struct {
	// Description 题目描述
	Description string `json:"description"`
	// InputFormat 输入格式
	InputFormat string `json:"formatI"`
	// OutputFormat 输出格式
	OutputFormat string `json:"formatO"`
	// Hint 说明/提示
	Hint string `json:"hint"`
	// Background 题目背景
	Background string `json:"background"`
}

// ProblemLimits 时空限制。是数组：洛谷在多子任务题上会给出多个值，通常取第一个即可。
type ProblemLimits struct {
	// Time 时间限制（毫秒）
	Time []int `json:"time"`
	// Memory 内存限制（KB）
	Memory []int `json:"memory"`
}

// UserInfo 用户信息（出题人、提交者等共用）
type UserInfo struct {
	UID    int    `json:"uid"`
	Name   string `json:"name"`
	Avatar string `json:"avatar"`
}

// ProblemSummary 题目摘要（搜索结果的条目）
type ProblemSummary struct {
	PID        string `json:"pid"`
	Title      string `json:"name"`
	Difficulty int    `json:"difficulty"`
	Tags       []int  `json:"tags"`
}

// SearchResult 题目搜索结果（GET /api/v1/problems）。
//
// 注意：服务端把上游 SDK 的结构体直接序列化，线上字段名是大写的 Problems/Total/Page/PerPage；
// encoding/json 的字段匹配不区分大小写，所以这里用小写标签就能同时兼容两种写法。
type SearchResult struct {
	// Problems 本页题目
	Problems []ProblemSummary `json:"problems"`
	// Total 符合条件的题目总数（不是本页条数）
	Total int `json:"total"`
	// Page 当前页码（服务端回显，最小为 1）
	Page int `json:"page"`
	// PerPage 每页条数（服务端默认 20，上限 100）
	PerPage int `json:"perPage"`
}

// --- 提交记录 ---

// RecordStatus 洛谷评测状态码
type RecordStatus int

// 提交状态常量。来源：洛谷实际响应（实测抓包验证）。
const (
	// RecordStatusCompiling 编译/等待中（score 为 0）
	RecordStatusCompiling RecordStatus = 2
	// RecordStatusAccepted 通过（score 为 100）
	RecordStatusAccepted RecordStatus = 12
	// RecordStatusUnaccepted 未通过/部分分
	RecordStatusUnaccepted RecordStatus = 14
)

// Language 编程语言 ID
type Language int

// 语言常量：只列出实测确认过取值的语言，其余取值原样透传。
const (
	// LanguageGo Go
	LanguageGo Language = 14
	// LanguageCPP14 C++14
	LanguageCPP14 Language = 28
)

// ProblemRef 记录中的题目引用（洛谷该字段名是 name，不是 title）
type ProblemRef struct {
	PID        string `json:"pid"`
	Title      string `json:"name"`
	Difficulty int    `json:"difficulty"`
	FullScore  int    `json:"fullScore"`
	Type       string `json:"type"`
	Submitted  bool   `json:"submitted"`
	Accepted   bool   `json:"accepted"`
}

// RecordSummary 单条提交记录摘要
type RecordSummary struct {
	// ID 记录 ID（洛谷的 rid）
	ID int `json:"id"`
	// Status 评测状态，见 RecordStatus* 常量
	Status RecordStatus `json:"status"`
	// Score 得分（满分 100）
	Score int `json:"score"`
	// Time 最大耗时（毫秒）
	Time int `json:"time"`
	// Memory 最大内存（KB）
	Memory int `json:"memory"`
	// SourceCodeLength 源代码长度（字节）
	SourceCodeLength int `json:"sourceCodeLength"`
	// SubmitTime 提交时间（Unix 秒）
	SubmitTime int64 `json:"submitTime"`
	// Language 编程语言，见 Language* 常量
	Language Language `json:"language"`
	// EnableO2 是否开启 O2 优化
	EnableO2 bool `json:"enableO2"`
	// Problem 题目引用
	Problem ProblemRef `json:"problem"`
	// User 提交者
	User UserInfo `json:"user"`
}

// RecordPage 提交记录分页（对应服务端的 RecordListDTO）。
//
// 分页信息由服务端算好后回显（每页条数固定 20），调用方不必自己数：
// count=178、page=9 时 totalPages=9、pageRecordCount=18。
type RecordPage struct {
	// UID 查询的洛谷用户 UID
	UID int `json:"uid"`
	// PID 题目过滤条件（空表示未按题目过滤）
	PID string `json:"pid,omitempty"`
	// Status 状态过滤条件（0 表示未按状态过滤）
	Status RecordStatus `json:"status,omitempty"`
	// Page 当前页码（从 1 开始）
	Page int `json:"page"`
	// PageSize 每页条数（洛谷固定值 20）
	PageSize int `json:"pageSize"`
	// TotalPages 总页数 = ceil(count/pageSize)；count 为 0 时是 0
	TotalPages int `json:"totalPages"`
	// Count 符合条件的记录总数（不是本页条数）
	Count int `json:"count"`
	// PageRecordCount 本页条数：最后一页可能不满，页码超出总页数时为 0
	PageRecordCount int `json:"pageRecordCount"`
	// Records 本页记录
	Records []RecordSummary `json:"records"`
}

// --- 号池状态 ---

// SweepResult 号池一轮扫描的统计（durationNs 是纳秒）
type SweepResult struct {
	At        time.Time     `json:"at"`
	Checked   int           `json:"checked"`
	OK        int           `json:"ok"`
	Relogged  int           `json:"relogged"`
	Declared  int           `json:"declared"`
	Pending   int           `json:"pending"`
	Disabled  int           `json:"disabled"`
	Banned    int           `json:"banned"`
	Transient int           `json:"transient"`
	Busy      int           `json:"busy"`
	Duration  time.Duration `json:"durationNs"`
}

// PoolStatus 号池快照（GET /api/v1/pool/status）。
//
// 是服务端的内存快照，不含任何账号凭据，适合做监控与告警。
type PoolStatus struct {
	// Total 账号总数
	Total int `json:"total"`
	// Online 在线（可被选中服务）的账号数
	Online int `json:"online"`
	// ReloginPending 等待重登的账号数
	ReloginPending int `json:"reloginPending"`
	// ReloginFailed 重登失败的账号数
	ReloginFailed int `json:"reloginFailed"`
	// Disabled 已停用的账号数
	Disabled int `json:"disabled"`
	// Banned 被洛谷封禁/限制的账号数
	Banned int `json:"banned"`
	// LastSweepAt 最近一轮扫描时间；从未扫描过时是零值（0001-01-01T00:00:00Z）
	LastSweepAt time.Time `json:"lastSweepAt"`
	// LastSweep 最近一轮扫描的统计；从未扫描过时为 nil
	LastSweep *SweepResult `json:"lastSweep,omitempty"`
}

// --- 探活 ---

// 状态取值，与服务端 service 包的常量一一对应
const (
	// StatusOK 依赖正常
	StatusOK = "ok"
	// StatusDegraded 依赖异常：数据库不可用或号池没有在线账号（HTTP 503）
	StatusDegraded = "degraded"
	// StatusError 单个依赖不可用（在 ComponentStatus.Status 里出现）
	StatusError = "error"
	// StatusDisabled 单个依赖未启用（在 ComponentStatus.Status 里出现）
	StatusDisabled = "disabled"
	// StatusAuthenticated 号池至少有一个在线账号（在 LuoguHealth.Status 里出现）
	StatusAuthenticated = "authenticated"
	// StatusUnavailable 号池一个在线账号都没有（在 LuoguHealth.Status 里出现）
	StatusUnavailable = "unavailable"
)

// ComponentStatus 单个依赖的状态
type ComponentStatus struct {
	Status string `json:"status"`
	// Error 失败原因；仅在 Status 为 StatusError 时出现
	Error string `json:"error,omitempty"`
}

// LuoguHealth 号池的健康状态
type LuoguHealth struct {
	// Status StatusAuthenticated（有在线账号）或 StatusUnavailable（没有）
	Status         string `json:"status"`
	Total          int    `json:"total"`
	Online         int    `json:"online"`
	ReloginPending int    `json:"reloginPending"`
	ReloginFailed  int    `json:"reloginFailed"`
	Disabled       int    `json:"disabled"`
	Banned         int    `json:"banned"`
	// LastSweepAt 最近一轮扫描时间（RFC3339）；从未扫描过时为空字符串
	LastSweepAt string `json:"lastSweepAt,omitempty"`
}

// HealthReport 健康检查结果（GET /healthz）
type HealthReport struct {
	// Status StatusOK 或 StatusDegraded
	Status string          `json:"status"`
	DB     ComponentStatus `json:"db"`
	Luogu  LuoguHealth     `json:"luogu"`
}

// LiveStatus 存活探针结果（GET /livez）：进程能处理 HTTP 就是 StatusOK。
type LiveStatus struct {
	Status string `json:"status"`
}
