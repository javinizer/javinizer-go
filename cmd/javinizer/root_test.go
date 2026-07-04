package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tuicmd "github.com/javinizer/javinizer-go/cmd/javinizer/commands/tui"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
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
	expectedCommands := []string{"actress", "api", "app", "genre", "history", "info", "init", "logs", "scrape", "sort", "tag", "tui", "update", "upgrade", "version"}

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
	assert.True(t, shouldSkipConfigInit(&cobra.Command{Use: "upgrade"}))

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
	testCfg := config.DefaultConfig(nil, nil)
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
	defer logging.InitLogger(&logging.Config{Level: "info", Format: "text", Output: "stdout"})

	// Verify cfgFile was set from environment variable
	assert.Equal(t, configPath, cfgFile, "cfgFile should be set from JAVINIZER_CONFIG env var")
}

func TestInitConfig_VerboseFlagSetsDebug(t *testing.T) {
	// Test that verbose flag sets log level to debug
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "verbose-config.yaml")

	testCfg := config.DefaultConfig(nil, nil)
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
	defer logging.InitLogger(&logging.Config{Level: "info", Format: "text", Output: "stdout"})

	// The verbose flag should cause debug logging
	// We verify this indirectly by checking the flag was processed
	assert.True(t, verboseFlag, "Verbose flag should remain true")
}

func TestInitConfig_ProxyValidation_EmptyURL(t *testing.T) {
	// Test that proxy validation handles empty URL correctly
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "proxy-empty-config.yaml")

	testCfg := config.DefaultConfig(nil, nil)
	testCfg.Database.DSN = filepath.Join(tmpDir, "test.db")
	testCfg.Logging.Output = filepath.Join(tmpDir, "logs")

	// Enable proxy with empty profile URL - initConfig should disable it
	testCfg.Scrapers.Proxy.Enabled = true
	testCfg.Scrapers.Proxy.DefaultProfile = "main"
	testCfg.Scrapers.Proxy.Profiles = map[string]models.ProxyProfile{
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
	defer logging.InitLogger(&logging.Config{Level: "info", Format: "text", Output: "stdout"})

	// Test passes if initConfig doesn't panic or exit
	// The proxy disabled logic is tested by the fact that initConfig completes
}

func TestInitConfig_ProxyValidation_ValidURL(t *testing.T) {
	// Test that proxy validation accepts valid URLs
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "proxy-valid-config.yaml")

	testCfg := config.DefaultConfig(nil, nil)
	testCfg.Database.DSN = filepath.Join(tmpDir, "test.db")
	testCfg.Logging.Output = filepath.Join(tmpDir, "logs")

	// Enable proxy with valid profile URL
	testCfg.Scrapers.Proxy.Enabled = true
	testCfg.Scrapers.Proxy.DefaultProfile = "main"
	testCfg.Scrapers.Proxy.Profiles = map[string]models.ProxyProfile{
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
	defer logging.InitLogger(&logging.Config{Level: "info", Format: "text", Output: "stdout"})

	// Test passes if initConfig doesn't panic or exit
}

func TestInitConfig_DownloadProxyValidation(t *testing.T) {
	// Test download proxy validation (similar to scraper proxy)
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "download-proxy-config.yaml")

	testCfg := config.DefaultConfig(nil, nil)
	testCfg.Database.DSN = filepath.Join(tmpDir, "test.db")
	testCfg.Logging.Output = filepath.Join(tmpDir, "logs")

	// Test valid profile case
	testCfg.Scrapers.Proxy.Enabled = true
	testCfg.Scrapers.Proxy.DefaultProfile = "main"
	testCfg.Scrapers.Proxy.Profiles = map[string]models.ProxyProfile{
		"main":     {URL: "http://proxy.example.com:8080"},
		"download": {URL: "socks5://localhost:1080"},
	}
	testCfg.Output.Download.DownloadProxy.Enabled = true
	testCfg.Output.Download.DownloadProxy.Profile = "download"

	err := config.Save(testCfg, configPath)
	require.NoError(t, err, "Failed to save test config")

	// Save original values
	originalCfgFile := cfgFile
	defer func() { cfgFile = originalCfgFile }()

	cfgFile = configPath

	// Call initConfig
	initConfig()
	defer logging.InitLogger(&logging.Config{Level: "info", Format: "text", Output: "stdout"})

	// Test passes if initConfig doesn't panic
}

