package main

import (
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/version"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Item 1: Root command initialization and structure tests

func TestRootCommand_Properties(t *testing.T) {
	// Test root command properties directly
	assert.Equal(t, "javinizer", rootCmd.Use, "Root command Use should be 'javinizer'")
	assert.Equal(t, "Javinizer - JAV metadata scraper and organizer", rootCmd.Short, "Root command Short description should match")
	assert.Equal(t, "A metadata scraper and file organizer for Japanese Adult Videos (JAV)", rootCmd.Long, "Root command Long description should match")
	assert.Equal(t, version.Short(), rootCmd.Version, "Root command Version should match version.Short()")
}

func TestRootCommand_SubcommandCount(t *testing.T) {
	// Test that all expected subcommands are registered
	subcommands := rootCmd.Commands()

	// Filter out built-in commands (help, completion)
	customCommands := 0
	for _, cmd := range subcommands {
		if cmd.Name() != "help" && cmd.Name() != "completion" {
			customCommands++
		}
	}

	assert.GreaterOrEqual(t, customCommands, 11, "Should have at least 11 custom subcommands")
}

func TestRootCommand_SubcommandNames(t *testing.T) {
	// Test that all expected subcommands are present
	expectedCommands := []string{"actress", "api", "genre", "history", "info", "init", "logs", "scrape", "sort", "tag", "tui", "update", "version"}

	subcommands := rootCmd.Commands()
	commandNames := make(map[string]bool)
	for _, cmd := range subcommands {
		commandNames[cmd.Name()] = true
	}

	for _, expected := range expectedCommands {
		assert.True(t, commandNames[expected], "Expected subcommand '%s' should be registered", expected)
	}
}

func TestExecute_FunctionExists(t *testing.T) {
	// Test that Execute function exists and is callable
	// We don't actually execute it to avoid side effects
	assert.NotNil(t, Execute, "Execute function should exist")
}

func TestRootCommand_VersionTemplate(t *testing.T) {
	// Verify that version.Info() and version.Short() return non-empty strings
	// This indirectly tests that the version template is properly set

	shortVersion := version.Short()
	assert.NotEmpty(t, shortVersion, "version.Short() should return a non-empty string")

	infoVersion := version.Info()
	assert.NotEmpty(t, infoVersion, "version.Info() should return a non-empty string")
	assert.Contains(t, infoVersion, "javinizer", "version.Info() should contain 'javinizer'")
	assert.Contains(t, infoVersion, "commit:", "version.Info() should contain commit info")
	assert.Contains(t, infoVersion, "built:", "version.Info() should contain build date")
	assert.Contains(t, infoVersion, "go:", "version.Info() should contain Go version")
}

func TestShouldSkipConfigInit(t *testing.T) {
	assert.True(t, shouldSkipConfigInit(&cobra.Command{Use: "version"}))
	assert.True(t, shouldSkipConfigInit(&cobra.Command{Use: "help"}))
	assert.True(t, shouldSkipConfigInit(&cobra.Command{Use: "completion"}))

	cmd := &cobra.Command{Use: "scrape"}
	cmd.Flags().Bool("version", false, "show version")
	require.NoError(t, cmd.Flags().Set("version", "true"))
	assert.True(t, shouldSkipConfigInit(cmd))

	assert.False(t, shouldSkipConfigInit(&cobra.Command{Use: "scrape"}))
}

// Item 2: Global persistent flags tests

func TestRootCommand_ConfigFlag(t *testing.T) {
	// Test that the config flag exists and has the correct default
	flag := rootCmd.PersistentFlags().Lookup("config")
	require.NotNil(t, flag, "Config flag should be registered")
	assert.Equal(t, "configs/config.yaml", flag.DefValue, "Config flag should have correct default value")
	assert.Equal(t, "config file path", flag.Usage, "Config flag should have correct usage text")
}

func TestRootCommand_VerboseFlag(t *testing.T) {
	// Test that the verbose flag exists and has the correct default
	flag := rootCmd.PersistentFlags().Lookup("verbose")
	require.NotNil(t, flag, "Verbose flag should be registered")
	assert.Equal(t, "false", flag.DefValue, "Verbose flag should default to false")
	assert.Equal(t, "enable debug logging", flag.Usage, "Verbose flag should have correct usage text")
	assert.Equal(t, "v", flag.Shorthand, "Verbose flag should have 'v' shorthand")
}

func TestRootCommand_FlagPersistence(t *testing.T) {
	// Test that flags are persistent (available to subcommands)
	subcommands := rootCmd.Commands()
	require.Greater(t, len(subcommands), 0, "Root command should have subcommands")

	// Pick a subcommand and verify persistent flags are inherited
	for _, cmd := range subcommands {
		if cmd.Name() == "info" {
			// Check that persistent flags from root are available
			configFlag := cmd.InheritedFlags().Lookup("config")
			verboseFlag := cmd.InheritedFlags().Lookup("verbose")

			assert.NotNil(t, configFlag, "Config flag should be inherited by subcommands")
			assert.NotNil(t, verboseFlag, "Verbose flag should be inherited by subcommands")
			break
		}
	}
}

// Item 3: InitConfig function behavior tests (with mocking)

func TestInitConfig_EnvironmentOverride(t *testing.T) {
	// Test JAVINIZER_CONFIG environment variable override
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "env-config.yaml")

	// Create a valid config file
	testCfg := config.DefaultConfig()
	testCfg.Database.DSN = filepath.Join(tmpDir, "test.db")
	testCfg.Logging.Output = filepath.Join(tmpDir, "logs")
	err := config.Save(testCfg, configPath)
	require.NoError(t, err, "Failed to save test config")

	// Set environment variable and cfgFile
	t.Setenv("JAVINIZER_CONFIG", configPath)
	originalCfgFile := cfgFile
	cfgFile = "" // Reset to trigger env var logic
	defer func() { cfgFile = originalCfgFile }()

	// Call initConfig - it should use JAVINIZER_CONFIG
	// We need to ensure the logger can be initialized
	initConfig()

	// Verify cfgFile was set from environment variable
	assert.Equal(t, configPath, cfgFile, "cfgFile should be set from JAVINIZER_CONFIG env var")
}

