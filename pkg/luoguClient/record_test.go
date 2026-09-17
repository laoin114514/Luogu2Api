package luoguclient

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 新版 lentille-context 的记录列表结构：data.records
func TestRecordGetListParsesLentilleContext(t *testing.T) {
	const body = `{"status":200,"data":{"records":{"result":[` +
		`{"id":101,"status":12,"score":100,"time":15,"memory":1024,"sourceCodeLength":42,` +
		`"language":14,"enableO2":true,` +
		`"problem":{"pid":"P1001","name":"A+B Problem","difficulty":1,"submitted":true,"accepted":true},` +
		`"user":{"uid":42,"name":"tester"}}],` +
		`"count":7,"perPage":20}}}`

	c := newTestClient(t, serveLentille(t, body))

	list, err := c.Record.GetList(RecordListParams{User: 42, Page: 1})
	if err != nil {
		t.Fatalf("GetList: %v", err)
	}
	if list.Count != 7 {
		t.Errorf("Count = %d, want 7", list.Count)
	}
	if len(list.Records) != 1 {
		t.Fatalf("len(Records) = %d, want 1", len(list.Records))
	}
	rec := list.Records[0]
	if rec.ID != 101 {
		t.Errorf("ID = %d, want 101", rec.ID)
	}
	if rec.Status != StatusAccepted {
		t.Errorf("Status = %d, want %d", rec.Status, StatusAccepted)
	}
	if rec.Language != LangGo {
		t.Errorf("Language = %d, want %d", rec.Language, LangGo)
	}
	if rec.Problem.PID != "P1001" || rec.Problem.Title != "A+B Problem" {
		// 洛谷该字段名为 name（不是 title），标签写错会让标题永远为空
		t.Errorf("Problem = %+v", rec.Problem)
	}
	if !rec.Problem.Submitted || !rec.Problem.Accepted {
		t.Errorf("Problem.Submitted/Accepted = %v/%v", rec.Problem.Submitted, rec.Problem.Accepted)
	}
	if rec.User.UID != 42 || rec.User.Name != "tester" {
		t.Errorf("User = %+v", rec.User)
	}
	if !rec.EnableO2 || rec.SourceCodeLength != 42 {
		t.Errorf("EnableO2/SourceCodeLength = %v/%d", rec.EnableO2, rec.SourceCodeLength)
	}
}

// 旧版 _feInjection 的 currentData 包装仍应兼容
func TestRecordGetListLegacyCurrentDataWrapper(t *testing.T) {
	const body = `{"currentData":{"records":{"result":[{"id":9,"status":14,"problem":{"pid":"P1000"}}],"count":1,"perPage":20}}}`
	c := newTestClient(t, serveLentille(t, body))

	list, err := c.Record.GetList(RecordListParams{Problem: "P1000"})
	if err != nil {
		t.Fatalf("GetList: %v", err)
	}
	if list.Count != 1 || len(list.Records) != 1 || list.Records[0].ID != 9 {
		t.Fatalf("legacy wrapper not parsed: %+v", list)
	}
}

// 记录列表的查询参数应正确拼接
func TestRecordGetListQueryParams(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(lentilleHTML(`{"data":{"records":{"result":[],"count":0}}}`)))
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv)
	if _, err := c.Record.GetList(RecordListParams{
		User:    1582049,
		Problem: "P1001",
		Status:  StatusAccepted,
		Page:    3,
	}); err != nil {
		t.Fatalf("GetList: %v", err)
	}

	for _, want := range []string{"user=1582049", "pid=P1001", "status=12", "page=3"} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("query %q missing %q", gotQuery, want)
		}
	}
}

