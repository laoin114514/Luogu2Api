package luoguclient

import (
	"fmt"
	"net/url"
	"strconv"
)

// TrainingService 题单服务
type TrainingService struct {
	client *Client
}

// GetList 获取题单列表
func (t *TrainingService) GetList(params TrainingListParams) (*TrainingList, error) {
	q := url.Values{}
	if params.Keyword != "" {
		q.Set("keyword", params.Keyword)
	}
	if params.Type > 0 {
		q.Set("type", strconv.Itoa(params.Type))
	}
	page := params.Page
	if page <= 0 {
		page = 1
	}
	q.Set("page", strconv.Itoa(page))

	path := "/training/list?" + q.Encode()
	resp, err := t.client.get(path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := checkResponse(resp, "get training list"); err != nil {
		return nil, err
	}

	var result struct {
		Data struct {
			Trainings struct {
				Result  []TrainingSummary `json:"result"`
				Count   int               `json:"count"`
				PerPage int               `json:"perPage"`
			} `json:"trainings"`
		} `json:"data"`
	}
	if err := parseLentilleContext(resp, &result); err != nil {
		return nil, err
	}
	return &TrainingList{
		Trainings: result.Data.Trainings.Result,
		Count:     result.Data.Trainings.Count,
	}, nil
}

// GetDetail 获取题单详情（含题目列表）
func (t *TrainingService) GetDetail(tid int) (*TrainingDetail, error) {
	path := fmt.Sprintf("/training/%d", tid)
	resp, err := t.client.get(path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := checkResponse(resp, "get training %d", tid); err != nil {
		return nil, err
	}

	var result struct {
		Data struct {
			Training TrainingDetail `json:"training"`
		} `json:"data"`
	}
	if err := parseLentilleContext(resp, &result); err != nil {
		return nil, err
	}
	return &result.Data.Training, nil
}
