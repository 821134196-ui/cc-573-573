package api

import (
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"mealcheck/web"
)

// registerStatic 挂载嵌入的前端构建产物，并提供 SPA history 回退。
// 用 NoRoute 统一处理 /app/ 前缀，避免 Gin 尾斜杠重定向与静态路由互相干扰。
func registerStatic(r *gin.Engine) {
	dist := web.FS()
	httpFS := http.FS(dist)
	fileServer := http.FileServer(httpFS)

	// 入口字节：FileServer 会把对 /index.html 的请求 301 到 ./，
	// 所以入口与 SPA 回退都直接输出字节，绕开该重定向。
	indexBytes := readIndex(httpFS)

	r.GET("/", func(c *gin.Context) { c.Redirect(http.StatusFound, "/app/") })

	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		if !strings.HasPrefix(path, "/app/") {
			c.JSON(http.StatusNotFound, gin.H{"ok": false, "error": "not found"})
			return
		}
		rel := strings.TrimPrefix(path, "/app/")
		switch {
		case rel == "" || rel == "index.html":
			c.Data(http.StatusOK, "text/html; charset=utf-8", indexBytes)
			return
		}
		if f, err := dist.Open(rel); err == nil {
			_ = f.Close()
			c.Request.URL.Path = "/" + rel
			fileServer.ServeHTTP(c.Writer, c.Request)
			return
		}
		// 前端路由回退：直接输出入口 HTML
		c.Data(http.StatusOK, "text/html; charset=utf-8", indexBytes)
	})
}

func readIndex(dist http.FileSystem) []byte {
	f, err := dist.Open("index.html")
	if err != nil {
		panic(err)
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		panic(err)
	}
	return b
}