func TestRecordGetDetailParsesSourceAndJudgeDetail(t *testing.T) {
	// 结构按线上实测取样：testCases 是**数组**；compileResult.message 为 null；
	// judgeResult 这一层的 score/time/memory/status 实测恒为 0。
	const body = `{"data":{"record":{"id":101,"status":12,"score":100,"time":15,"memory":1024,"language":14,` +
		`"sourceCode":"package main\n\nfunc main() {}",` +
		`"problem":{"pid":"P1001","name":"A+B Problem"},` +
		`"detail":{"compileResult":{"success":true,"message":null,"opt2":false},` +
		`"judgeResult":{"status":0,"finishedCaseCount":6,"score":0,"time":0,"memory":0,` +
		`"subtasks":[{"id":1,"score":100,"status":12,"judger":"","time":31,"memory":2904,"testCases":[` +
		`{"id":5,"status":12,"time":31,"memory":2904,"score":0,"signal":0,"exitCode":0,"description":"ok accepted","subtaskID":1},` +
		`{"id":3,"status":6,"time":42,"memory":2920,"score":20,"signal":0,"exitCode":0,"description":"wrong answer On line 1","subtaskID":1}` +
		`]}]}}}}}`

	c := newTestClient(t, serveLentille(t, body))

	rec, err := c.Record.GetDetail(101)
	if err != nil {
		t.Fatalf("GetDetail: %v", err)
	}
	if rec.ID != 101 || rec.SourceCode != "package main\n\nfunc main() {}" {
		t.Errorf("unexpected record: ID=%d source=%q", rec.ID, rec.SourceCode)
	}
	if !rec.Detail.CompileResult.Success {
		t.Error("compile result should be success")
	}
	if rec.Detail.CompileResult.Opt2 {
		t.Error("opt2 should be false")
	}
	if rec.Detail.CompileResult.Message != "" {
		t.Errorf("null message should decode to empty string, got %q", rec.Detail.CompileResult.Message)
	}
	if rec.Detail.JudgeResult.FinishedCaseCount != 6 {
		t.Errorf("FinishedCaseCount = %d, want 6", rec.Detail.JudgeResult.FinishedCaseCount)
	}
	if rec.Detail.JudgeResult.Score != 0 {
		// 线上该层恒为 0；总分在 RecordSummary.Score
		t.Errorf("judgeResult.Score = %d, want 0（实测该层不汇总）", rec.Detail.JudgeResult.Score)
	}
	if len(rec.Detail.JudgeResult.Subtasks) != 1 {
		t.Fatalf("subtasks = %d, want 1", len(rec.Detail.JudgeResult.Subtasks))
	}
	sub := rec.Detail.JudgeResult.Subtasks[0]
	if sub.ID != 1 || sub.Status != StatusAccepted || sub.Memory != 2904 {
		t.Errorf("subtask = %+v", sub)
	}
	if len(sub.TestCases) != 2 {
		t.Fatalf("testCases = %d, want 2（真实数据是数组）", len(sub.TestCases))
	}
	if sub.TestCases[0].Status != TestCaseAccepted || sub.TestCases[0].Description != "ok accepted" {
		t.Errorf("testcase[0] = %+v", sub.TestCases[0])
	}
	if sub.TestCases[1].Status != TestCaseWrongAnswer || !strings.Contains(sub.TestCases[1].Description, "wrong answer") {
		t.Errorf("testcase[1] = %+v", sub.TestCases[1])
	}
	if sub.TestCases[1].SubtaskID != 1 || sub.TestCases[1].Score != 20 {
		t.Errorf("testcase[1] 字段 = %+v", sub.TestCases[1])
	}
}

// 页面没有记录数据时必须报错，而不是返回空结构体
func TestRecordGetDetailMissingData(t *testing.T) {
	c := newTestClient(t, serveLentille(t, `{"data":{"errorCode":404,"errorMessage":"not found"}}`))

	rec, err := c.Record.GetDetail(999999)
	if err == nil {
		t.Fatalf("expected error, got record %+v", rec)
	}
	if !strings.Contains(err.Error(), "record data not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

// 未登录时洛谷返回 401，应统一映射为 *UnauthorizedError
func TestRecordUnauthorizedMapsToUnauthorizedError(t *testing.T) {
	c := newTestClient(t, serveStatus(t, http.StatusUnauthorized))

	if _, err := c.Record.GetList(RecordListParams{Page: 1}); err != nil {
		var unauthorized *UnauthorizedError
		if !errors.As(err, &unauthorized) {
			t.Fatalf("GetList error = %T (%v), want *UnauthorizedError", err, err)
		}
		if unauthorized.StatusCode != http.StatusUnauthorized {
			t.Errorf("StatusCode = %d, want 401", unauthorized.StatusCode)
		}
		if !strings.Contains(unauthorized.Error(), "get record list") {
			t.Errorf("error should describe the operation: %v", unauthorized)
		}
	} else {
		t.Fatal("GetList should fail on 401")
	}

	if _, err := c.Record.GetDetail(1); err == nil {
		t.Fatal("GetDetail should fail on 401")
	} else {
		var unauthorized *UnauthorizedError
		if !errors.As(err, &unauthorized) {
			t.Fatalf("GetDetail error = %T (%v), want *UnauthorizedError", err, err)
		}
	}
}
