package service

import "errors"

var (
	ErrNotFound      = errors.New("记录不存在")
	ErrConflict      = errors.New("状态冲突")
	ErrForbidden     = errors.New("无权限执行该操作")
	ErrValidation    = errors.New("参数校验失败")
	ErrSealed        = errors.New("批次已封箱，装箱数量不可修改")
	ErrNoActiveRoute = errors.New("该批次当前没有有效路线任务")
	ErrAlreadyPosted = errors.New("退款已入账，回执不得重复记账")
)
