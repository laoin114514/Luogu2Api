package luogu2api

import (
	"context"
	"net/http"
	"testing"
)

// recordFixture 是 GET /api/v1/users/:uid/records 的真实响应形状（分页字段由服务端算好）
const recordFixture = `{
  "code": 0,
  "message": "ok",
  "data": {
    "uid": 1582049,
    "pid": "P1001",
    "status": 12,
    "page": 2,
    "pageSize": 20,
    "totalPages": 9,
    "count": 178,
    "pageRecordCount": 20,
    "records": [
      {
        "id": 240247732,
        "status": 12,
        "score": 100,
        "time": 15,
        "memory": 1024,
        "sourceCodeLength": 42,
        "submitTime": 1750000000,
        "language": 14,
        "enableO2": true,
        "problem": {
          "pid": "P1001",
          "name": "A+B Problem",
          "difficulty": 1,
          "fullScore": 100,
          "type": "P",
          "submitted": true,
          "accepted": true
        },
        "user": {"uid": 1582049, "name": "tester", "avatar": ""}
      }
    ]
  }
}`

func TestRecordListByUser(t *testing.T) {
	sdk := newTestSDK(t, func(w http.ResponseWriter, r *http.Request) {
		requireToken(t, r)
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/users/1582049/records" {
			t.Errorf("请求 = %s %s", r.Method, r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("pid") != "P1001" || q.Get("status") != "12" || q.Get("page") != "2" {
			t.Errorf("query = %v", q)
		}
		writeRaw(w, http.StatusOK, recordFixture)
	})

	page, err := sdk.Record.ListByUser(context.Background(), 1582049, RecordListParams{
		PID:    " P1001 ",
		Status: RecordStatusAccepted,
		Page:   2,
	})
	if err != nil {
		t.Fatalf("Record.ListByUser: %v", err)
	}

	if page.UID != 1582049 || page.PID != "P1001" || page.Status != RecordStatusAccepted || page.Page != 2 {
		t.Errorf("回显的查询条件 = %+v", page)
	}
	if page.PageSize != 20 || page.TotalPages != 9 || page.Count != 178 || page.PageRecordCount != 20 {
		t.Errorf("分页 = %+v", page)
	}

	if len(page.Records) != 1 {
		t.Fatalf("records = %+v", page.Records)
	}
	record := page.Records[0]
	if record.ID != 240247732 || record.Status != RecordStatusAccepted || record.Score != 100 {
		t.Errorf("record = %+v", record)
	}
	if record.Time != 15 || record.Memory != 1024 || record.SourceCodeLength != 42 || record.SubmitTime != 1750000000 {
		t.Errorf("record 评测信息 = %+v", record)
	}
	if record.Language != LanguageGo || !record.EnableO2 {
		t.Errorf("language/enableO2 = %v/%v", record.Language, record.EnableO2)
	}
	if record.Problem.PID != "P1001" || record.Problem.FullScore != 100 || !record.Problem.Accepted {
		t.Errorf("problem = %+v", record.Problem)
	}
	if record.User.UID != 1582049 || record.User.Name != "tester" {
		t.Errorf("user = %+v", record.User)
	}
}

// 可选过滤与分页都不传时不要发空参数（status=0 表示"全部"，不是"状态 0"）
func TestRecordListByUserOmitsZeroParams(t *testing.T) {
	sdk := newTestSDK(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			t.Errorf("query = %q, want 空", r.URL.RawQuery)
		}
		writeRaw(w, http.StatusOK, recordFixture)
	})

	if _, err := sdk.Record.ListByUser(context.Background(), 1582049, RecordListParams{}); err != nil {
		t.Fatalf("Record.ListByUser: %v", err)
	}
}

func TestRecordListByUserRejectsLocalInvalidParams(t *testing.T) {
	sdk := newTestSDK(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("本地校验失败时不应发出请求: %s", r.URL)
	})

	if _, err := sdk.Record.ListByUser(context.Background(), 0, RecordListParams{}); !IsInvalidParam(err) {
		t.Errorf("uid=0: err = %v, want 参数错误", err)
	}
	if _, err := sdk.Record.ListByUser(context.Background(), 1582049, RecordListParams{Status: -1}); !IsInvalidParam(err) {
		t.Errorf("status=-1: err = %v, want 参数错误", err)
	}
}

func TestRecordListByUserPoolExhausted(t *testing.T) {
	sdk := newTestSDK(t, func(w http.ResponseWriter, r *http.Request) {
		writeEnvelope(t, w, http.StatusServiceUnavailable, CodePoolExhausted, "号池暂无可用的洛谷账号", nil)
	})

	_, err := sdk.Record.ListByUser(context.Background(), 1582049, RecordListParams{})
	if !IsPoolExhausted(err) {
		t.Errorf("err = %v, want 1001", err)
	}
}
