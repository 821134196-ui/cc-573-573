package main

import (
	"log"

	"campusmeal/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openDB(dsn string) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	migrate(db)
	return db
}

func migrate(db *gorm.DB) {
	if err := db.AutoMigrate(
		&models.Company{},
		&models.User{},
		&models.MealOrder{},
		&models.OrderLine{},
		&models.Batch{},
		&models.PackItem{},
		&models.CoolerBox{},
		&models.RouteAssignment{},
		&models.Event{},
		&models.Receipt{},
		&models.ReceiptLine{},
		&models.Discrepancy{},
		&models.LossRecord{},
		&models.Refund{},
		&models.BoxLedger{},
	); err != nil {
		log.Fatalf("迁移失败: %v", err)
	}
}
