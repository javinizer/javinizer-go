package database

import (
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"github.com/spf13/afero"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DB wraps the GORM database connection
type DB struct {
	*gorm.DB
	dsn string
	fs  afero.Fs
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
func New(cfg *Config) (*DB, error) {
	var dialector gorm.Dialector

	switch cfg.Type {
	case "sqlite", "":
		dialector = sqlite.Open(normalizeSQLiteDSN(cfg.DSN))
	default:
		return nil, fmt.Errorf("unsupported database type: %s", cfg.Type)
	}

	// Configure database logger level (independent from app logging)
	logLevel := parseLogLevel(cfg.LogLevel)

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
		dsn: cfg.DSN,
		fs:  afero.NewOsFs(),
	}, nil
}

// sqliteTimeFormat is used to format time.Time values for SQLite datetime comparisons.
// SQLite stores timestamps as TEXT in inconsistent formats (RFC3339 with T/Z, with
// fractional seconds, etc.) and GORM binds time.Time as "2006-01-02 15:04:05" (space,
// no TZ). Direct TEXT comparison between these formats produces wrong results because
// 'T' > ' ' and fractional seconds alter lexicographic order. Wrapping both sides in
// datetime() normalizes to a consistent format before comparison.
const sqliteTimeFormat = "2006-01-02 15:04:05"

func normalizeSQLiteDSN(dsn string) string {
	normalized := strings.ToLower(strings.TrimSpace(dsn))
	if normalized != ":memory:" {
		// File-backed SQLite DSN: enable WAL journal mode and a 5s busy timeout
		// to reduce writer/reader lock contention when the batch scrape phase
		// persists concurrently with API/UI reads. The in-memory DSN below
		// already gets _busy_timeout=5000; file DSNs previously used the
		// platform default (no busy handler) which made concurrent writes from
		// the worker pool fail with SQLITE_BUSY under load. WAL allows readers to
		// proceed during writes, and busy_timeout makes writers wait briefly
		// instead of returning SQLITE_BUSY immediately.
		return enhanceFileSQLiteDSN(dsn)
	}
	// `:memory:` is scoped per SQLite connection. Goose migration checks and applies
	// can use multiple connections, so convert to a unique shared-cache memory URI.
	next := sqliteMemoryDSNCounter.Add(1)
	return fmt.Sprintf("file:javinizer_mem_%d_%d?mode=memory&cache=shared&_busy_timeout=5000", time.Now().UnixNano(), next)
}

// dsnParamSep reports the separator expected between the DSN's path component
// and its query parameters: '?' for file: URIs, '&' for query-style params.
// mattn/go-sqlite3 recognizes both file: URI DSNs and plain path DSNs.
func dsnParamSep(dsn string) byte {
	if strings.HasPrefix(strings.ToLower(dsn), "file:") {
		// file: URI DSNs use a '?' separator before the query string. If the DSN
		// already has parameters, append with '&'.
		if strings.Contains(dsn, "?") {
			return '&'
		}
		return '?'
	}
	// Plain-path DSNs use SQLite's pragma query parameters with '?' separators
	// (e.g. /path/to/db.sqlite?_journal_mode=WAL&_busy_timeout=5000).
	if strings.Contains(dsn, "?") {
		return '&'
	}
	return '?'
}

// enhanceFileSQLiteDSN appends WAL journal mode and a 5s busy timeout to a
// file-backed SQLite DSN, preserving any existing query parameters.
func enhanceFileSQLiteDSN(dsn string) string {
	lower := strings.ToLower(dsn)
	sep := dsnParamSep(dsn)
	additions := make([]string, 0, 2)
	if !strings.Contains(lower, "_journal_mode=") && !strings.Contains(lower, "_pragma=journal_mode") {
		additions = append(additions, "_journal_mode=WAL")
	}
	if !strings.Contains(lower, "_busy_timeout=") {
		additions = append(additions, "_busy_timeout=5000")
	}
	if len(additions) == 0 {
		return dsn
	}
	return dsn + string(sep) + strings.Join(additions, "&")
}

// Close closes the database connection
func (db *DB) Close() error {
	sqlDB, err := db.DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
