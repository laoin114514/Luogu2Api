package luoguclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 实测抓自 https://www.luogu.com.cn/user/setting/preference（账号 laoin，2026-09）
const preferenceContextJSON = `{"instance":"main","template":"user.setting","status":200,"locale":"zh-CN",` +
	`"data":{"openSourceJoinTime":1789556739,"setting":{"codeFont":null,"colorScheme":null,` +
	`"openSource":1,"codeSharingWithAi":false,"learningMode":true,"messageMode":2,"acceptPromotion":true}}}`

func TestUserGetPreference(t *testing.T) {
	srv := serveLentille(t, preferenceContextJSON)
	c := newTestClient(t, srv)

	pref, err := c.User.GetPreference()
	if err != nil {
		t.Fatalf("GetPreference: %v", err)
	}

	if pref.OpenSource != OpenSourceEnabled {
		t.Errorf("OpenSource = %d, want %d", pref.OpenSource, OpenSourceEnabled)
	}
	if pref.CodeSharingWithAi {
		t.Error("CodeSharingWithAi = true, want false")
	}
	if !pref.LearningMode {
		t.Error("LearningMode = false, want true")
	}
	if pref.MessageMode != MessageReceiveAnyone {
		t.Errorf("MessageMode = %d, want %d", pref.MessageMode, MessageReceiveAnyone)
	}
	if !pref.AcceptPromotion {
		t.Error("AcceptPromotion = false, want true")
	}
	if pref.CodeFont != nil {
		t.Errorf("CodeFont = %v, want nil", *pref.CodeFont)
	}
	if pref.ColorScheme != nil {
		t.Errorf("ColorScheme = %v, want nil", *pref.ColorScheme)
	}
	if pref.OpenSourceJoinTime != 1789556739 {
		t.Errorf("OpenSourceJoinTime = %d, want 1789556739", pref.OpenSourceJoinTime)
	}
}

// 未登录访问设置页：洛谷 302 → /login，SDK 跟随重定向后应报 *UnauthorizedError 而不是解析失败
func TestUserGetPreferenceUnauthorizedOnLoginRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/login") {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte("<!DOCTYPE html><html><body>login</body></html>"))
			return
		}
		http.Redirect(w, r, "/login?redirect=%2Fuser%2Fsetting%2Fpreference", http.StatusFound)
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv)

	_, err := c.User.GetPreference()
	var unauthorized *UnauthorizedError
	if !errors.As(err, &unauthorized) {
		t.Fatalf("GetPreference error = %v, want *UnauthorizedError", err)
	}
}

func TestUserGetPreferenceUnauthorizedOn401(t *testing.T) {
	srv := serveStatus(t, http.StatusUnauthorized)
	c := newTestClient(t, srv)

	_, err := c.User.GetPreference()
	var unauthorized *UnauthorizedError
	if !errors.As(err, &unauthorized) {
		t.Fatalf("GetPreference error = %v, want *UnauthorizedError", err)
	}
	if unauthorized.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want 401", unauthorized.StatusCode)
	}
}

