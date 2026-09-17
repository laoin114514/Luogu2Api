package handler

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/laoin114514/luogu2api/internal/response"
	"github.com/laoin114514/luogu2api/internal/service"
)

// RecordReader 提交记录读取能力（由 service.RecordService 实现）
type RecordReader interface {
	ListByUser(ctx context.Context, uid int, pid string, status, page int) (*service.RecordListDTO, error)
}

// RecordHandler 提交记录接口处理器
type RecordHandler struct {
	records RecordReader
}

// NewRecordHandler 创建提交记录处理器
func NewRecordHandler(records RecordReader) *RecordHandler {
	return &RecordHandler{records: records}
}

// ListByUser 处理 GET /api/v1/users/:uid/records?pid=&status=&page=
//
// 记录接口需要登录态，走号池选号；cookie 失效时号池会自动换号重试，
// 号池没有可用账号则返回 503（业务码 1001）。
func (h *RecordHandler) ListByUser(c *gin.Context) {
	uid, ok := parseUID(c)
	if !ok {
		return
	}

	status, ok := parseStatus(c)
	if !ok {
		return
	}

	result, err := h.records.ListByUser(
		c.Request.Context(),
		uid,
		c.Query("pid"),
		status,
		atoiDefault(c.Query("page"), 1),
	)
	if err != nil {
		Fail(c, err)
		return
	}
	response.OK(c, result)
}

// parseUID 解析路径参数 uid（洛谷用户 UID），非正整数直接 400
func parseUID(c *gin.Context) (int, bool) {
	n, err := strconv.ParseInt(c.Param("uid"), 10, 64)
	if err != nil || n <= 0 || n > math.MaxInt32 {
		response.FailCode(c, http.StatusBadRequest, response.CodeInvalidParam, "uid 必须是正整数")
		return 0, false
	}
	return int(n), true
}

// parseStatus 解析可选的 status 过滤（0 或省略表示全部）。
//
// 与 page 的"非法值退回默认值"不同：过滤条件写错时静默当"不过滤"会返回一份
// 看似正常、实际不是调用方要的数据，所以非法值必须显式报 400。
func parseStatus(c *gin.Context) (int, bool) {
	raw := strings.TrimSpace(c.Query("status"))
	if raw == "" {
		return 0, true
	}

	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		response.FailCode(c, http.StatusBadRequest, response.CodeInvalidParam, "status 必须是非负整数")
		return 0, false
	}
	return n, true
}
