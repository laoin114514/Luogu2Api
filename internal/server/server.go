// Package server 组装 2Api 的 HTTP 路由与处理器。
package server

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	luogu "github.com/laoin114514/luoguClient"
)

// ProblemFetcher 取题目详情。
//
// *luogu.Client 通过 SDKClient 适配；测试时可替换成桩实现。
type ProblemFetcher interface {
	Problem(pid string) (*luogu.Problem, error)
}

// SDKClient 把洛谷客户端适配成 ProblemFetcher
type SDKClient struct {
	Client *luogu.Client
}

// Problem 实现 ProblemFetcher
func (s SDKClient) Problem(pid string) (*luogu.Problem, error) {
	return s.Client.Problem.Get(pid)
}

// New 返回服务的 HTTP 处理器
func New(problems ProblemFetcher, logger *log.Logger) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// 匿名即可访问的示例接口：GET /api/problem/P1001
	mux.HandleFunc("GET /api/problem/{pid}", problemHandler(problems, logger))

	return mux
}

// ProblemDTO 对外返回的题目精简结构
type ProblemDTO struct {
	PID         string     `json:"pid"`
	Title       string     `json:"title"`
	Difficulty  int        `json:"difficulty"`
	Tags        []int      `json:"tags"`
	TimeLimit   int        `json:"timeLimitMs"`
	MemoryLimit int        `json:"memoryLimitKb"`
	Samples     [][]string `json:"samples"`
}

func problemHandler(problems ProblemFetcher, logger *log.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pid := r.PathValue("pid")

		problem, err := problems.Problem(pid)
		if err != nil {
			status, message := errorStatus(err)
			logger.Printf("取题目 %s 失败: %v", pid, err)
			writeJSON(w, status, map[string]string{"error": message})
			return
		}

		writeJSON(w, http.StatusOK, ProblemDTO{
			PID:         problem.PID,
			Title:       problem.Title,
			Difficulty:  problem.Difficulty,
			Tags:        problem.Tags,
			TimeLimit:   problem.TimeLimit(),
			MemoryLimit: problem.MemoryLimit(),
			Samples:     problem.Samples,
		})
	}
}

// errorStatus 把 SDK 错误映射成 HTTP 状态码。
//
// 注意：SDK 目前只有 *luogu.UnauthorizedError 是类型化错误，404 只能靠错误消息判断；
// 后续可在 SDK 中补 NotFoundError，这里就能改成类型判断。
func errorStatus(err error) (int, string) {
	var unauthorized *luogu.UnauthorizedError
	switch {
	case errors.As(err, &unauthorized):
		return http.StatusUnauthorized, "需要登录洛谷账号"
	case strings.Contains(err.Error(), "status 404"):
		return http.StatusNotFound, "题目不存在"
	default:
		return http.StatusBadGateway, "上游（洛谷）请求失败: " + err.Error()
	}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