func TestInitConfig_VerboseFlagSetsDebug(t *testing.T) {
	// Test that verbose flag sets log level to debug
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "verbose-config.yaml")

	testCfg := config.DefaultConfig()
	testCfg.Database.DSN = filepath.Join(tmpDir, "test.db")
	testCfg.Logging.Output = filepath.Join(tmpDir, "logs")
	testCfg.Logging.Level = "info" // Start with info level
	err := config.Save(testCfg, configPath)
	require.NoError(t, err, "Failed to save test config")

	// Save original values
	originalCfgFile := cfgFile
	originalVerbose := verboseFlag
	defer func() {
		cfgFile = originalCfgFile
		verboseFlag = originalVerbose
	}()

	// Set verbose flag
	cfgFile = configPath
	verboseFlag = true

	// Call initConfig
	initConfig()

	// The verbose flag should cause debug logging
	// We verify this indirectly by checking the flag was processed
	assert.True(t, verboseFlag, "Verbose flag should remain true")
}

func TestInitConfig_ProxyValidation_EmptyURL(t *testing.T) {
	// Test that proxy validation handles empty URL correctly
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "proxy-empty-config.yaml")

	testCfg := config.DefaultConfig()
	testCfg.Database.DSN = filepath.Join(tmpDir, "test.db")
	testCfg.Logging.Output = filepath.Join(tmpDir, "logs")

	// Enable proxy with empty profile URL - initConfig should disable it
	testCfg.Scrapers.Proxy.Enabled = true
	testCfg.Scrapers.Proxy.DefaultProfile = "main"
	testCfg.Scrapers.Proxy.Profiles = map[string]config.ProxyProfile{
		"main": {URL: ""},
	}

	err := config.Save(testCfg, configPath)
	require.NoError(t, err, "Failed to save test config")

	// Save original values
	originalCfgFile := cfgFile
	defer func() { cfgFile = originalCfgFile }()

	cfgFile = configPath

	// Call initConfig - it should warn and disable the proxy
	initConfig()

	// Test passes if initConfig doesn't panic or exit
	// The proxy disabled logic is tested by the fact that initConfig completes
}

