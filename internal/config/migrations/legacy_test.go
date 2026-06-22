package migrations

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLegacyMigration_Info(t *testing.T) {
	m := config.NewLegacyMigration()

	assert.Equal(t, []int{0, 1, 2}, m.FromVersions())
	assert.Equal(t, 3, m.ToVersion())
	assert.Contains(t, m.Description(), "Backup")
}

func TestLegacyMigration_Migrate_CreatesBackup(t *testing.T) {
	config.RegisterMigration(config.NewLegacyMigration())

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	oldContent := "config_version: 2\nserver:\n  port: 9999\n"
	err := os.WriteFile(configPath, []byte(oldContent), 0644)
	require.NoError(t, err)

	config.SetMigrationContext(config.MigrationContext{
		ConfigPath: configPath,
		DryRun:     false,
	})

	cfg := &config.Config{ConfigVersion: 2}
	err = config.MigrateToCurrent(cfg)
	require.NoError(t, err)

	assert.Equal(t, 3, cfg.ConfigVersion)

	ctx := config.GetMigrationContext()
	assert.NotEmpty(t, ctx.BackupPath, "backup path should be set")

	_, err = os.Stat(ctx.BackupPath)
	assert.NoError(t, err, "backup file should exist")
}

func TestLegacyMigration_Migrate_DryRun_NoBackup(t *testing.T) {
	config.RegisterMigration(config.NewLegacyMigration())

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	oldContent := "config_version: 1\n"
	err := os.WriteFile(configPath, []byte(oldContent), 0644)
	require.NoError(t, err)

	config.SetMigrationContext(config.MigrationContext{
		ConfigPath: configPath,
		DryRun:     true,
	})

	cfg := &config.Config{ConfigVersion: 1}
	err = config.MigrateToCurrent(cfg)
	require.NoError(t, err)

	assert.Equal(t, 3, cfg.ConfigVersion)

	ctx := config.GetMigrationContext()
	assert.Empty(t, ctx.BackupPath, "no backup should be created in dry run")
}
