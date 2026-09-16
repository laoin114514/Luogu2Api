package luoguclient

// CaptchaSolver 验证码求解器，接收 JPEG 图片字节，返回识别结果
type CaptchaSolver func(image []byte) (string, error)

// LoginRequest 登录请求体
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Captcha  string `json:"captcha"`
}

// LoginResponse 登录响应
//
// 洛谷登录成功后通过 Set-Cookie 建立会话，响应体只有下列字段（实测）：
//
//	{"username":"xxx","locked":false,"syncToken":"...","redirectTo":"/"}
//
// 响应里**不含 uid**；需要 UID 请用 Client.UID()（读 _uid cookie），
// 判断登录是否成功请用 AuthService.IsAuthenticated()。
type LoginResponse struct {
	Username   string `json:"username"`
	Locked     bool   `json:"locked"`
	SyncToken  string `json:"syncToken"`
	RedirectTo string `json:"redirectTo"`
}

// Problem 题目详情（匹配洛谷 SSR 页面中 lentille-context 的实际结构）
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

// DescText 返回题目描述的 Markdown 文本
func (p *Problem) DescText() string { return p.Content.Description }

// InputText 返回输入格式描述
func (p *Problem) InputText() string { return p.Content.InputFormat }

// OutputText 返回输出格式描述
func (p *Problem) OutputText() string { return p.Content.OutputFormat }

// HintText 返回说明/提示
func (p *Problem) HintText() string { return p.Content.Hint }

// TimeLimit 返回时间限制（ms），取第一个值
func (p *Problem) TimeLimit() int {
	if len(p.Limits.Time) > 0 {
		return p.Limits.Time[0]
	}
	return 0
}

// MemoryLimit 返回内存限制（KB），取第一个值
func (p *Problem) MemoryLimit() int {
	if len(p.Limits.Memory) > 0 {
		return p.Limits.Memory[0]
	}
	return 0
}

// ProblemContent 题目内容（从 contenu/content 字段提取）
type ProblemContent struct {
	Description  string `json:"description"`
	InputFormat  string `json:"formatI"`
	OutputFormat string `json:"formatO"`
	Hint         string `json:"hint"`
	Background   string `json:"background"`
}

// ProblemLimits 时空限制
type ProblemLimits struct {
	Time   []int `json:"time"`
	Memory []int `json:"memory"`
}

// SearchParams 题目搜索参数
type SearchParams struct {
	Keyword  string
	Page     int
	PageSize int
}

// SearchResult 搜索结果
type SearchResult struct {
	Problems []ProblemSummary
	Total    int
	Page     int
	PerPage  int
}

// ProblemSummary 题目摘要
type ProblemSummary struct {
	PID        string `json:"pid"`
	Title      string `json:"name"`
	Difficulty int    `json:"difficulty"`
	Tags       []int  `json:"tags"`
}

// Solution 题解详情
type Solution struct {
	ID      string   `json:"lid"`
	Author  UserInfo `json:"author"`
	Title   string   `json:"title"`
	Content string   `json:"content"`
	Likes   int      `json:"upvote"`
}

// SolutionList 题解列表
type SolutionList struct {
	Solutions []SolutionSummary
	Total     int
	Page      int
	PerPage   int
}

// SolutionSummary 题解摘要
type SolutionSummary struct {
	ID     string   `json:"lid"`
	Title  string   `json:"title"`
	Author UserInfo `json:"author"`
	Likes  int      `json:"upvote"`
}

// Translation 题目翻译
type Translation struct {
	Language     string `json:"-"`
	Title        string `json:"name"`
	Description  string `json:"description"`
	InputFormat  string `json:"formatI"`
	OutputFormat string `json:"formatO"`
	Hint         string `json:"hint"`
	Background   string `json:"background"`
}

// UserInfo 用户信息
type UserInfo struct {
	UID    int    `json:"uid"`
	Name   string `json:"name"`
	Avatar string `json:"avatar"`
}

// --- 记录 (Record) ---

