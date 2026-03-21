package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadOrCreateMigratesLegacyConfigVersion(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	legacy := `server:
  port: 7777
scrapers:
  priority:
    - r18dev
    - dmm
`

	err := os.WriteFile(cfgPath, []byte(legacy), 0644)
	require.NoError(t, err)

	cfg, err := LoadOrCreate(cfgPath)
	require.NoError(t, err)

	assert.Equal(t, CurrentConfigVersion, cfg.ConfigVersion)
	assert.Equal(t, 7777, cfg.Server.Port)
	assert.Equal(t, []string{"r18dev", "dmm", "libredmm", "mgstage", "javlibrary", "javdb", "javbus", "jav321", "tokyohot", "aventertainment", "dlgetchu", "caribbeancom", "fc2"}, cfg.Scrapers.Priority)

	saved, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	assert.Contains(t, string(saved), "config_version: 3")
	assert.Contains(t, string(saved), "libredmm")
	assert.Contains(t, string(saved), "javlibrary")
	assert.Contains(t, string(saved), "javdb")
	assert.Contains(t, string(saved), "javbus")
	assert.Contains(t, string(saved), "jav321")
	assert.Contains(t, string(saved), "tokyohot")
	assert.Contains(t, string(saved), "aventertainment")
	assert.Contains(t, string(saved), "dlgetchu")
	assert.Contains(t, string(saved), "caribbeancom")
	assert.Contains(t, string(saved), "fc2")
}

func TestLoadOrCreateSkipsMigrationForCurrentVersion(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	current := `config_version: 3
server:
  port: 9090
scrapers:
  priority:
    - dmm
`

	err := os.WriteFile(cfgPath, []byte(current), 0644)
	require.NoError(t, err)

	cfg, err := LoadOrCreate(cfgPath)
	require.NoError(t, err)

	assert.Equal(t, CurrentConfigVersion, cfg.ConfigVersion)
	assert.Equal(t, []string{"dmm"}, cfg.Scrapers.Priority)
	assert.Equal(t, 9090, cfg.Server.Port)

	saved, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	assert.True(t, strings.Contains(string(saved), "config_version: 3"))
	assert.True(t, strings.Contains(string(saved), "- dmm"))
}

func TestLoadOrCreatePreservesUnknownKeysAndCommentsOnMigration(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	legacy := `# user-managed config
server:
  port: 8081
scrapers:
  priority:
    - dmm
  custom_source:
    enabled: true
`

	err := os.WriteFile(cfgPath, []byte(legacy), 0644)
	require.NoError(t, err)

	cfg, err := LoadOrCreate(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, CurrentConfigVersion, cfg.ConfigVersion)

	saved, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	savedText := string(saved)

	assert.Contains(t, savedText, "# user-managed config")
	assert.Contains(t, savedText, "custom_source:")
	assert.Contains(t, savedText, "config_version: 3")
	assert.Contains(t, savedText, "libredmm")
	assert.Contains(t, savedText, "javlibrary")
	assert.Contains(t, savedText, "javdb")
	assert.Contains(t, savedText, "javbus")
	assert.Contains(t, savedText, "jav321")
	assert.Contains(t, savedText, "tokyohot")
	assert.Contains(t, savedText, "aventertainment")
	assert.Contains(t, savedText, "dlgetchu")
	assert.Contains(t, savedText, "caribbeancom")
	assert.Contains(t, savedText, "fc2")
}

func TestLoadOrCreateMigrationPreservesExplicitUpdateDisabled(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	v2 := `config_version: 2
system:
  update_enabled: false
  update_check_interval_hours: 12
`

	err := os.WriteFile(cfgPath, []byte(v2), 0644)
	require.NoError(t, err)

	cfg, err := LoadOrCreate(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, CurrentConfigVersion, cfg.ConfigVersion)
	assert.False(t, cfg.System.UpdateEnabled)
	assert.Equal(t, 12, cfg.System.UpdateCheckIntervalHours)

	saved, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	savedText := string(saved)
	assert.Contains(t, savedText, "config_version: 3")
	assert.Contains(t, savedText, "update_enabled: false")
	assert.Contains(t, savedText, "update_check_interval_hours: 12")
}

func TestLoadOrCreateRejectsNewerConfigVersion(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	newer := `config_version: 999
server:
  port: 8080
`

	err := os.WriteFile(cfgPath, []byte(newer), 0644)
	require.NoError(t, err)

	_, err = LoadOrCreate(cfgPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "newer than supported version")
}

func TestLegacyScraperPriorityBaseline(t *testing.T) {
	assert.Equal(
		t,
		[]string{
			"r18dev",
			"dmm",
			"libredmm",
			"mgstage",
			"javlibrary",
			"javdb",
			"javbus",
			"jav321",
			"tokyohot",
			"aventertainment",
			"dlgetchu",
			"caribbeancom",
			"fc2",
		},
		legacyScraperPriorityBaseline(),
	)
}