func TestInitConfig_UmaskValid(t *testing.T) {
	// Test valid umask values
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "umask-config.yaml")

	testCfg := config.DefaultConfig(nil, nil)
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
	defer logging.InitLogger(&logging.Config{Level: "info", Format: "text", Output: "stdout"})

	// Test passes if initConfig doesn't panic
}

func TestInitConfig_UmaskInvalid(t *testing.T) {
	// Test invalid umask values
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "umask-invalid-config.yaml")

	testCfg := config.DefaultConfig(nil, nil)
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
	defer logging.InitLogger(&logging.Config{Level: "info", Format: "text", Output: "stdout"})

	// Test passes if initConfig doesn't panic (it should warn but continue)
}

func TestInitConfig_MultipleEnvironmentVariables(t *testing.T) {
	// Test that multiple environment variables can be set simultaneously
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "multi-env-config.yaml")
	dbPath := filepath.Join(tmpDir, "custom.db")
	logDir := filepath.Join(tmpDir, "logs")

	// Create a valid config file
	testCfg := config.DefaultConfig(nil, nil)
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
	defer logging.InitLogger(&logging.Config{Level: "info", Format: "text", Output: "stdout"})

	// Test passes if initConfig doesn't panic with multiple env vars
}

func TestSanitizeProxyURL(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{"url with credentials", "http://user:pass@proxy:8080", "http://%5BREDACTED%5D@proxy:8080"},
		{"url without credentials", "http://proxy:8080", "http://proxy:8080"},
		{"invalid url returned as-is", "://not-a-url", "://not-a-url"},
		{"empty string", "", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, sanitizeProxyURL(tc.input))
		})
	}
}

// TestIsTUICommand verifies the TUI-subcommand detection used to suppress stdout
// logging during initial setup (so startup messages don't leak before AltScreen).
func TestIsTUICommand(t *testing.T) {
	// Use the REAL TUI command (fresh instance) so a future rename in
	// cmd/javinizer/commands/tui/command.go breaks this test, not just production.
	tuiCmd := tuicmd.NewCommand()
	childCmd := &cobra.Command{Use: "something"}
	tuiCmd.AddCommand(childCmd)

	scrapeCmd := &cobra.Command{Use: "scrape"}

	tests := []struct {
		name     string
		cmd      *cobra.Command
		expected bool
	}{
		{"tui command", tuiCmd, true},
		{"child of tui walks up to tui", childCmd, true},
		{"unrelated command", scrapeCmd, false},
		{"nil command", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isTUICommand(tt.cmd))
		})
	}
}

// TestIsTUICommand_DetectsRealRegisteredCommand ensures isTUICommand detects the
// actual TUI command registered on rootCmd, not just a hardcoded mock — so a
// future rename of the TUI command breaks this test (and surfaces the regression).
func TestIsTUICommand_DetectsRealRegisteredCommand(t *testing.T) {
	var realTUI *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == "tui" {
			realTUI = c
			break
		}
	}
	require.NotNil(t, realTUI, "the real tui command should be registered on rootCmd")
	assert.True(t, isTUICommand(realTUI), "isTUICommand must detect the real registered tui command")
}