// RecordListParams 记录搜索参数
type RecordListParams struct {
	User    int          // 用户 UID（0 表示不限制）
	Problem string       // 题目 PID（空表示不限制）
	Status  RecordStatus // 状态过滤（0 表示全部，可用 StatusAccepted 等常量）
	Page    int          // 页码
}

// RecordList 记录列表
type RecordList struct {
	Records []RecordSummary
	Count   int
}

// RecordSummary 记录摘要
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

// ProblemRef 记录中的题目引用（实测：洛谷该字段名为 name，不是 title）
type ProblemRef struct {
	PID        string `json:"pid"`
	Title      string `json:"name"`
	Difficulty int    `json:"difficulty"`
	FullScore  int    `json:"fullScore"`
	Type       string `json:"type"`
	Submitted  bool   `json:"submitted"`
	Accepted   bool   `json:"accepted"`
}

// RecordDetail 记录详情（含源代码和评测详情）
type RecordDetail struct {
	RecordSummary
	SourceCode string      `json:"sourceCode"`
	Detail     JudgeDetail `json:"detail"`
}

// JudgeDetail 评测详情
type JudgeDetail struct {
	CompileResult CompileResult `json:"compileResult"`
	JudgeResult   JudgeResult   `json:"judgeResult"`
}

// CompileResult 编译结果
type CompileResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Opt2    bool   `json:"opt2"`
}

// JudgeResult 评测结果
//
// 注意：实测 judgeResult 这一层的 time/memory/score/status 恒为 0（洛谷不再在此层汇总），
// 有意义的数据在 Subtasks 与 Subtasks[].TestCases；总分/总状态请用 RecordSummary.Score / Status。
type JudgeResult struct {
	Subtasks          []SubtaskResult `json:"subtasks"`
	FinishedCaseCount int             `json:"finishedCaseCount"`
	Status            RecordStatus    `json:"status"`
	Time              int             `json:"time"`
	Memory            int             `json:"memory"`
	Score             int             `json:"score"`
}

// SubtaskResult 子任务结果
type SubtaskResult struct {
	ID        int              `json:"id"`
	Score     int              `json:"score"`
	Status    RecordStatus     `json:"status"`
	Time      int              `json:"time"`
	Memory    int              `json:"memory"`
	Judger    string           `json:"judger"`
	TestCases []TestCaseResult `json:"testCases"`
}

// TestCaseResult 单个测试点结果
type TestCaseResult struct {
	ID          int          `json:"id"`
	Status      RecordStatus `json:"status"`
	Time        int          `json:"time"`
	Memory      int          `json:"memory"`
	Score       int          `json:"score"`
	Signal      int          `json:"signal"`
	ExitCode    int          `json:"exitCode"`
	Description string       `json:"description"`
	SubtaskID   int          `json:"subtaskID"`
}

// --- 题单 (Training) ---

// TrainingListParams 题单搜索参数
type TrainingListParams struct {
	Keyword string // 关键词搜索
	Type    int    // 题单类型（0 表示全部）
	Page    int    // 页码
}

// TrainingList 题单列表
type TrainingList struct {
	Trainings []TrainingSummary
	Count     int
}

// TrainingSummary 题单摘要
type TrainingSummary struct {
	ID           int      `json:"id"`
	Name         string   `json:"name"`
	Type         int      `json:"type"`
	Provider     UserInfo `json:"provider"`
	CreateTime   int64    `json:"createTime"`
	Deadline     *int64   `json:"deadline"`
	ProblemCount int      `json:"problemCount"`
	MarkCount    int      `json:"markCount"`
}

// TrainingDetail 题单详情
type TrainingDetail struct {
	TrainingSummary
	Description string            `json:"description"`
	Marked      bool              `json:"marked"`
	Problems    []TrainingProblem `json:"problems"`
}

// TrainingProblem 题单中的题目
type TrainingProblem struct {
	PID           string `json:"pid"`
	Type          string `json:"type"`
	Name          string `json:"name"`
	Difficulty    int    `json:"difficulty"`
	Submitted     bool   `json:"submitted"`
	Accepted      bool   `json:"accepted"`
	Tags          []int  `json:"tags"`
	TotalSubmit   int    `json:"totalSubmit"`
	TotalAccepted int    `json:"totalAccepted"`
	Flag          int    `json:"flag"`
}

