package luoguclient

import (
	"fmt"
	"net/url"
	"strconv"
)

// RecordService 记录服务
type RecordService struct {
	client *Client
}

// recordListPayload 记录列表数据
//
// 新版（lentille-context）与旧版（_feInjection）的字段结构一致，仅外层包装不同：
// 新版为 data.records，旧版为 currentData.records。
type recordListPayload struct {
	Result  []RecordSummary `json:"result"`
	Count   int             `json:"count"`
	PerPage int             `json:"perPage"`
}

// GetList 获取记录列表
func (r *RecordService) GetList(params RecordListParams) (*RecordList, error) {
	q := url.Values{}
	if params.User > 0 {
		q.Set("user", strconv.Itoa(params.User))
	}
	if params.Problem != "" {
		q.Set("pid", params.Problem)
	}
	if params.Status > 0 {
		q.Set("status", strconv.Itoa(int(params.Status)))
	}
	page := params.Page
	if page <= 0 {
		page = 1
	}
	q.Set("page", strconv.Itoa(page))

	path := "/record/list?" + q.Encode()
	resp, err := r.client.get(path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := checkResponse(resp, "get record list"); err != nil {
		return nil, err
	}

	var result struct {
		Data        recordListWrapper `json:"data"`
		CurrentData recordListWrapper `json:"currentData"`
	}
	if err := parseLentilleContext(resp, &result); err != nil {
		return nil, err
	}

	records := result.Data.Records
	if len(records.Result) == 0 && records.Count == 0 {
		// 兼容旧版 _feInjection 的 currentData 包装
		records = result.CurrentData.Records
	}
	return &RecordList{
		Records: records.Result,
		Count:   records.Count,
	}, nil
}

// recordListWrapper 记录列表的包装层
type recordListWrapper struct {
	Records recordListPayload `json:"records"`
}

// GetDetail 获取记录详情（含源代码和评测结果）
func (r *RecordService) GetDetail(rid int) (*RecordDetail, error) {
	path := fmt.Sprintf("/record/%d", rid)
	resp, err := r.client.get(path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if err := checkResponse(resp, "get record %d", rid); err != nil {
		return nil, err
	}

	var result struct {
		Data struct {
			Record RecordDetail `json:"record"`
		} `json:"data"`
		CurrentData struct {
			Record RecordDetail `json:"record"`
		} `json:"currentData"`
	}
	if err := parseLentilleContext(resp, &result); err != nil {
		return nil, err
	}

	record := result.Data.Record
	if record.ID == 0 {
		// 兼容旧版 _feInjection 的 currentData 包装
		record = result.CurrentData.Record
	}
	if record.ID == 0 {
		// 页面拿到了但没有记录数据：明确报错，避免返回空结构体让调用方误判
		return nil, fmt.Errorf("get record %d: record data not found in page", rid)
	}
	return &record, nil
}