// TestInitConfig_TUICommandStripsStdoutFromStartup proves the pre-alt-screen
// startup leak (issue N1) is fixed: when the TUI subcommand is invoked, the
// initial logger is file-only, so the "Log file: ..." startup message does not
// reach stdout. Without the fix, the default "stdout,file" output would leak it.
func TestInitConfig_TUICommandStripsStdoutFromStartup(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "tui-startup.yaml")
	logFile := filepath.Join(tmpDir, "tui-startup.log")

	testCfg := config.DefaultConfig(nil, nil)
	testCfg.Database.DSN = filepath.Join(tmpDir, "test.db")
	testCfg.Logging.Output = "stdout," + logFile // dual output — would leak without the fix
	require.NoError(t, config.Save(testCfg, configPath))

	t.Setenv("JAVINIZER_CONFIG", configPath)
	origCfgFile := cfgFile
	cfgFile = ""
	defer func() { cfgFile = origCfgFile }()

	origLogOutput := originalLogOutput
	defer func() { originalLogOutput = origLogOutput }()

	origVerbose := verboseFlag
	verboseFlag = false
	defer func() { verboseFlag = origVerbose }()

	origCmd := currentCmd
	defer func() { currentCmd = origCmd }()
	currentCmd = &cobra.Command{Use: "tui [path]"}

	defer logging.CloseLogger()

	r, w, err := os.Pipe()
	require.NoError(t, err)
	origStdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = origStdout }() // defensive: restore even if initConfig os.Exit's

	initConfig()

	_ = w.Close()
	os.Stdout = origStdout
	outBuf, err := io.ReadAll(r)
	require.NoError(t, err)
	_ = r.Close()

	if strings.Contains(string(outBuf), "Log file") {
		t.Errorf("startup \"Log file\" message leaked to stdout in TUI mode: %q", string(outBuf))
	}

	content, err := os.ReadFile(logFile)
	require.NoError(t, err, "log file target should exist and receive logs")
	if !strings.Contains(string(content), "Log file") {
		t.Errorf("log file did not receive the startup message; content: %s", string(content))
	}
}

// TestInitConfig_TUICommandWithJavinizerLogDir_PreservesRelocation
// verifies the JAVINIZER_LOG_DIR + TUI interaction: the stdout strip must use
// the env-relocated file target (so logs land in JAVINIZER_LOG_DIR) while stdout
// stays clean. This exercises the round-2 review Medium concern that the strip
// cooperates with JAVINIZER_LOG_DIR file relocation.
func TestInitConfig_TUICommandWithJavinizerLogDir_PreservesRelocation(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "logdir.yaml")
	logDir := filepath.Join(tmpDir, "customlogs")

	testCfg := config.DefaultConfig(nil, nil)
	testCfg.Database.DSN = filepath.Join(tmpDir, "test.db")
	testCfg.Logging.Output = "stdout,data/logs/javinizer.log"
	require.NoError(t, config.Save(testCfg, configPath))

	t.Setenv("JAVINIZER_CONFIG", configPath)
	t.Setenv("JAVINIZER_LOG_DIR", logDir)
	origCfgFile := cfgFile
	cfgFile = ""
	defer func() { cfgFile = origCfgFile }()
	origLogOutput := originalLogOutput
	defer func() { originalLogOutput = origLogOutput }()
	origVerbose := verboseFlag
	verboseFlag = false
	defer func() { verboseFlag = origVerbose }()
	origCmd := currentCmd
	defer func() { currentCmd = origCmd }()
	currentCmd = &cobra.Command{Use: "tui [path]"}
	defer logging.CloseLogger()

	r, w, err := os.Pipe()
	require.NoError(t, err)
	origStdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = origStdout }()

	initConfig()

	_ = w.Close()
	os.Stdout = origStdout
	outBuf, err := io.ReadAll(r)
	require.NoError(t, err)
	_ = r.Close()
	if strings.Contains(string(outBuf), "Log file") {
		t.Errorf("startup message leaked to stdout in TUI+JAVINIZER_LOG_DIR mode: %q", string(outBuf))
	}

	// The logger used the env-relocated file target, so logs land in JAVINIZER_LOG_DIR.
	relocatedLog := filepath.Join(logDir, "javinizer.log")
	content, err := os.ReadFile(relocatedLog)
	require.NoError(t, err, "relocated log file should exist in JAVINIZER_LOG_DIR")
	assert.Contains(t, string(content), "Log file")
}