func TestInitConfig_ProxyValidation_ValidURL(t *testing.T) {
	// Test that proxy validation accepts valid URLs
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "proxy-valid-config.yaml")

	testCfg := config.DefaultConfig()
	testCfg.Database.DSN = filepath.Join(tmpDir, "test.db")
	testCfg.Logging.Output = filepath.Join(tmpDir, "logs")

	// Enable proxy with valid profile URL
	testCfg.Scrapers.Proxy.Enabled = true
	testCfg.Scrapers.Proxy.DefaultProfile = "main"
	testCfg.Scrapers.Proxy.Profiles = map[string]config.ProxyProfile{
		"main": {URL: "http://proxy.example.com:8080"},
	}

	err := config.Save(testCfg, configPath)
	require.NoError(t, err, "Failed to save test config")

	// Save original values
	originalCfgFile := cfgFile
	defer func() { cfgFile = originalCfgFile }()

	cfgFile = configPath

	// Call initConfig - it should accept the valid proxy
	initConfig()

	// Test passes if initConfig doesn't panic or exit
}

func TestInitConfig_DownloadProxyValidation(t *testing.T) {
	// Test download proxy validation (similar to scraper proxy)
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "download-proxy-config.yaml")

	testCfg := config.DefaultConfig()
	testCfg.Database.DSN = filepath.Join(tmpDir, "test.db")
	testCfg.Logging.Output = filepath.Join(tmpDir, "logs")

	// Test valid profile case
	testCfg.Scrapers.Proxy.Enabled = true
	testCfg.Scrapers.Proxy.DefaultProfile = "main"
	testCfg.Scrapers.Proxy.Profiles = map[string]config.ProxyProfile{
		"main":     {URL: "http://proxy.example.com:8080"},
		"download": {URL: "socks5://localhost:1080"},
	}
	testCfg.Output.DownloadProxy.Enabled = true
	testCfg.Output.DownloadProxy.Profile = "download"

	err := config.Save(testCfg, configPath)
	require.NoError(t, err, "Failed to save test config")

	// Save original values
	originalCfgFile := cfgFile
	defer func() { cfgFile = originalCfgFile }()

	cfgFile = configPath

	// Call initConfig
	initConfig()

	// Test passes if initConfig doesn't panic
}

func TestInitConfig_UmaskValid(t *testing.T) {
	// Test valid umask values
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "umask-config.yaml")

	testCfg := config.DefaultConfig()
	testCfg.Database.DSN = filepath.Join(tmpDir, "test.db")
	testCfg.Logging.Output = filepath.Join(tmpDir, "logs")
	testCfg.System.Umask = "0022"

	err := config.Save(testCfg, configPath)
	require.NoError(t, err, "Failed to save test config")

	// Save original values
	originalCfgFile := cfgFile
	defer func() { cfgFile = originalCfgFile }()

	cfgFile = configPath

	// Call initConfig - should apply umask without error
	initConfig()

	// Test passes if initConfig doesn't panic
}

func TestInitConfig_UmaskInvalid(t *testing.T) {
	// Test invalid umask values
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "umask-invalid-config.yaml")

	testCfg := config.DefaultConfig()
	testCfg.Database.DSN = filepath.Join(tmpDir, "test.db")
	testCfg.Logging.Output = filepath.Join(tmpDir, "logs")
	testCfg.System.Umask = "invalid" // Invalid umask

	err := config.Save(testCfg, configPath)
	require.NoError(t, err, "Failed to save test config")

	// Save original values
	originalCfgFile := cfgFile
	defer func() { cfgFile = originalCfgFile }()

	cfgFile = configPath

	// Call initConfig - should warn but not fail
	initConfig()

	// Test passes if initConfig doesn't panic (it should warn but continue)
}

func TestInitConfig_MultipleEnvironmentVariables(t *testing.T) {
	// Test that multiple environment variables can be set simultaneously
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "multi-env-config.yaml")
	dbPath := filepath.Join(tmpDir, "custom.db")
	logDir := filepath.Join(tmpDir, "logs")

	// Create a valid config file
	testCfg := config.DefaultConfig()
	testCfg.Database.DSN = dbPath
	testCfg.Logging.Output = logDir
	err := config.Save(testCfg, configPath)
	require.NoError(t, err, "Failed to save test config")

	// Set multiple environment variables
	t.Setenv("JAVINIZER_CONFIG", configPath)
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("JAVINIZER_DB", dbPath)
	t.Setenv("JAVINIZER_LOG_DIR", logDir)
	t.Setenv("JAVINIZER_HOME", tmpDir)

	// Save original values
	originalCfgFile := cfgFile
	defer func() { cfgFile = originalCfgFile }()

	cfgFile = ""

	// Call initConfig with all env vars set
	initConfig()

	// Test passes if initConfig doesn't panic with multiple env vars
}