// UpdatePreference 必须发送完整对象，且不能把只读的 openSourceJoinTime 带上去
func TestUserUpdatePreferenceSendsFullObject(t *testing.T) {
	var gotBody map[string]interface{}
	var gotMethod, gotPath, gotContentType string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")

		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &gotBody); err != nil {
			t.Errorf("请求体不是 JSON: %v (%s)", err, raw)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"setting":{"codeFont":null,"colorScheme":null,"openSource":1,` +
			`"codeSharingWithAi":false,"learningMode":false,"messageMode":1,"acceptPromotion":false}}`))
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv)

	sent := UserPreference{
		OpenSource:        OpenSourceEnabled,
		CodeSharingWithAi: false,
		LearningMode:      false,
		MessageMode:       MessageReceiveFollowing,
		AcceptPromotion:   false,
		// 只读字段：不应出现在请求体里
		OpenSourceJoinTime: 1789556739,
	}

	updated, err := c.User.UpdatePreference(sent)
	if err != nil {
		t.Fatalf("UpdatePreference: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if gotPath != "/user/setting/preference/update" {
		t.Errorf("path = %s, want /user/setting/preference/update", gotPath)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %s, want application/json", gotContentType)
	}

	// 7 个可写字段必须全部出现（服务端对省略字段套用默认值）
	for _, key := range []string{
		"codeFont", "colorScheme", "openSource",
		"codeSharingWithAi", "learningMode", "messageMode", "acceptPromotion",
	} {
		if _, ok := gotBody[key]; !ok {
			t.Errorf("请求体缺少字段 %q（服务端会把它重置成默认值）: %v", key, gotBody)
		}
	}
	if len(gotBody) != 7 {
		t.Errorf("请求体字段数 = %d, want 7: %v", len(gotBody), gotBody)
	}
	if _, ok := gotBody["openSourceJoinTime"]; ok {
		t.Error("请求体不应包含只读字段 openSourceJoinTime")
	}

	// 响应解析
	if updated == nil {
		t.Fatal("UpdatePreference 返回 nil")
	}
	if updated.LearningMode {
		t.Error("LearningMode = true, want false")
	}
	if updated.MessageMode != MessageReceiveFollowing {
		t.Errorf("MessageMode = %d, want %d", updated.MessageMode, MessageReceiveFollowing)
	}
	if updated.AcceptPromotion {
		t.Error("AcceptPromotion = true, want false")
	}
}

// 业务规则拒绝（HTTP 400 + 洛谷错误体）应映射成 *APIError，保留文案
func TestUserUpdatePreferenceBusinessError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errorCode":400,` +
			`"errorType":"Symfony\\Component\\HttpKernel\\Exception\\BadRequestHttpException",` +
			`"errorMessage":"加入代码公开计划未满 30 天，不能退出","errorData":{":":0}}`))
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv)

	_, err := c.User.UpdatePreference(UserPreference{OpenSource: OpenSourcePrivacyProtection})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("UpdatePreference error = %v, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("StatusCode = %d, want 400", apiErr.StatusCode)
	}
	if apiErr.Code != 400 {
		t.Errorf("Code = %d, want 400", apiErr.Code)
	}
	if !strings.Contains(apiErr.Type, "BadRequestHttpException") {
		t.Errorf("Type = %q, want 含 BadRequestHttpException", apiErr.Type)
	}
	if apiErr.Message != "加入代码公开计划未满 30 天，不能退出" {
		t.Errorf("Message = %q", apiErr.Message)
	}
	if !strings.Contains(apiErr.Error(), "加入代码公开计划未满 30 天，不能退出") {
		t.Errorf("Error() = %q", apiErr.Error())
	}
}

// 401/403 仍走 *UnauthorizedError，不能退化成 APIError
func TestUserUpdatePreferenceUnauthorized(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		srv := serveStatus(t, status)
		c := newTestClient(t, srv)

		_, err := c.User.UpdatePreference(UserPreference{})
		var unauthorized *UnauthorizedError
		if !errors.As(err, &unauthorized) {
			t.Fatalf("status %d: error = %v, want *UnauthorizedError", status, err)
		}
		if unauthorized.StatusCode != status {
			t.Errorf("StatusCode = %d, want %d", unauthorized.StatusCode, status)
		}
	}
}

