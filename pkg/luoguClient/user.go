package luoguclient

import (
	"fmt"
	"net/http"
	"strings"
)

// UserService 用户服务
type UserService struct {
	client *Client
}

// Get 获取用户详情
func (u *UserService) Get(uid int) (*UserDetail, error) {
	path := fmt.Sprintf("/user/%d", uid)
	resp, err := u.client.get(path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := checkResponse(resp, "get user %d", uid); err != nil {
		return nil, err
	}

	var result struct {
		Data struct {
			User UserDetail `json:"user"`
		} `json:"data"`
	}
	if err := parseLentilleContext(resp, &result); err != nil {
		return nil, err
	}
	return &result.Data.User, nil
}

// GetRanking 获取排名列表
func (u *UserService) GetRanking(page int) (*RankingList, error) {
	if page <= 0 {
		page = 1
	}
	path := fmt.Sprintf("/ranking?page=%d", page)
	resp, err := u.client.get(path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := checkResponse(resp, "get ranking"); err != nil {
		return nil, err
	}

	var result struct {
		Data struct {
			Ranking struct {
				Result  []RankingItem `json:"result"`
				Count   int           `json:"count"`
				PerPage int           `json:"perPage"`
			} `json:"ranking"`
		} `json:"data"`
	}
	if err := parseLentilleContext(resp, &result); err != nil {
		return nil, err
	}
	return &RankingList{
		Items: result.Data.Ranking.Result,
		Count: result.Data.Ranking.Count,
	}, nil
}

// GetPreference 获取当前账号的偏好设置（需要已登录）
//
// 数据来自设置页 /user/setting/preference 内嵌的 lentille-context（data.setting），
// 返回的 UserPreference 是可直接回写给 UpdatePreference 的完整对象。
//
// 未登录时洛谷返回 302 → /login，这里统一转成 *UnauthorizedError。
func (u *UserService) GetPreference() (*UserPreference, error) {
	resp, err := u.client.get("/user/setting/preference")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := checkPreferenceResponse(resp, "get user preference"); err != nil {
		return nil, err
	}

	var result struct {
		Data struct {
			OpenSourceJoinTime int64          `json:"openSourceJoinTime"`
			Setting            UserPreference `json:"setting"`
		} `json:"data"`
	}
	if err := parseLentilleContext(resp, &result); err != nil {
		return nil, err
	}

	pref := result.Data.Setting
	pref.OpenSourceJoinTime = result.Data.OpenSourceJoinTime
	return &pref, nil
}

// UpdatePreference 更新偏好设置，返回服务端落库后的完整设置（需要已登录）
//
// 注意：洛谷对**省略的字段套用平台默认值**，而不是保持原值——例如只发
// {"learningMode":true} 会把 openSource 重置成默认的 1（加入代码公开计划）、
// codeSharingWithAi 重置成默认的 true。因此本方法总是序列化完整对象，
// 并且推荐"先读后写"：
//
//	pref, err := client.User.GetPreference()
//	if err != nil {
//		return err
//	}
//	pref.AcceptPromotion = false
//	updated, err := client.User.UpdatePreference(*pref)
//
// 手工构造 UserPreference 时，零值不等于平台默认值：OpenSource 零值是 0
// （不公开代码）、MessageMode 零值是 0（仅限管理员）、LearningMode 零值是 false。
//
// 本接口实测不校验 X-CSRF-TOKEN（无 token、伪造 token 均返回 200），只要 cookie 有效即可；
// 业务规则拒绝（如"加入代码公开计划未满 30 天，不能退出"）返回 HTTP 400，错误类型是 *APIError。
func (u *UserService) UpdatePreference(pref UserPreference) (*UserPreference, error) {
	resp, err := u.client.post("/user/setting/preference/update", pref)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := checkPreferenceResponse(resp, "update user preference"); err != nil {
		return nil, err
	}

	var result struct {
		Setting UserPreference `json:"setting"`
	}
	if err := parseBody(resp, &result); err != nil {
		return nil, err
	}
	return &result.Setting, nil
}

// JoinOpenSourcePlan 确保当前账号已加入洛谷"代码公开计划"（openSource=1），幂等。
//
// 这是 GetPreference + UpdatePreference 的安全组合，用来避免直接调 UpdatePreference
// 时最常见的误用：偏好更新是**全量替换**语义，只发 {"openSource":1} 会把
// codeSharingWithAi 重置成平台默认的 true、learningMode 重置成 false。这里先读回
// 整份偏好、只改 openSource，再整份回写，其它字段原样保留。
//
// 返回洛谷记录的加入时间（Unix 秒，未加入时为 0）。全程最多 3 次请求：
// 读偏好 →（未加入时）写偏好 → 再读一次确认。
//
// 注意这是**不可逆动作**：洛谷限制加入后 30 天内不能退出（把 openSource 改回
// 0/-1 会返回 HTTP 400），因此调用方应当只在明确需要时调用，并按"一生一次"的
// 幂等语义使用——远端已加入时本方法只读不写。
func (u *UserService) JoinOpenSourcePlan() (int64, error) {
	pref, err := u.GetPreference()
	if err != nil {
		return 0, err
	}
	if pref.OpenSource == OpenSourceEnabled {
		// 远端已加入（例如人工在网页上开过）：不重复写，直接采用洛谷记录的时间
		return pref.OpenSourceJoinTime, nil
	}

	pref.OpenSource = OpenSourceEnabled
	if _, err := u.UpdatePreference(*pref); err != nil {
		return 0, err
	}

	// 更新响应里不含 openSourceJoinTime（它是 setting 的兄弟字段，不在 setting 内），
	// 而"加入时间 + 30 天"才是真正有意义的解锁基准，值得多这一次读；
	// 何况"落库后的值"也必须复核——HTTP 200 不代表业务生效。
	confirmed, err := u.GetPreference()
	if err != nil {
		return 0, fmt.Errorf("已提交加入代码公开计划但确认读取失败: %w", err)
	}
	if confirmed.OpenSource != OpenSourceEnabled {
		return 0, fmt.Errorf("加入代码公开计划未生效：openSource=%d", confirmed.OpenSource)
	}
	return confirmed.OpenSourceJoinTime, nil
}

// checkPreferenceResponse 偏好接口的错误归一化。
//
// 与 checkResponse 相同的 401/403 → *UnauthorizedError 规则，另外两点：
//   - 未登录访问设置页时洛谷返回 302 → /login，SDK 会跟随重定向拿到登录页（200），
//     因此还要看最终落点是不是登录页；
//   - 其余非 200 用 *APIError 保留洛谷的业务错误文案（checkResponse 只会给出状态码）。
func checkPreferenceResponse(resp *http.Response, format string, args ...interface{}) error {
	op := fmt.Sprintf(format, args...)

	if resp.StatusCode == http.StatusOK {
		// 兼容 302→登录页：跟随重定向后状态码是 200，只能看最终 URL
		if resp.Request != nil && strings.Contains(resp.Request.URL.Path, "/login") {
			return &UnauthorizedError{StatusCode: http.StatusFound, Message: op}
		}
		return nil
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return &UnauthorizedError{StatusCode: resp.StatusCode, Message: op}
	}
	return parseAPIError(resp, "%s", op)
}
