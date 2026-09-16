package model

import "errors"

// 跨越 repository → service → handler 的哨兵错误。
//
// 放在 model（所有层都已依赖的叶子包）里，避免 service/handler 为了
// errors.Is 判断而 import repository（那会把 GORM 的依赖扩散出去）。
var (
	// ErrAccountNotFound 账号不存在（或已软删除）
	ErrAccountNotFound = errors.New("账号不存在")
	// ErrAccountExists 用户名或洛谷 UID 冲突
	ErrAccountExists = errors.New("账号已存在")
)
