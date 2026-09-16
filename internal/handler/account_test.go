package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/laoin114514/luogu2api/internal/model"
	"github.com/laoin114514/luogu2api/internal/response"
	"github.com/laoin114514/luogu2api/internal/service"
)

// stubAccountAdmin 账号管理替身
type stubAccountAdmin struct {
	accounts []service.AccountDTO
	account  service.AccountDTO
	err      error

	created  []string
	enabled  map[uint]bool
	deleted  []uint
	relogins []uint
}

func (s *stubAccountAdmin) Create(_ context.Context, username, password, nickname string) (service.AccountDTO, error) {
	s.created = append(s.created, username)
	if s.err != nil {
		return service.AccountDTO{}, s.err
	}
	return s.account, nil
}

func (s *stubAccountAdmin) Get(context.Context, uint) (service.AccountDTO, error) {
	if s.err != nil {
		return service.AccountDTO{}, s.err
	}
	return s.account, nil
}

func (s *stubAccountAdmin) List(context.Context) ([]service.AccountDTO, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.accounts, nil
}

func (s *stubAccountAdmin) SetEnabled(_ context.Context, id uint, enabled bool) (service.AccountDTO, error) {
	if s.enabled == nil {
		s.enabled = map[uint]bool{}
	}
	s.enabled[id] = enabled
	if s.err != nil {
		return service.AccountDTO{}, s.err
	}
	return s.account, nil
}

func (s *stubAccountAdmin) Delete(_ context.Context, id uint) error {
	s.deleted = append(s.deleted, id)
	return s.err
}

func (s *stubAccountAdmin) Relogin(_ context.Context, id uint) (service.AccountDTO, error) {
	s.relogins = append(s.relogins, id)
	if s.err != nil {
		return service.AccountDTO{}, s.err
	}
	return s.account, nil
}

func newAccountEngine(admin AccountAdmin) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewAccountHandler(admin)
	r.GET("/accounts", h.List)
	r.POST("/accounts", h.Create)
	r.GET("/accounts/:id", h.Get)
	r.PATCH("/accounts/:id", h.Update)
	r.DELETE("/accounts/:id", h.Delete)
	r.POST("/accounts/:id/relogin", h.Relogin)
	return r
}

func doRequest(t *testing.T, engine *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()

	var reader *bytes.Reader
	if body == "" {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader([]byte(body))
	}

	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func sampleDTO() service.AccountDTO {
	next := time.Date(2026, 3, 1, 12, 10, 0, 0, time.UTC)
	return service.AccountDTO{
		ID:           1,
		Username:     "user1",
		LuoguUID:     1965145,
		Nickname:     "昵称",
		Online:       true,
		Status:       model.AccountStatusActive,
		Enabled:      true,
		Weight:       1,
		Name:         "昵称",
		NextVerifyAt: &next,
	}
}

func TestAccountListOK(t *testing.T) {
	engine := newAccountEngine(&stubAccountAdmin{accounts: []service.AccountDTO{sampleDTO()}})

	rec := doRequest(t, engine, http.MethodGet, "/accounts", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "user1") || !strings.Contains(rec.Body.String(), "1965145") {
		t.Errorf("body = %s", rec.Body.String())
	}
}

// 管理接口也不能泄漏凭据
func TestAccountListLeaksNoCredentials(t *testing.T) {
	dto := sampleDTO()
	raw, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	for _, forbidden := range []string{"password", "cookie", "cookieEnc", "passwordEnc"} {
		if strings.Contains(strings.ToLower(string(raw)), strings.ToLower(forbidden)) {
			t.Errorf("账号响应里出现敏感字段 %q: %s", forbidden, raw)
		}
	}
}

func TestAccountCreateOK(t *testing.T) {
	admin := &stubAccountAdmin{account: sampleDTO()}
	engine := newAccountEngine(admin)

	rec := doRequest(t, engine, http.MethodPost, "/accounts", `{"username":"user1","password":"pwd","nickname":"昵称"}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body=%s)", rec.Code, rec.Body.String())
	}
	if len(admin.created) != 1 || admin.created[0] != "user1" {
		t.Errorf("created = %v", admin.created)
	}
}

func TestAccountCreateRejectsBadJSON(t *testing.T) {
	engine := newAccountEngine(&stubAccountAdmin{})

	rec := doRequest(t, engine, http.MethodPost, "/accounts", `{"username":`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if body := decodeBody(t, rec); body.Code != response.CodeInvalidParam {
		t.Errorf("code = %d", body.Code)
	}
}

func TestAccountCreateMapsDuplicateTo409(t *testing.T) {
	engine := newAccountEngine(&stubAccountAdmin{err: model.ErrAccountExists})

	rec := doRequest(t, engine, http.MethodPost, "/accounts", `{"username":"user1","password":"pwd"}`)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}

func TestAccountGetRejectsBadID(t *testing.T) {
	engine := newAccountEngine(&stubAccountAdmin{account: sampleDTO()})

	for _, id := range []string{"abc", "0", "-1"} {
		rec := doRequest(t, engine, http.MethodGet, "/accounts/"+id, "")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("id=%s status = %d, want 400", id, rec.Code)
		}
	}
}

func TestAccountGetNotFound(t *testing.T) {
	engine := newAccountEngine(&stubAccountAdmin{err: model.ErrAccountNotFound})

	rec := doRequest(t, engine, http.MethodGet, "/accounts/404", "")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if body := decodeBody(t, rec); body.Code != response.CodeNotFound {
		t.Errorf("code = %d", body.Code)
	}
}

func TestAccountUpdateRequiresEnabled(t *testing.T) {
	engine := newAccountEngine(&stubAccountAdmin{account: sampleDTO()})

	rec := doRequest(t, engine, http.MethodPatch, "/accounts/1", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺 enabled 时应 400，得到 %d", rec.Code)
	}

	admin := &stubAccountAdmin{account: sampleDTO()}
	rec = doRequest(t, newAccountEngine(admin), http.MethodPatch, "/accounts/7", `{"enabled":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body=%s)", rec.Code, rec.Body.String())
	}
	if got, ok := admin.enabled[7]; !ok || got {
		t.Errorf("enabled = %v", admin.enabled)
	}
}

func TestAccountDeleteAndRelogin(t *testing.T) {
	admin := &stubAccountAdmin{account: sampleDTO()}
	engine := newAccountEngine(admin)

	rec := doRequest(t, engine, http.MethodDelete, "/accounts/3", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status = %d (body=%s)", rec.Code, rec.Body.String())
	}
	if len(admin.deleted) != 1 || admin.deleted[0] != 3 {
		t.Errorf("deleted = %v", admin.deleted)
	}

	rec = doRequest(t, engine, http.MethodPost, "/accounts/3/relogin", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("relogin status = %d (body=%s)", rec.Code, rec.Body.String())
	}
	if len(admin.relogins) != 1 || admin.relogins[0] != 3 {
		t.Errorf("relogins = %v", admin.relogins)
	}
}

func TestAccountCreatePropagatesPoolUnavailable(t *testing.T) {
	engine := newAccountEngine(&stubAccountAdmin{err: service.ErrPoolUnavailable})

	rec := doRequest(t, engine, http.MethodPost, "/accounts", `{"username":"u","password":"p"}`)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if body := decodeBody(t, rec); body.Code != response.CodePoolExhausted {
		t.Errorf("code = %d", body.Code)
	}
}
