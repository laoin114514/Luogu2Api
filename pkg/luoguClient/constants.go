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

// OpenSourceType 代码公开范围（偏好设置 openSource 字段）
type OpenSourceType int

// MessageReceiveMode 私信接收范围（偏好设置 messageMode 字段）
type MessageReceiveMode int

// --- 偏好设置常量 ---
// 来源：洛谷前端配置接口 GET /_lfe/config 的 UserOpenSourceType / UserMessageReceiveMode
// （2026-09 抓取，_version=7256d85f54190ceb），与设置页实测写入值一致。
const (
	OpenSourcePrivacyProtection OpenSourceType = -1 // 完全隐私保护
	OpenSourceDisabled          OpenSourceType = 0  // 不公开代码
	OpenSourceEnabled           OpenSourceType = 1  // 加入代码公开计划

	MessageReceiveAdminOnly MessageReceiveMode = 0 // 仅限管理员
	MessageReceiveFollowing MessageReceiveMode = 1 // 关注的人与管理员
	MessageReceiveAnyone    MessageReceiveMode = 2 // 所有人（拉黑的用户除外）
)
