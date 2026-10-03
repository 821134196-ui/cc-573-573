package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

	"campusmeal/models"

	"github.com/gin-gonic/gin"
)

const demoDate = "2026-10-03"

// seed 写入演示数据：2 家企业、厨房/配送员/前台/复核员、午餐订单。
func seed(svc *Service) {
	var n int64
	svc.db.Model(&models.Company{}).Count(&n)
	if n > 0 {
		return
	}
	c1 := models.Company{Name: "星云科技", Address: "园区 A 座 12F"}
	c2 := models.Company{Name: "云帆网络", Address: "园区 B 座 8F"}
	must(svc.db.Create(&c1).Error, "seed company1")
	must(svc.db.Create(&c2).Error, "seed company2")

	users := []models.User{
		{Name: "周大厨", Role: models.RoleKitchen, Phone: "100"},
		{Name: "王骑手", Role: models.RoleCourier, Phone: "201"},
		{Name: "李骑手", Role: models.RoleCourier, Phone: "202"},
		{Name: "陈前台", Role: models.RoleFrontDesk, CompanyID: &c1.ID, Phone: "301"},
		{Name: "林前台", Role: models.RoleFrontDesk, CompanyID: &c2.ID, Phone: "302"},
		{Name: "赵复核", Role: models.RoleReviewer, Phone: "401"},
	}
	for i := range users {
		must(svc.db.Create(&users[i]).Error, "seed user")
	}

	o1 := models.MealOrder{CompanyID: c1.ID, MealDate: demoDate, MealType: models.MealLunch}
	must(svc.db.Create(&o1).Error, "seed order1")
	lines := []models.OrderLine{
		{MealOrderID: o1.ID, DishName: "红烧排骨饭", OrderedQty: 12},
		{MealOrderID: o1.ID, DishName: "宫保鸡丁饭", OrderedQty: 8},
		{MealOrderID: o1.ID, DishName: "素食菌菇饭", OrderedQty: 4},
	}
	for i := range lines {
		must(svc.db.Create(&lines[i]).Error, "seed orderline")
	}

	o2 := models.MealOrder{CompanyID: c2.ID, MealDate: demoDate, MealType: models.MealLunch}
	must(svc.db.Create(&o2).Error, "seed order2")
	lines2 := []models.OrderLine{
		{MealOrderID: o2.ID, DishName: "红烧排骨饭", OrderedQty: 6},
		{MealOrderID: o2.ID, DishName: "番茄牛腩饭", OrderedQty: 6},
	}
	for i := range lines2 {
		must(svc.db.Create(&lines2[i]).Error, "seed orderline2")
	}
	log.Println("已写入演示种子数据（2026-10-03 午餐）")
}

func must(err error, where string) {
	if err != nil {
		log.Fatalf("seed %s: %v", where, err)
	}
}

func main() {
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		dsn = "campusmeal.db"
	}
	db := openDB(dsn)
	svc := NewService(db)
	seed(svc)

	h := &Handler{svc: svc}
	r := router(h)

	// 生产模式托管前端构建产物（dist 目录存在时）
	dist := "./frontend_dist"
	if st, err := os.Stat(dist); err == nil && st.IsDir() {
		r.NoRoute(func(c *gin.Context) {
			p := filepath.Join(dist, c.Request.URL.Path)
			if _, err := os.Stat(p); os.IsNotExist(err) {
				p = filepath.Join(dist, "index.html")
			}
			c.File(p)
		})
	}

	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	srv := &http.Server{Addr: addr, Handler: r}
	log.Printf("园区团餐核对系统已启动: http://localhost%s", addr)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
