package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"

	"mealcheck/internal/api"
	"mealcheck/internal/store"
)

func main() {
	addr := flag.String("addr", envOr("ADDR", ":8080"), "监听地址")
	dbPath := flag.String("db", envOr("DB_PATH", "data/mealcheck.db"), "SQLite 数据库路径")
	flag.Parse()

	if err := os.MkdirAll(filepath.Dir(*dbPath), 0o755); err != nil {
		log.Fatalf("创建数据目录失败: %v", err)
	}
	db, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	h := api.NewWithDB(db)
	r := h.Router()
	log.Printf("前端已嵌入二进制（/app/）")
	log.Printf("园区团餐批次核对系统启动: http://localhost%s", *addr)
	if err := r.Run(*addr); err != nil {
		log.Fatal(err)
	}
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
