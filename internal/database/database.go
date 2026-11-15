package database

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"otp-server/internal/config"
	"otp-server/internal/model"
)

func Open(cfg config.Config) (*gorm.DB, error) {
	var dialector gorm.Dialector
	switch cfg.DBDriver {
	case "sqlite":
		// WAL + busy timeout so background AI jobs and API requests can share the file.
		sep := "?"
		if strings.Contains(cfg.DatabaseURL, "?") {
			sep = "&"
		}
		dialector = sqlite.Open(cfg.DatabaseURL + sep + "_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	case "postgres":
		dialector = postgres.Open(cfg.DatabaseURL)
	default:
		return nil, fmt.Errorf("unsupported DB_DRIVER %q (sqlite|postgres)", cfg.DBDriver)
	}

	db, err := gorm.Open(dialector, &gorm.Config{
		Logger: logger.New(log.New(os.Stderr, "", log.LstdFlags), logger.Config{
			SlowThreshold:             500 * time.Millisecond,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
		}),
		TranslateError: true,
	})
	if err != nil {
		return nil, err
	}
	if cfg.DBDriver == "sqlite" {
		sqlDB, err := db.DB()
		if err != nil {
			return nil, err
		}
		sqlDB.SetMaxOpenConns(1) // sqlite allows a single writer
	}

	if err := db.AutoMigrate(
		&model.Wiki{},
		&model.WikiSession{},
		&model.Chat{},
		&model.Page{},
		&model.GenerationJob{},
	); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}