// --- 用户 (User) ---

// UserDetail 用户详情
type UserDetail struct {
	UID        int    `json:"uid"`
	Name       string `json:"name"`
	Avatar     string `json:"avatar"`
	Slogan     string `json:"slogan"`
	Badge      string `json:"badge"`
	IsAdmin    bool   `json:"isAdmin"`
	IsBanned   bool   `json:"isBanned"`
	Color      string `json:"color"`
	CCFLevel   int    `json:"ccfLevel"`
	XCPCLevel  int    `json:"xcpcLevel"`
	Background string `json:"background"`
}

// --- 排名 (Ranking) ---

// RankingList 排名列表
type RankingList struct {
	Items []RankingItem
	Count int
}

// RankingItem 排名项
type RankingItem struct {
	Rating int         `json:"rating"`
	Time   int64       `json:"time"`
	User   UserSummary `json:"user"`
}

// UserSummary 用户摘要
type UserSummary struct {
	UID     int    `json:"uid"`
	Name    string `json:"name"`
	Avatar  string `json:"avatar"`
	Slogan  string `json:"slogan"`
	Color   string `json:"color"`
	Badge   string `json:"badge"`
	IsAdmin bool   `json:"isAdmin"`
}

// --- 讨论 (Discuss) ---

// DiscussList 讨论列表
type DiscussList struct {
	Posts        []DiscussSummary
	Count        int
	PublicForums []DiscussForum
}

// DiscussSummary 讨论摘要
type DiscussSummary struct {
	ID     int         `json:"id"`
	Title  string      `json:"title"`
	Author UserSummary `json:"author"`
	Time   int64       `json:"time"`
}

// DiscussPosts 讨论帖列表（API 包装）
type DiscussPosts struct {
	Result []DiscussSummary `json:"result"`
	Count  int              `json:"count"`
}

// DiscussForum 讨论板块
type DiscussForum struct {
	Name  string `json:"name"`
	Type  int    `json:"type"`
	Slug  string `json:"slug"`
	Color string `json:"color"`
}

// DiscussDetail 讨论详情
type DiscussDetail struct {
	Post    DiscussPost
	Replies []DiscussReply
	Count   int
	Forum   DiscussForum
}

// DiscussPost 讨论帖
type DiscussPost struct {
	ID      int         `json:"id"`
	Title   string      `json:"title"`
	Author  UserSummary `json:"author"`
	Time    int64       `json:"time"`
	Content string      `json:"content"`
	Topped  bool        `json:"topped"`
	Valid   bool        `json:"valid"`
	Locked  bool        `json:"locked"`
}

// DiscussReply 讨论回帖
type DiscussReply struct {
	ID      int         `json:"id"`
	Author  UserSummary `json:"author"`
	Time    int64       `json:"time"`
	Content string      `json:"content"`
}

// DiscussReplies 回帖列表（API 包装）
type DiscussReplies struct {
	Result []DiscussReply `json:"result"`
	Count  int            `json:"count"`
}

// --- 比赛 (Contest) ---

// ContestList 比赛列表
type ContestList struct {
	Contests []ContestSummary
	Count    int
}

// ContestSummary 比赛摘要
type ContestSummary struct {
	ID                 int         `json:"id"`
	Name               string      `json:"name"`
	StartTime          int64       `json:"startTime"`
	EndTime            int64       `json:"endTime"`
	Method             int         `json:"method"`
	Visibility         int         `json:"visibility"`
	InvitationCodeType int         `json:"invitationCodeType"`
	Rated              int         `json:"rated"`
	Host               ContestHost `json:"host"`
	ProblemCount       int         `json:"problemCount"`
}

// ContestHost 比赛主办方
type ContestHost struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	IsPremium bool   `json:"isPremium"`
}

// ContestDetail 比赛详情
type ContestDetail struct {
	ContestSummary
	Joined            int    `json:"joined"`
	Description       string `json:"description"`
	RuleType          int    `json:"ruleType"`
	Difficulty        int    `json:"difficulty"`
	TotalParticipants int    `json:"totalParticipants"`
}
