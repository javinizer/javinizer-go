package database

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm/logger"
)

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedLevel logger.LogLevel
	}{
		{"lowercase silent", "silent", logger.Silent},
		{"lowercase info", "info", logger.Info},
		{"lowercase warn", "warn", logger.Warn},
		{"lowercase error", "error", logger.Error},
		{"empty string defaults to silent", "", logger.Silent},
		{"mixed case Info", "Info", logger.Info},
		{"uppercase WARN", "WARN", logger.Warn},
		{"with leading whitespace", "  info", logger.Info},
		{"with trailing whitespace", "warn  ", logger.Warn},
		{"with both whitespace", "  error  ", logger.Error},
		{"invalid value defaults to silent", "invalid", logger.Silent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the actual parseLogLevel function used in production
			result := parseLogLevel(tt.input)

			if result != tt.expectedLevel {
				t.Errorf("parseLogLevel(%q) = %v, want %v", tt.input, result, tt.expectedLevel)
			}
		})
	}
}

func TestDBClose(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := New(cfg)
	require.NoError(t, err)

	// Close the database
	err = db.Close()
	require.NoError(t, err)

	// Try to use closed database (should work since it's just a connection close)
	// We can't easily test that it's actually closed without errors,
	// but the test validates that Close() doesn't panic
}

func TestDBAutoMigrate(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Test AutoMigrate creates all tables
	err = db.AutoMigrate()
	require.NoError(t, err)

	// Verify tables exist by checking if we can create records
	repo := NewMovieRepository(db)
	movie := &models.Movie{
		ContentID: "test-migrate",
		ID:        "TEST-MIGRATE",
		Title:     "Migration Test",
	}
	err = repo.Create(movie)
	require.NoError(t, err)

	found, err := repo.FindByID("TEST-MIGRATE")
	require.NoError(t, err)
	assert.Equal(t, "Migration Test", found.Title)
}

func TestParseLogLevelEdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"all lowercase info", "INFO"},
		{"with tabs", "\tinfo\t"},
		{"with newlines", "\nwarn\n"},
		{"mixed case mixed", "ErRoR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseLogLevel(tt.input)
			assert.NotEqual(t, logger.Silent, result, "Should not default to silent for valid inputs")
		})
	}
}

func TestDBAutoMigrate_DMMIDPartialUniqueIndex(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.AutoMigrate())

	// Ensure partial unique index exists for dmm_id > 0.
	var count int
	err = db.DB.Raw(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND tbl_name='actresses' AND name='idx_actresses_dmm_id_positive' AND sql LIKE '%WHERE dmm_id > 0%'",
	).Scan(&count).Error
	require.NoError(t, err)
	require.Equal(t, 1, count)

	repo := NewActressRepository(db)

	// Multiple unknown DMM IDs (0) should be allowed.
	require.NoError(t, repo.Create(&models.Actress{DMMID: 0, JapaneseName: "零A"}))
	require.NoError(t, repo.Create(&models.Actress{DMMID: 0, JapaneseName: "零B"}))

	// Real DMM IDs (>0) must remain unique.
	require.NoError(t, repo.Create(&models.Actress{DMMID: 123456, JapaneseName: "正A"}))
	err = repo.Create(&models.Actress{DMMID: 123456, JapaneseName: "正B"})
	require.Error(t, err)
}

