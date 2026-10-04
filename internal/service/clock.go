package service

import "time"

// clockNow 便于测试时固定时间
var clockNow = func() time.Time { return time.Now() }
