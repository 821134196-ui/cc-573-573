package store

import (
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"mealcheck/internal/models"
)

func Open(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, err
	}
	// SQLite 单写，开启 WAL 与外键
	if err := db.Exec("PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;").Error; err != nil {
		return nil, err
	}
	if err := Migrate(db); err != nil {
		return nil, err
	}
	if err := Seed(db); err != nil {
		return nil, err
	}
	return db, nil
}

func Migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&models.User{},
		&models.Enterprise{},
		&models.Route{},
		&models.Batch{},
		&models.BatchItem{},
		&models.Box{},
		&models.RouteAssignment{},
		&models.RouteEvent{},
		&models.Receipt{},
		&models.ReceiptLine{},
		&models.Discrepancy{},
		&models.DiscrepancyLine{},
		&models.Resolution{},
		&models.RefundRecord{},
		&models.AuditLog{},
	); err != nil {
		return err
	}
	// 同一批次至多一条有效路线任务 —— 数据库层兜底“不能两条路线同时完成”
	stmts := []string{
		// 清理旧版本 GORM 可能按字段 tag 生成的错误全局唯一索引
		`DROP INDEX IF EXISTS idx_assignment_active`,
		`DROP INDEX IF EXISTS route_assignments.active`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_assignment_active_guard
		 ON route_assignments(batch_id) WHERE active = 1`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_seal_no ON boxes(seal_no)`,
	}
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil {
			return err
		}
	}
	return nil
}

// Seed 预置企业、角色账号、配送路线
func Seed(db *gorm.DB) error {
	var n int64
	db.Model(&models.User{}).Count(&n)
	if n > 0 {
		return nil
	}
	ents := []models.Enterprise{
		{Name: "星河科技", Address: "园区 A 栋 12 层"},
		{Name: "云杉设计", Address: "园区 B 栋 3 层"},
	}
	if err := db.Create(&ents).Error; err != nil {
		return err
	}
	users := []models.User{
		{Username: "kitchen", Name: "厨房-王师傅", Role: models.RoleKitchen},
		{Username: "desk1", Name: "星河科技前台", Role: models.RoleFrontDesk, EnterpriseID: &ents[0].ID},
		{Username: "desk2", Name: "云杉设计前台", Role: models.RoleFrontDesk, EnterpriseID: &ents[1].ID},
		{Username: "reviewer", Name: "运营复核员", Role: models.RoleReviewer},
		{Username: "admin", Name: "调度管理员", Role: models.RoleAdmin},
	}
	courierNames := []string{"配送员-甲", "配送员-乙", "配送员-丙"}
	for i := 0; i < 3; i++ {
		users = append(users, models.User{
			Username: "c" + string(rune('0'+i+1)),
			Name:     courierNames[i],
			Role:     models.RoleCourier,
		})
	}
	if err := db.Create(&users).Error; err != nil {
		return err
	}
	var couriers []models.User
	db.Where("role = ?", models.RoleCourier).Find(&couriers)
	routes := []models.Route{
		{Name: "北线", CourierID: couriers[0].ID},
		{Name: "南线", CourierID: couriers[1].ID},
		{Name: "机动线", CourierID: couriers[2].ID},
	}
	if err := db.Create(&routes).Error; err != nil {
		return err
	}
	return nil
}