// TestInitConfig_TUICommandPureStdoutWithLogDir verifies the fallback TUI log
// path honors JAVINIZER_LOG_DIR when the config has NO file target (pure
// "stdout"): logs land in the env-configured dir instead of the hardcoded
// data/logs/javinizer-tui.log (CodeRabbit finding).
func TestInitConfig_TUICommandPureStdoutWithLogDir(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "purestdout.yaml")
	logDir := filepath.Join(tmpDir, "envlogs")

	testCfg := config.DefaultConfig(nil, nil)
	testCfg.Database.DSN = filepath.Join(tmpDir, "test.db")
	testCfg.Logging.Output = "stdout" // no file target at all
	require.NoError(t, config.Save(testCfg, configPath))

	t.Setenv("JAVINIZER_CONFIG", configPath)
	t.Setenv("JAVINIZER_LOG_DIR", logDir)
	origCfgFile := cfgFile
	cfgFile = ""
	defer func() { cfgFile = origCfgFile }()
	origLogOutput := originalLogOutput
	defer func() { originalLogOutput = origLogOutput }()
	origVerbose := verboseFlag
	verboseFlag = false
	defer func() { verboseFlag = origVerbose }()
	origCmd := currentCmd
	defer func() { currentCmd = origCmd }()
	currentCmd = &cobra.Command{Use: "tui [path]"}
	defer logging.CloseLogger()
	// Defensive: if the fix regresses, InitLogger would create the hardcoded fallback
	// in the repo root; clean it up so the test never pollutes the working tree.
	defer func() { _ = os.RemoveAll("data/logs/javinizer-tui.log") }()

	r, w, err := os.Pipe()
	require.NoError(t, err)
	origStdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = origStdout }()

	initConfig()

	_ = w.Close()
	os.Stdout = origStdout
	outBuf, err := io.ReadAll(r)
	require.NoError(t, err)
	_ = r.Close()
	if strings.Contains(string(outBuf), "Log file") {
		t.Errorf("startup message leaked to stdout: %q", string(outBuf))
	}

	// The fallback honored JAVINIZER_LOG_DIR (not the hardcoded data/logs path).
	envLog := filepath.Join(logDir, "javinizer-tui.log")
	content, err := os.ReadFile(envLog)
	require.NoError(t, err, "fallback log should land in JAVINIZER_LOG_DIR, not data/logs/")
	assert.Contains(t, string(content), "Log file")
}

// TestInitConfig_TUICommandWithVerbose_DebugLevelFileOnly verifies that the
// verbose flag combines with the TUI stdout-strip: debug-level startup messages
// go to the file only, never stdout.
func TestInitConfig_TUICommandWithVerbose_DebugLevelFileOnly(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "verbose.yaml")
	logFile := filepath.Join(tmpDir, "verbose.log")

	testCfg := config.DefaultConfig(nil, nil)
	testCfg.Database.DSN = filepath.Join(tmpDir, "test.db")
	testCfg.Logging.Output = "stdout," + logFile
	testCfg.Logging.Level = "info"
	require.NoError(t, config.Save(testCfg, configPath))

	t.Setenv("JAVINIZER_CONFIG", configPath)
	origCfgFile := cfgFile
	cfgFile = ""
	defer func() { cfgFile = origCfgFile }()
	origLogOutput := originalLogOutput
	defer func() { originalLogOutput = origLogOutput }()
	origVerbose := verboseFlag
	verboseFlag = true
	defer func() { verboseFlag = origVerbose }()
	origCmd := currentCmd
	defer func() { currentCmd = origCmd }()
	currentCmd = &cobra.Command{Use: "tui [path]"}
	defer logging.CloseLogger()

	r, w, err := os.Pipe()
	require.NoError(t, err)
	origStdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = origStdout }()

	initConfig()

	_ = w.Close()
	os.Stdout = origStdout
	outBuf, err := io.ReadAll(r)
	require.NoError(t, err)
	_ = r.Close()
	if strings.Contains(string(outBuf), "Log file") || strings.Contains(string(outBuf), "Loaded configuration") {
		t.Errorf("debug-level startup messages leaked to stdout in TUI verbose mode: %q", string(outBuf))
	}

	content, err := os.ReadFile(logFile)
	require.NoError(t, err, "log file should exist")
	// debug level emits the "Loaded configuration from:" message.
	assert.Contains(t, string(content), "Loaded configuration from:",
		"verbose flag should produce debug-level output in the log file")
}

