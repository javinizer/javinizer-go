package database

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DB wraps the GORM database connection
type DB struct {
	*gorm.DB
	dsn string
}

var sqliteMemoryDSNCounter atomic.Uint64

// parseLogLevel converts a log level string to a GORM logger.LogLevel
// Normalizes input by trimming whitespace and converting to lowercase
// Returns logger.Silent for invalid values with a warning
func parseLogLevel(level string) logger.LogLevel {
	// Normalize input: trim whitespace and convert to lowercase for case-insensitive comparison
	normalized := strings.ToLower(strings.TrimSpace(level))

	switch normalized {
	case "info":
		return logger.Info
	case "warn":
		return logger.Warn
	case "error":
		return logger.Error
	case "silent", "":
		return logger.Silent
	default:
		// Invalid log level provided - warn and default to silent
		log.Printf("Warning: invalid database log_level '%s', defaulting to 'silent'. Valid options: silent, error, warn, info\n", level)
		return logger.Silent
	}
}

// New creates a new database connection
func New(cfg *config.Config) (*DB, error) {
	var dialector gorm.Dialector

	switch cfg.Database.Type {
	case "sqlite", "":
		dialector = sqlite.Open(normalizeSQLiteDSN(cfg.Database.DSN))
	default:
		return nil, fmt.Errorf("unsupported database type: %s", cfg.Database.Type)
	}

	// Configure database logger level (independent from app logging)
	logLevel := parseLogLevel(cfg.Database.LogLevel)

	db, err := gorm.Open(dialector, &gorm.Config{
		Logger: logger.Default.LogMode(logLevel),
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	return &DB{
		DB:  db,
		dsn: cfg.Database.DSN,
	}, nil
}

// AutoMigrate runs startup database migrations.
//
// Kept for backward compatibility in tests and existing call sites.
// New runtime paths should call RunMigrationsOnStartup directly.
func (db *DB) AutoMigrate() error {
	return db.RunMigrationsOnStartup(context.Background())
}

// SqliteTimeFormat is used to format time.Time values for SQLite datetime comparisons.
// SQLite stores timestamps as TEXT in inconsistent formats (RFC3339 with T/Z, with
// fractional seconds, etc.) and GORM binds time.Time as "2006-01-02 15:04:05" (space,
// no TZ). Direct TEXT comparison between these formats produces wrong results because
// 'T' > ' ' and fractional seconds alter lexicographic order. Wrapping both sides in
// datetime() normalizes to a consistent format before comparison.
const SqliteTimeFormat = "2006-01-02 15:04:05"

func normalizeSQLiteDSN(dsn string) string {
	normalized := strings.ToLower(strings.TrimSpace(dsn))
	if normalized != ":memory:" {
		return dsn
	}
	// `:memory:` is scoped per SQLite connection. Goose migration checks and applies
	// can use multiple connections, so convert to a unique shared-cache memory URI.
	next := sqliteMemoryDSNCounter.Add(1)
	return fmt.Sprintf("file:javinizer_mem_%d_%d?mode=memory&cache=shared", time.Now().UnixNano(), next)
}

// Close closes the database connection
func (db *DB) Close() error {
	sqlDB, err := db.DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// isRecordNotFound returns true if the error is a GORM record-not-found error.
func isRecordNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}