func TestRunMigrationsOnStartup_RepairsLegacyConstraintAndNormalizesNegativeDMMID(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.DB.Exec(`
		CREATE TABLE actresses (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			dmm_id INTEGER,
			first_name TEXT,
			last_name TEXT,
			japanese_name TEXT,
			thumb_url TEXT,
			aliases TEXT,
			created_at DATETIME,
			updated_at DATETIME,
			UNIQUE(dmm_id),
			UNIQUE(japanese_name)
		)
	`).Error)
	require.NoError(t, db.DB.Exec(
		"CREATE INDEX IF NOT EXISTS idx_custom_actresses_dmm_id_name ON actresses(dmm_id, japanese_name)",
	).Error)
	require.NoError(t, db.DB.Exec(
		"INSERT INTO actresses (dmm_id, japanese_name) VALUES (-111, '負A'), (-222, '負B'), (333, '正A')",
	).Error)

	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	var zeroCount int
	err = db.DB.Raw(
		"SELECT COUNT(*) FROM actresses WHERE dmm_id = 0",
	).Scan(&zeroCount).Error
	require.NoError(t, err)
	assert.Equal(t, 2, zeroCount, "legacy negative dmm_id values should be normalized to zero")

	var positiveCount int
	err = db.DB.Raw(
		"SELECT COUNT(*) FROM actresses WHERE dmm_id = 333",
	).Scan(&positiveCount).Error
	require.NoError(t, err)
	assert.Equal(t, 1, positiveCount)

	var customIndexCount int
	err = db.DB.Raw(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND tbl_name='actresses' AND name='idx_custom_actresses_dmm_id_name'",
	).Scan(&customIndexCount).Error
	require.NoError(t, err)
	assert.Equal(t, 1, customIndexCount, "custom indexes should be preserved during legacy table rebuild")

	// Unique constraints that were previously represented as constraint-owned
	// indexes should remain enforced after rebuild.
	err = db.DB.Exec("INSERT INTO actresses (dmm_id, japanese_name) VALUES (444, '正A')").Error
	require.Error(t, err, "duplicate japanese_name should remain rejected after rebuild")
}

func TestRunMigrationsOnStartup_PreservesConstraintCollationOnRebuild(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.DB.Exec(`
		CREATE TABLE actresses (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			dmm_id INTEGER,
			first_name TEXT,
			last_name TEXT,
			japanese_name TEXT,
			thumb_url TEXT,
			aliases TEXT,
			created_at DATETIME,
			updated_at DATETIME,
			UNIQUE(dmm_id),
			UNIQUE(japanese_name COLLATE NOCASE)
		)
	`).Error)
	require.NoError(t, db.DB.Exec(
		"INSERT INTO actresses (dmm_id, japanese_name) VALUES (-1, 'abc')",
	).Error)

	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	err = db.DB.Exec(
		"INSERT INTO actresses (dmm_id, japanese_name) VALUES (1, 'ABC')",
	).Error
	require.Error(t, err, "NOCASE collation on preserved unique constraint should still reject case-insensitive duplicates")
}

func TestRunMigrationsOnStartup_PreservesNonIndexConstraintsOnRebuild(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.DB.Exec(`
		CREATE TABLE actresses (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			dmm_id INTEGER,
			first_name TEXT,
			last_name TEXT,
			japanese_name TEXT CHECK(length(japanese_name) > 0),
			thumb_url TEXT,
			aliases TEXT,
			created_at DATETIME,
			updated_at DATETIME,
			UNIQUE(dmm_id)
		)
	`).Error)
	require.NoError(t, db.DB.Exec(
		"INSERT INTO actresses (dmm_id, japanese_name) VALUES (-1, 'abc')",
	).Error)

	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	err = db.DB.Exec(
		"INSERT INTO actresses (dmm_id, japanese_name) VALUES (1, '')",
	).Error
	require.Error(t, err, "CHECK constraint should still reject empty japanese_name after rebuild")
}

func TestRunMigrationsOnStartup_SupportsInlineUniqueDMMConstraintOnRebuild(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.DB.Exec(`
		CREATE TABLE actresses (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			dmm_id INTEGER UNIQUE,
			first_name TEXT,
			last_name TEXT,
			japanese_name TEXT,
			thumb_url TEXT,
			aliases TEXT,
			created_at DATETIME,
			updated_at DATETIME
		)
	`).Error)
	require.NoError(t, db.DB.Exec(
		"INSERT INTO actresses (dmm_id, japanese_name) VALUES (-1, 'A'), (5, 'B')",
	).Error)

	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	var normalizedNegativeCount int
	err = db.DB.Raw("SELECT COUNT(*) FROM actresses WHERE dmm_id = 0").Scan(&normalizedNegativeCount).Error
	require.NoError(t, err)
	assert.Equal(t, 1, normalizedNegativeCount)

	err = db.DB.Exec(
		"INSERT INTO actresses (dmm_id, japanese_name) VALUES (0, 'C')",
	).Error
	require.NoError(t, err, "multiple dmm_id=0 rows should be allowed after inline unique removal")

	err = db.DB.Exec(
		"INSERT INTO actresses (dmm_id, japanese_name) VALUES (5, 'D')",
	).Error
	require.Error(t, err, "positive dmm_id values should remain unique after migration")
}

