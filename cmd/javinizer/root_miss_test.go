package main

import (
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestShouldSkipConfigInit_NilCmd tests the nil command path.
func TestShouldSkipConfigInit_NilCmd(t *testing.T) {
	assert.False(t, shouldSkipConfigInit(nil), "nil cmd should not skip config init")
}

// TestShouldSkipConfigInit_CompletionCommand tests the "completion" name path.
func TestShouldSkipConfigInit_CompletionCommand(t *testing.T) {
	cmd := &cobra.Command{Use: "completion"}
	assert.True(t, shouldSkipConfigInit(cmd), "completion command should skip config init")
}

// TestShouldSkipConfigInit_VersionFlagNotChanged tests when version flag exists but is not changed.
func TestShouldSkipConfigInit_VersionFlagNotChanged(t *testing.T) {
	cmd := &cobra.Command{Use: "scrape"}
	cmd.Flags().Bool("version", false, "show version")
	assert.False(t, shouldSkipConfigInit(cmd), "version flag not changed should not skip")
}

// TestShouldSkipConfigInit_NoVersionFlag tests a command without a version flag.
func TestShouldSkipConfigInit_NoVersionFlag(t *testing.T) {
	cmd := &cobra.Command{Use: "sort"}
	assert.False(t, shouldSkipConfigInit(cmd), "command without version flag should not skip")
}

// TestInitConfig_DownloadProxyEmptyURL tests the download proxy validation
// with an empty resolved URL — hits the "Download proxy is enabled but resolved
// profile URL is empty, disabling proxy" warning path.
func TestInitConfig_DownloadProxyEmptyURL(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "dl-proxy-empty.yaml")

	testCfg := config.DefaultConfig(nil, nil)
	testCfg.Database.DSN = filepath.Join(tmpDir, "test.db")
	testCfg.Logging.Output = filepath.Join(tmpDir, "logs")
	testCfg.Scrapers.Proxy.Enabled = true
	testCfg.Scrapers.Proxy.DefaultProfile = "main"
	testCfg.Scrapers.Proxy.Profiles = map[string]models.ProxyProfile{
		"main":     {URL: "http://proxy:8080"},
		"download": {URL: ""}, // Empty URL for download profile
	}
	testCfg.Output.Download.DownloadProxy.Enabled = true
	testCfg.Output.Download.DownloadProxy.Profile = "download" // Points to profile with empty URL

	require.NoError(t, config.Save(testCfg, configPath))

	originalCfgFile := cfgFile
	defer func() { cfgFile = originalCfgFile }()

	cfgFile = configPath
	initConfig()
	defer logging.InitLogger(&logging.Config{Level: "info", Format: "text", Output: "stdout"})
}

// TestInitConfig_DownloadProxyWithValidProfile tests the download proxy validation
// with a valid profile URL — hits the "Download proxy enabled:" info log path.
func TestInitConfig_DownloadProxyWithValidProfile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "dl-proxy-valid.yaml")

	testCfg := config.DefaultConfig(nil, nil)
	testCfg.Database.DSN = filepath.Join(tmpDir, "test.db")
	testCfg.Logging.Output = filepath.Join(tmpDir, "logs")
	testCfg.Scrapers.Proxy.Enabled = true
	testCfg.Scrapers.Proxy.DefaultProfile = "main"
	testCfg.Scrapers.Proxy.Profiles = map[string]models.ProxyProfile{
		"main":     {URL: "http://proxy:8080"},
		"download": {URL: "http://dl-proxy:8080"},
	}
	testCfg.Output.Download.DownloadProxy.Enabled = true
	testCfg.Output.Download.DownloadProxy.Profile = "download"

	require.NoError(t, config.Save(testCfg, configPath))

	originalCfgFile := cfgFile
	defer func() { cfgFile = originalCfgFile }()

	cfgFile = configPath
	initConfig()
	defer logging.InitLogger(&logging.Config{Level: "info", Format: "text", Output: "stdout"})
}

// TestInitConfig_ProxyWithCredentials tests that sanitizeProxyURL strips
// credentials from the proxy URL logged by initConfig.
func TestInitConfig_ProxyWithCredentials(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "proxy-creds.yaml")

	testCfg := config.DefaultConfig(nil, nil)
	testCfg.Database.DSN = filepath.Join(tmpDir, "test.db")
	testCfg.Logging.Output = filepath.Join(tmpDir, "logs")
	testCfg.Scrapers.Proxy.Enabled = true
	testCfg.Scrapers.Proxy.DefaultProfile = "main"
	testCfg.Scrapers.Proxy.Profiles = map[string]models.ProxyProfile{
		"main": {URL: "http://user:pass@proxy:8080"},
	}

	require.NoError(t, config.Save(testCfg, configPath))

	originalCfgFile := cfgFile
	defer func() { cfgFile = originalCfgFile }()

	cfgFile = configPath
	initConfig()
	defer logging.InitLogger(&logging.Config{Level: "info", Format: "text", Output: "stdout"})
}
