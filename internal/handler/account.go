package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/laoin114514/luogu2api/internal/response"
	"github.com/laoin114514/luogu2api/internal/service"
)

// AccountAdmin 账号管理能力（由 service.AccountService 实现）
type AccountAdmin interface {
	Create(ctx context.Context, username, password, nickname string) (service.AccountDTO, error)
	Get(ctx context.Context, id uint) (service.AccountDTO, error)
	List(ctx context.Context) ([]service.AccountDTO, error)
	SetEnabled(ctx context.Context, id uint, enabled bool) (service.AccountDTO, error)
	UpdatePassword(ctx context.Context, id uint, password string) (service.AccountDTO, error)
	Delete(ctx context.Context, id uint) error
	Relogin(ctx context.Context, id uint) (service.AccountDTO, error)
}

// AccountHandler 账号（号池）管理接口处理器。
//
// 全部路由都挂在需要 X-Admin-Token 的分组下（见 router）；响应体是 DTO，
// 永远不含 password / cookie。
type AccountHandler struct {
	accounts AccountAdmin
}

// NewAccountHandler 创建账号管理处理器
func NewAccountHandler(accounts AccountAdmin) *AccountHandler {
	return &AccountHandler{accounts: accounts}
}

type createAccountRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Nickname string `json:"nickname"`
}

type updateAccountRequest struct {
	Enabled *bool `json:"enabled"`
}

type updatePasswordRequest struct {
	Password string `json:"password"`
}

// List 处理 GET /api/v1/admin/accounts
func (h *AccountHandler) List(c *gin.Context) {
	accounts, err := h.accounts.List(c.Request.Context())
	if err != nil {
		Fail(c, err)
		return
	}
	response.OK(c, accounts)
}

// Create 处理 POST /api/v1/admin/accounts
func (h *AccountHandler) Create(c *gin.Context) {
	var req createAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailCode(c, http.StatusBadRequest, response.CodeInvalidParam, "请求体不是合法 JSON")
		return
	}

	acc, err := h.accounts.Create(c.Request.Context(), req.Username, req.Password, req.Nickname)
	if err != nil {
		Fail(c, err)
		return
	}
	response.JSON(c, http.StatusCreated, response.CodeOK, "ok", acc)
}

// Get 处理 GET /api/v1/admin/accounts/:id
func (h *AccountHandler) Get(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	acc, err := h.accounts.Get(c.Request.Context(), id)
	if err != nil {
		Fail(c, err)
		return
	}
	response.OK(c, acc)
}

// Update 处理 PATCH /api/v1/admin/accounts/:id（当前只支持启停）
func (h *AccountHandler) Update(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	var req updateAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailCode(c, http.StatusBadRequest, response.CodeInvalidParam, "请求体不是合法 JSON")
		return
	}
	if req.Enabled == nil {
		response.FailCode(c, http.StatusBadRequest, response.CodeInvalidParam, "enabled 不能为空")
		return
	}

	acc, err := h.accounts.SetEnabled(c.Request.Context(), id, *req.Enabled)
	if err != nil {
		Fail(c, err)
		return
	}
	response.OK(c, acc)
}

// UpdatePassword 处理 PUT /api/v1/admin/accounts/:id/password
func (h *AccountHandler) UpdatePassword(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	var req updatePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailCode(c, http.StatusBadRequest, response.CodeInvalidParam, "请求体不是合法 JSON")
		return
	}
	if req.Password == "" {
		response.FailCode(c, http.StatusBadRequest, response.CodeInvalidParam, "password 不能为空")
		return
	}

	acc, err := h.accounts.UpdatePassword(c.Request.Context(), id, req.Password)
	if err != nil {
		Fail(c, err)
		return
	}
	response.OK(c, acc)
}

// Delete 处理 DELETE /api/v1/admin/accounts/:id
func (h *AccountHandler) Delete(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	if err := h.accounts.Delete(c.Request.Context(), id); err != nil {
		Fail(c, err)
		return
	}
	response.OK(c, gin.H{"id": id, "deleted": true})
}

// Relogin 处理 POST /api/v1/admin/accounts/:id/relogin
func (h *AccountHandler) Relogin(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	acc, err := h.accounts.Relogin(c.Request.Context(), id)
	if err != nil {
		Fail(c, err)
		return
	}
	response.OK(c, acc)
}

func parseID(c *gin.Context) (uint, bool) {
	raw := c.Param("id")
	n, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || n == 0 {
		response.FailCode(c, http.StatusBadRequest, response.CodeInvalidParam, "id 必须是正整数")
		return 0, false
	}
	return uint(n), true
}
