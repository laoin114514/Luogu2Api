package luoguclient

import (
	"fmt"
	"net/url"
	"strconv"
)

// DiscussService 讨论服务
type DiscussService struct {
	client *Client
}

// GetList 获取讨论列表
func (d *DiscussService) GetList(page int) (*DiscussList, error) {
	if page <= 0 {
		page = 1
	}
	path := fmt.Sprintf("/discuss/lists?page=%d", page)
	resp, err := d.client.get(path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := checkResponse(resp, "get discuss list"); err != nil {
		return nil, err
	}

	var result struct {
		Data struct {
			Posts        DiscussPosts   `json:"posts"`
			PublicForums []DiscussForum `json:"publicForums"`
			CanPost      bool           `json:"canPost"`
		} `json:"data"`
	}
	if err := parseLentilleContext(resp, &result); err != nil {
		return nil, err
	}
	return &DiscussList{
		Posts:        result.Data.Posts.Result,
		Count:        result.Data.Posts.Count,
		PublicForums: result.Data.PublicForums,
	}, nil
}

// GetDetail 获取讨论详情（含回帖）
func (d *DiscussService) GetDetail(id int, page int) (*DiscussDetail, error) {
	if page <= 0 {
		page = 1
	}
	q := url.Values{}
	q.Set("page", strconv.Itoa(page))
	path := fmt.Sprintf("/discuss/%d?%s", id, q.Encode())
	resp, err := d.client.get(path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := checkResponse(resp, "get discuss %d", id); err != nil {
		return nil, err
	}

	var result struct {
		Data struct {
			Post    DiscussPost    `json:"post"`
			Replies DiscussReplies `json:"replies"`
			Forum   DiscussForum   `json:"forum"`
		} `json:"data"`
	}
	if err := parseLentilleContext(resp, &result); err != nil {
		return nil, err
	}
	return &DiscussDetail{
		Post:    result.Data.Post,
		Replies: result.Data.Replies.Result,
		Count:   result.Data.Replies.Count,
		Forum:   result.Data.Forum,
	}, nil
}
