package dao

import (
	"github.com/Terry-Mao/goim/internal/gateway/conf"
	"github.com/Terry-Mao/goim/internal/gateway/model"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// Dao is data access object.
type Dao struct {
	db *gorm.DB
}

// New creates a Dao and auto-migrates the database schema.
func New(c *conf.Config) *Dao {
	db, err := gorm.Open(mysql.Open(c.MySQL.DSN), &gorm.Config{})
	if err != nil {
		panic(err)
	}
	err = db.AutoMigrate(
		&model.User{},
		&model.Friend{},
		&model.Message{},
	)
	if err != nil {
		panic(err)
	}
	return &Dao{db: db}
}

// Close closes the database connection.
func (d *Dao) Close() error {
	sqlDB, err := d.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// Exec executes raw SQL (for tests and migrations).
func (d *Dao) Exec(sql string) {
	d.db.Exec(sql)
}