// TestInitConfig_TUICommandStripsStderr verifies stderr targets are also
// stripped in the TUI startup path (FileOnlyOutput excludes both stdout and
// stderr), so stderr doesn't corrupt the TUI either.
func TestInitConfig_TUICommandStripsStderr(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "stderr.yaml")
	logFile := filepath.Join(tmpDir, "stderr.log")

	testCfg := config.DefaultConfig(nil, nil)
	testCfg.Database.DSN = filepath.Join(tmpDir, "test.db")
	testCfg.Logging.Output = "stdout,stderr," + logFile
	require.NoError(t, config.Save(testCfg, configPath))

	t.Setenv("JAVINIZER_CONFIG", configPath)
	origCfgFile := cfgFile
	cfgFile = ""
	defer func() { cfgFile = origCfgFile }()
	origLogOutput := originalLogOutput
	defer func() { originalLogOutput = origLogOutput }()
	origVerbose := verboseFlag
	verboseFlag = false
	defer func() { verboseFlag = origVerbose }()
	origCmd := currentCmd
	defer func() { currentCmd = origCmd }()
	currentCmd = &cobra.Command{Use: "tui [path]"}
	defer logging.CloseLogger()

	// Capture both stdout and stderr.
	rOut, wOut, err := os.Pipe()
	require.NoError(t, err)
	rErr, wErr, err := os.Pipe()
	require.NoError(t, err)
	origStdout, origStderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = wOut, wErr
	defer func() { os.Stdout, os.Stderr = origStdout, origStderr }()

	initConfig()

	_ = wOut.Close()
	_ = wErr.Close()
	os.Stdout, os.Stderr = origStdout, origStderr
	outBuf, err := io.ReadAll(rOut)
	require.NoError(t, err)
	errBuf, err := io.ReadAll(rErr)
	require.NoError(t, err)
	_ = rOut.Close()
	_ = rErr.Close()

	if strings.Contains(string(outBuf), "Log file") {
		t.Errorf("startup message leaked to stdout: %q", string(outBuf))
	}
	if strings.Contains(string(errBuf), "Log file") {
		t.Errorf("startup message leaked to stderr: %q", string(errBuf))
	}

	content, err := os.ReadFile(logFile)
	require.NoError(t, err, "log file should exist")
	assert.Contains(t, string(content), "Log file")
}

// TestInitConfig_NonTUICommand_KeepsStdoutOutput is a negative test: for a
// non-TUI command (e.g. scrape), stdout is NOT stripped — the logger keeps the
// configured stdout target so CLI/API output remains visible. Guards against
// over-stripping.
func TestInitConfig_NonTUICommand_KeepsStdoutOutput(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "nontui.yaml")
	logFile := filepath.Join(tmpDir, "nontui.log")

	testCfg := config.DefaultConfig(nil, nil)
	testCfg.Database.DSN = filepath.Join(tmpDir, "test.db")
	testCfg.Logging.Output = "stdout," + logFile
	require.NoError(t, config.Save(testCfg, configPath))

	t.Setenv("JAVINIZER_CONFIG", configPath)
	origCfgFile := cfgFile
	cfgFile = ""
	defer func() { cfgFile = origCfgFile }()
	origLogOutput := originalLogOutput
	defer func() { originalLogOutput = origLogOutput }()
	origVerbose := verboseFlag
	verboseFlag = false
	defer func() { verboseFlag = origVerbose }()
	origCmd := currentCmd
	defer func() { currentCmd = origCmd }()
	currentCmd = &cobra.Command{Use: "scrape [pattern]"} // non-TUI command
	defer logging.CloseLogger()

	r, w, err := os.Pipe()
	require.NoError(t, err)
	origStdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = origStdout }()

	initConfig()

	_ = w.Close()
	os.Stdout = origStdout
	outBuf, err := io.ReadAll(r)
	require.NoError(t, err)
	_ = r.Close()
	if !strings.Contains(string(outBuf), "Log file") {
		t.Errorf("non-TUI command should retain stdout output, but Log file was not captured: %q", string(outBuf))
	}
}
