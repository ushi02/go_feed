package db

import (
	"database/sql"
	"embed"
	"os"
	"time"

	"github.com/pressly/goose/v3"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

//go:embed migrations/*.sql
var migrations embed.FS

func NewDB() (*gorm.DB, error) {
	dsn := os.Getenv("MYSQL_DSN")
	if dsn == "" {
		dsn = "root:123456@tcp(127.0.0.1:3308)/go_feed?charset=utf8mb4&parseTime=true&loc=UTC"
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	return db, sqlDB.Ping()
}

func Migrate(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return MigrateSQL(sqlDB)
}

func MigrateSQL(db *sql.DB) error {
	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("mysql"); err != nil {
		return err
	}
	return goose.Up(db, "migrations")
}
