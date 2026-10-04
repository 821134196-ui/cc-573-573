// Package webassets 将 Vue 构建产物嵌入 Go 二进制，
// 使整个系统可通过单一可执行文件启动，无需额外静态资源目录。
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS 返回以站点根为根的只读文件系统
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
