package luoguclient

import (
	"fmt"
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
