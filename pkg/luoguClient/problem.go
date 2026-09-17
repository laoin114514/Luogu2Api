package luoguclient

import (
	"fmt"
	"net/url"
	"strconv"
)

// ProblemService 题目服务
type ProblemService struct {
	client *Client
}

// Get 获取题目详情
func (p *ProblemService) Get(pid string) (*Problem, error) {
	path := fmt.Sprintf("/problem/%s", pid)
	resp, err := p.client.get(path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := checkResponse(resp, "get problem %s", pid); err != nil {
		return nil, err
	}

	var result struct {
		Data struct {
			Problem Problem `json:"problem"`
		} `json:"data"`
	}
	if err := parseLentilleContext(resp, &result); err != nil {
		return nil, err
	}
	return &result.Data.Problem, nil
}

// Search 搜索题目
func (p *ProblemService) Search(params SearchParams) (*SearchResult, error) {
	q := url.Values{}
	q.Set("keyword", params.Keyword)
	page := params.Page
	if page <= 0 {
		page = 1
	}
	q.Set("page", strconv.Itoa(page))
	if params.PageSize > 0 {
		q.Set("perPage", strconv.Itoa(params.PageSize))
	}
	path := "/problem/list?" + q.Encode()
	resp, err := p.client.get(path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := checkResponse(resp, "search problems"); err != nil {
		return nil, err
	}

	var result struct {
		Data struct {
			Problems struct {
				Result    []ProblemSummary `json:"result"`
				Count     int              `json:"count"`
				TotalPage int              `json:"totalPages"`
				Page      int              `json:"page"`
				PerPage   int              `json:"perPage"`
			} `json:"problems"`
		} `json:"data"`
	}
	if err := parseLentilleContext(resp, &result); err != nil {
		return nil, err
	}
	return &SearchResult{
		Problems: result.Data.Problems.Result,
		Total:    result.Data.Problems.Count,
		Page:     result.Data.Problems.Page,
		PerPage:  result.Data.Problems.PerPage,
	}, nil
}

// GetSolutions 获取题解列表
func (p *ProblemService) GetSolutions(pid string, page int) (*SolutionList, error) {
	q := url.Values{}
	q.Set("page", strconv.Itoa(page))
	path := fmt.Sprintf("/problem/solution/%s?%s", pid, q.Encode())
	resp, err := p.client.get(path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := checkResponse(resp, "get solutions for %s", pid); err != nil {
		return nil, err
	}

	var result struct {
		Data struct {
			Solutions struct {
				Result    []SolutionSummary `json:"result"`
				Count     int               `json:"count"`
				TotalPage int               `json:"totalPages"`
				Page      int               `json:"page"`
				PerPage   int               `json:"perPage"`
			} `json:"solutions"`
		} `json:"data"`
	}
	if err := parseLentilleContext(resp, &result); err != nil {
		return nil, err
	}
	return &SolutionList{
		Solutions: result.Data.Solutions.Result,
		Total:     result.Data.Solutions.Count,
		Page:      result.Data.Solutions.Page,
		PerPage:   result.Data.Solutions.PerPage,
	}, nil
}

// GetSolutionDetail 获取题解详情
func (p *ProblemService) GetSolutionDetail(sid string) (*Solution, error) {
	path := fmt.Sprintf("/article/%s", sid)
	resp, err := p.client.get(path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := checkResponse(resp, "get solution %s", sid); err != nil {
		return nil, err
	}

	var result struct {
		Data struct {
			Solution Solution `json:"article"`
		} `json:"data"`
	}
	if err := parseLentilleContext(resp, &result); err != nil {
		return nil, err
	}
	return &result.Data.Solution, nil
}

// GetTranslation 获取题目翻译（翻译数据嵌在题目详情页的 lentille-context 中）
//
// 注意：此方法会单独请求一次题目页面。如需同时获取题目详情和翻译，
// 请使用 GetFull() 以减少请求次数。
func (p *ProblemService) GetTranslation(pid string) ([]Translation, error) {
	path := fmt.Sprintf("/problem/%s", pid)
	resp, err := p.client.get(path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := checkResponse(resp, "get translation for %s", pid); err != nil {
		return nil, err
	}

	var result struct {
		Data struct {
			Translations map[string]Translation `json:"translations"`
		} `json:"data"`
	}
	if err := parseLentilleContext(resp, &result); err != nil {
		return nil, err
	}

	var out []Translation
	for lang, t := range result.Data.Translations {
		t.Language = lang
		out = append(out, t)
	}
	return out, nil
}

// GetFull 获取完整的题目信息（题目详情 + 所有翻译），只需一次 HTTP 请求
//
// 相比分别调用 Get() + GetTranslation()（两次请求同一页面），此方法更高效。
func (p *ProblemService) GetFull(pid string) (*Problem, []Translation, error) {
	path := fmt.Sprintf("/problem/%s", pid)
	resp, err := p.client.get(path)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	if err := checkResponse(resp, "get problem %s", pid); err != nil {
		return nil, nil, err
	}

	var result struct {
		Data struct {
			Problem      Problem                `json:"problem"`
			Translations map[string]Translation `json:"translations"`
		} `json:"data"`
	}
	if err := parseLentilleContext(resp, &result); err != nil {
		return nil, nil, err
	}

	var translations []Translation
	for lang, t := range result.Data.Translations {
		t.Language = lang
		translations = append(translations, t)
	}
	return &result.Data.Problem, translations, nil
}
