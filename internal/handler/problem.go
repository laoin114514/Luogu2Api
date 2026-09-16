package handler

import (
	"context"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/laoin114514/luogu2api/internal/client"
	"github.com/laoin114514/luogu2api/internal/response"
)

// ProblemReader 题目读取能力（由 service.ProblemService 实现）
type ProblemReader interface {
	Get(ctx context.Context, pid string) (*client.Problem, error)
	Search(ctx context.Context, keyword string, page, pageSize int) (*client.SearchResult, error)
}

// ProblemHandler 题目接口处理器
type ProblemHandler struct {
	problems ProblemReader
}

// NewProblemHandler 创建题目处理器
func NewProblemHandler(problems ProblemReader) *ProblemHandler {
	return &ProblemHandler{problems: problems}
}

// Get 处理 GET /api/v1/problems/:pid
//
// 该账号的 cookie 失效时号池会自动换号重试；号池没有可用账号则返回 503。
func (h *ProblemHandler) Get(c *gin.Context) {
	problem, err := h.problems.Get(c.Request.Context(), c.Param("pid"))
	if err != nil {
		Fail(c, err)
		return
	}
	response.OK(c, problem)
}

// Search 处理 GET /api/v1/problems?keyword=&page=&pageSize=
func (h *ProblemHandler) Search(c *gin.Context) {
	result, err := h.problems.Search(
		c.Request.Context(),
		c.Query("keyword"),
		atoiDefault(c.Query("page"), 1),
		atoiDefault(c.Query("pageSize"), 20),
	)
	if err != nil {
		Fail(c, err)
		return
	}
	response.OK(c, result)
}

func atoiDefault(raw string, def int) int {
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return n
}
