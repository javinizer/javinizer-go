package config

import (
	"fmt"
	"os"
	"time"
)

type Migration interface {
	FromVersions() []int
	ToVersion() int
	Description() string
	Migrate(cfg *Config) error
}

type MigrationContext struct {
	ConfigPath string
	DryRun     bool
	BackupPath string
}

var migrationContext MigrationContext

func SetMigrationContext(ctx MigrationContext) {
	migrationContext = ctx
}

func GetMigrationContext() MigrationContext {
	return migrationContext
}

var migrations = make(map[int]Migration)

func RegisterMigration(m Migration) {
	for _, v := range m.FromVersions() {
		migrations[v] = m
	}
}

//nolint:unused // Used in tests
func getMigration(fromVersion int) (Migration, bool) {
	m, ok := migrations[fromVersion]
	return m, ok
}

func MigrateToCurrent(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}

	if cfg.ConfigVersion > CurrentConfigVersion {
		return fmt.Errorf("no migration from version %d. This version is not supported", cfg.ConfigVersion)
	}

	for cfg.ConfigVersion < CurrentConfigVersion {
		m, ok := migrations[cfg.ConfigVersion]
		if !ok {
			return fmt.Errorf("no migration from version %d. This version is not supported", cfg.ConfigVersion)
		}
		if err := m.Migrate(cfg); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
		cfg.ConfigVersion = m.ToVersion()
	}

	return nil
}

func ResetMigrations() {
	migrations = make(map[int]Migration)
	migrationContext = MigrationContext{}
}

type LegacyMigration struct{}

func NewLegacyMigration() *LegacyMigration {
	return &LegacyMigration{}
}

func (m *LegacyMigration) FromVersions() []int { return []int{0, 1, 2} }
func (m *LegacyMigration) ToVersion() int      { return 3 }
func (m *LegacyMigration) Description() string {
	return "Backup + recreate from embedded example (v0/v1/v2 → v3)"
}

func (m *LegacyMigration) Migrate(cfg *Config) error {
	ctx := GetMigrationContext()

	if ctx.ConfigPath != "" && !ctx.DryRun {
		if _, err := os.Stat(ctx.ConfigPath); err == nil {
			backupPath := fmt.Sprintf("%s.bak-%s", ctx.ConfigPath, time.Now().Format("20060102-150405"))
			data, err := os.ReadFile(ctx.ConfigPath)
			if err != nil {
				return fmt.Errorf("failed to read config for backup: %w", err)
			}
			if err := os.WriteFile(backupPath, data, FilePerm); err != nil {
				return fmt.Errorf("failed to create backup: %w", err)
			}
			ctx.BackupPath = backupPath
			SetMigrationContext(ctx)
		}
	}

	newCfg := DefaultConfig()
	*cfg = *newCfg

	return nil
}
