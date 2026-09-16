package luoguclient

// RecordStatus 提交状态
type RecordStatus int

// Language 编程语言
type Language int

// --- 提交状态常量 ---
// 来源：实测抓包验证（279 条 laoin 记录 + P1001 记录）
const (
	StatusCompiling  RecordStatus = 2  // 编译/等待中（score=null）
	StatusAccepted   RecordStatus = 12 // 通过（score=100）
	StatusUnaccepted RecordStatus = 14 // 未通过/部分分

	// 以下为测试点级别的状态码（detail.judgeResult.subtasks.testCases.status）
	TestCaseMLEorTLE    RecordStatus = 4  // 资源超限（description 为空，疑为 MLE/TLE）
	TestCaseWrongAnswer RecordStatus = 6  // 答案错误（description 含 "wrong answer"）
	TestCaseAccepted    RecordStatus = 12 // 单个测试点通过（description 含 "ok accepted"）
)

// --- 语言常量 ---
// 来源：实测抓包验证（源码内容匹配）
const (
	LangGo    Language = 14 // Go（源码含 package main / import）
	LangCPP14 Language = 28 // C++14（import <bits/stdc++.h> 等特征）
)