func TestRunMigrationsOnStartup_SupportsInlineUniqueDMMConstraintWithConflictClause(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.DB.Exec(`
		CREATE TABLE actresses (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			dmm_id INTEGER UNIQUE ON CONFLICT IGNORE,
			first_name TEXT,
			last_name TEXT,
			japanese_name TEXT,
			thumb_url TEXT,
			aliases TEXT,
			created_at DATETIME,
			updated_at DATETIME
		)
	`).Error)
	require.NoError(t, db.DB.Exec(
		"INSERT INTO actresses (dmm_id, japanese_name) VALUES (-1, 'A'), (9, 'B')",
	).Error)

	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	err = db.DB.Exec(
		"INSERT INTO actresses (dmm_id, japanese_name) VALUES (9, 'C')",
	).Error
	require.Error(t, err, "positive dmm_id uniqueness should be enforced by canonical partial index")

	var rebuiltSchemaSQL string
	err = db.DB.Raw(
		"SELECT sql FROM sqlite_master WHERE type='table' AND name='actresses'",
	).Scan(&rebuiltSchemaSQL).Error
	require.NoError(t, err)
	assert.NotContains(t, strings.ToLower(rebuiltSchemaSQL), "constraint uq_dmm", "inline named unique constraint should be removed entirely, not left dangling")
}

func TestRunMigrationsOnStartup_SupportsInlineUniqueDMMConstraintWithQuotedConstraintName(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.DB.Exec(`
		CREATE TABLE actresses (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			dmm_id INTEGER CONSTRAINT "uq dmm" UNIQUE ON CONFLICT IGNORE,
			first_name TEXT,
			last_name TEXT,
			japanese_name TEXT,
			thumb_url TEXT,
			aliases TEXT,
			created_at DATETIME,
			updated_at DATETIME
		)
	`).Error)
	require.NoError(t, db.DB.Exec(
		"INSERT INTO actresses (dmm_id, japanese_name) VALUES (-1, 'A'), (9, 'B')",
	).Error)

	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	var rebuiltSchemaSQL string
	err = db.DB.Raw(
		"SELECT sql FROM sqlite_master WHERE type='table' AND name='actresses'",
	).Scan(&rebuiltSchemaSQL).Error
	require.NoError(t, err)
	assert.NotContains(t, strings.ToLower(rebuiltSchemaSQL), `constraint "uq dmm"`, "quoted inline unique constraint prefix should be removed entirely")
}

func TestRunMigrationsOnStartup_CreatesBackupAndIsIdempotent(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "javinizer.db")

	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  dbPath,
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	initialBackups, err := filepath.Glob(dbPath + ".*.backup")
	require.NoError(t, err)
	require.Len(t, initialBackups, 1, "first startup migration should create exactly one backup file")

	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	finalBackups, err := filepath.Glob(dbPath + ".*.backup")
	require.NoError(t, err)
	assert.Len(t, finalBackups, 1, "no additional backups should be created when no migrations are pending")
}

func TestRunMigrationsOnStartup_FileURIRestoreHintUsesFilesystemPath(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "javinizer.db")
	uriDSN := "file:" + dbPath + "?cache=shared"

	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  uriDSN,
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.DB.Exec(`
		CREATE TABLE actresses (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			dmm_id INTEGER,
			first_name TEXT,
			last_name TEXT,
			japanese_name TEXT,
			thumb_url TEXT,
			aliases TEXT,
			created_at DATETIME,
			updated_at DATETIME
		)
	`).Error)
	require.NoError(t, db.DB.Exec(
		"INSERT INTO actresses (dmm_id, japanese_name) VALUES (777, 'A'), (777, 'B')",
	).Error)

	err = db.RunMigrationsOnStartup(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "restore with: cp")
	assert.Contains(t, err.Error(), dbPath)
	assert.NotContains(t, err.Error(), uriDSN)
}