// JoinOpenSourcePlan 必须"读-改-写"：只改 openSource，其它偏好原样保留
// （直接发 {"openSource":1} 会让服务端把省略字段重置成默认值）
func TestUserJoinOpenSourcePlanReadModifyWrite(t *testing.T) {
	var posts []map[string]interface{}
	gets := 0
	joined := false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			gets++
			openSource, joinTime := 0, 0
			if joined {
				openSource, joinTime = 1, 1789556739
			}
			body := fmt.Sprintf(`{"instance":"main","data":{"openSourceJoinTime":%d,"setting":`+
				`{"codeFont":null,"colorScheme":null,"openSource":%d,"codeSharingWithAi":false,`+
				`"learningMode":true,"messageMode":2,"acceptPromotion":true}}}`, joinTime, openSource)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(lentilleHTML(body)))
			return
		}

		raw, _ := io.ReadAll(r.Body)
		var sent map[string]interface{}
		_ = json.Unmarshal(raw, &sent)
		posts = append(posts, sent)
		joined = true

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"setting":` + string(raw) + `}`))
	}))
	t.Cleanup(srv.Close)

	joinTime, err := newTestClient(t, srv).User.JoinOpenSourcePlan()
	if err != nil {
		t.Fatalf("JoinOpenSourcePlan: %v", err)
	}

	if joinTime != 1789556739 {
		t.Errorf("joinTime = %d, want 1789556739（应为洛谷落库后的加入时间）", joinTime)
	}
	if len(posts) != 1 {
		t.Fatalf("应只写一次，实际 %d 次", len(posts))
	}
	if gets != 2 {
		t.Errorf("GET 次数 = %d, want 2（读偏好 + 写后确认）", gets)
	}

	sent := posts[0]
	if sent["openSource"] != float64(1) {
		t.Errorf("openSource = %v, want 1", sent["openSource"])
	}
	if len(sent) != 7 {
		t.Errorf("请求体字段数 = %d, want 7（必须整份回写）: %v", len(sent), sent)
	}
	// 这几项是"被省略就会被重置成默认值"的字段：必须原样带回，而不是缺席
	for key, want := range map[string]interface{}{
		"codeSharingWithAi": false, // 平台默认是 true
		"learningMode":      true,  // 平台默认是 false
		"messageMode":       float64(2),
		"acceptPromotion":   true,
		"codeFont":          nil,
		"colorScheme":       nil,
	} {
		got, ok := sent[key]
		if !ok || got != want {
			t.Errorf("字段 %s = %v (存在=%v), want %v", key, got, ok, want)
		}
	}
}

// 远端已是 1 时只读不写：不产生任何写请求
func TestUserJoinOpenSourcePlanAlreadyJoined(t *testing.T) {
	gets, posts := 0, 0

	body := `{"instance":"main","data":{"openSourceJoinTime":1789556739,"setting":` +
		`{"codeFont":null,"colorScheme":null,"openSource":1,"codeSharingWithAi":false,` +
		`"learningMode":true,"messageMode":2,"acceptPromotion":true}}}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			gets++
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(lentilleHTML(body)))
			return
		}
		posts++
	}))
	t.Cleanup(srv.Close)

	joinTime, err := newTestClient(t, srv).User.JoinOpenSourcePlan()
	if err != nil {
		t.Fatalf("JoinOpenSourcePlan: %v", err)
	}
	if joinTime != 1789556739 {
		t.Errorf("joinTime = %d, want 1789556739", joinTime)
	}
	if posts != 0 {
		t.Errorf("已加入时不应写，实际写了 %d 次", posts)
	}
	if gets != 1 {
		t.Errorf("GET 次数 = %d, want 1", gets)
	}
}

// 写入后复核发现没生效：必须报错，不能只看 HTTP 200
func TestUserJoinOpenSourcePlanWriteNotEffective(t *testing.T) {
	body := `{"instance":"main","data":{"openSourceJoinTime":0,"setting":` +
		`{"codeFont":null,"colorScheme":null,"openSource":0,"codeSharingWithAi":true,` +
		`"learningMode":false,"messageMode":2,"acceptPromotion":true}}}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(lentilleHTML(body)))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"setting":{"codeFont":null,"colorScheme":null,"openSource":0,` +
			`"codeSharingWithAi":true,"learningMode":false,"messageMode":2,"acceptPromotion":true}}`))
	}))
	t.Cleanup(srv.Close)

	_, err := newTestClient(t, srv).User.JoinOpenSourcePlan()
	if err == nil {
		t.Fatal("写入未生效时应返回错误")
	}
	if !strings.Contains(err.Error(), "未生效") {
		t.Errorf("error = %v, want 含 '未生效'", err)
	}
}

// 幂等：把 GetPreference 的结果原样回写，请求体应与读取到的设置一致
func TestUserPreferenceRoundTrip(t *testing.T) {
	var gotBody map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(lentilleHTML(preferenceContextJSON)))
			return
		}

		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"setting":` + string(raw) + `}`))
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv)

	pref, err := c.User.GetPreference()
	if err != nil {
		t.Fatalf("GetPreference: %v", err)
	}
	pref.OpenSourceJoinTime = 0 // 只读字段，回写时本来就不发送

	updated, err := c.User.UpdatePreference(*pref)
	if err != nil {
		t.Fatalf("UpdatePreference: %v", err)
	}

	for key, want := range map[string]interface{}{
		"openSource":        float64(1),
		"codeSharingWithAi": false,
		"learningMode":      true,
		"messageMode":       float64(2),
		"acceptPromotion":   true,
		"codeFont":          nil,
		"colorScheme":       nil,
	} {
		if got, ok := gotBody[key]; !ok || got != want {
			t.Errorf("回写字段 %s = %v (存在=%v), want %v", key, got, ok, want)
		}
	}

	if updated.OpenSource != OpenSourceEnabled || !updated.LearningMode || updated.MessageMode != MessageReceiveAnyone {
		t.Errorf("往返后设置不一致: %+v", *updated)
	}
}
