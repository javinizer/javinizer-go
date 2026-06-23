package tui

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/stretchr/testify/assert"
)

func TestConfigureTUILogging_StripsStdoutFromDefaultConfig(t *testing.T) {
	cfg := &config.Config{}
	cfg.Logging.Output = "stdout,data/logs/javinizer.log"
	cfg.Logging.Level = "info"
	cfg.Logging.Format = "text"

	logCfg := configureTUILogging(cfg, false)

	assert.NotContains(t, logCfg.Output, "stdout")
	assert.Equal(t, "data/logs/javinizer.log", logCfg.Output)
}

func TestConfigureTUILogging_PureStdoutFallsBackToDefault(t *testing.T) {
	cfg := &config.Config{}
	cfg.Logging.Output = "stdout"

	logCfg := configureTUILogging(cfg, false)

	assert.Equal(t, "data/logs/javinizer-tui.log", logCfg.Output)
}

func TestConfigureTUILogging_StdoutStderrStrippedKeepsFiles(t *testing.T) {
	cfg := &config.Config{}
	cfg.Logging.Output = "stdout,stderr,/var/log/a.log,/var/log/b.log"

	logCfg := configureTUILogging(cfg, false)

	assert.Equal(t, "/var/log/a.log,/var/log/b.log", logCfg.Output)
}

func TestConfigureTUILogging_PreservesRotation(t *testing.T) {
	cfg := &config.Config{}
	cfg.Logging.Output = "stdout"
	cfg.Logging.MaxSizeMB = 10
	cfg.Logging.MaxBackups = 5
	cfg.Logging.MaxAgeDays = 30
	cfg.Logging.Compress = true

	logCfg := configureTUILogging(cfg, false)

	assert.Equal(t, 10, logCfg.MaxSizeMB)
	assert.Equal(t, 5, logCfg.MaxBackups)
	assert.Equal(t, 30, logCfg.MaxAgeDays)
	assert.True(t, logCfg.Compress)
}

func TestConfigureTUILogging_VerboseFlagSetsDebug(t *testing.T) {
	cfg := &config.Config{}
	cfg.Logging.Output = "stdout"
	cfg.Logging.Level = "info"

	assert.Equal(t, "info", configureTUILogging(cfg, false).Level, "level unchanged when verbose=false")
	assert.Equal(t, "debug", configureTUILogging(cfg, true).Level, "level overridden to debug when verbose=true")
}

func TestConfigureTUILogging_DoesNotMutateConfig(t *testing.T) {
	cfg := &config.Config{}
	cfg.Logging.Output = "stdout,data/logs/javinizer.log"
	original := cfg.Logging.Output

	_ = configureTUILogging(cfg, false)

	assert.Equal(t, original, cfg.Logging.Output, "configureTUILogging must not mutate the source config (avoids session-override leakage on save)")
}

func TestConfigureTUILogging_ReturnsLoggingConfig(t *testing.T) {
	cfg := &config.Config{}
	cfg.Logging.Output = "data/logs/x.log"
	cfg.Logging.Level = "warn"
	cfg.Logging.Format = "json"

	logCfg := configureTUILogging(cfg, false)

	assert.IsType(t, &logging.Config{}, logCfg)
	assert.Equal(t, "warn", logCfg.Level)
	assert.Equal(t, "json", logCfg.Format)
	assert.Equal(t, "data/logs/x.log", logCfg.Output)
}

func TestConfigureTUILogging_EmptyOutputFallsBackToDefault(t *testing.T) {
	cfg := &config.Config{}
	cfg.Logging.Output = "" // no targets at all

	logCfg := configureTUILogging(cfg, false)

	assert.Equal(t, "data/logs/javinizer-tui.log", logCfg.Output,
		"empty config output should fall back to the TUI default file")
}

// TestResolveConfigPath verifies the TUI loads AND persists via the same path the
// rest of the app uses: JAVINIZER_CONFIG overrides the --config flag (mirrors
// root.go initConfig). Without this, SetConfigPath would persist to the flag path
// while LoadOrCreate loaded from the env path.
func TestResolveConfigPath(t *testing.T) {
	t.Setenv("JAVINIZER_CONFIG", "/custom/env-config.yaml")
	assert.Equal(t, "/custom/env-config.yaml", resolveConfigPath("configs/config.yaml"),
		"JAVINIZER_CONFIG should override the --config flag value")
}

func TestResolveConfigPath_FallsBackToFlag(t *testing.T) {
	t.Setenv("JAVINIZER_CONFIG", "")
	assert.Equal(t, "configs/config.yaml", resolveConfigPath("configs/config.yaml"),
		"empty JAVINIZER_CONFIG should fall back to the --config flag value")
}
