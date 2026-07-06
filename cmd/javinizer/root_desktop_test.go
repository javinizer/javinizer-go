package main

import (
	"os"
	"testing"

	"github.com/javinizer/javinizer-go/internal/desktop"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInitDesktopDefault_CLIBuildKeepsRunENil verifies that in a CLI build
// (BuildDesktop="0"), initDesktopDefault leaves rootCmd.RunE unset so the
// root command prints help on no-arg launch — preserving CLI behavior.
func TestInitDesktopDefault_CLIBuildKeepsRunENil(t *testing.T) {
	orig := desktop.BuildDesktop
	desktop.BuildDesktop = "0"
	defer func() { desktop.BuildDesktop = orig }()

	origRunE := rootCmd.RunE
	defer func() { rootCmd.RunE = origRunE }()
	rootCmd.RunE = nil

	initDesktopDefault()
	assert.Nil(t, rootCmd.RunE, "CLI build must not set a no-arg Run handler")
}

// TestInitDesktopDefault_DesktopBuildSetsRunE verifies that when built as a
// desktop app (BuildDesktop="1"), initDesktopDefault wires rootCmd.RunE so
// no-arg launch opens the GUI. In a CLI-build test binary, desktop.Run is the
// app_stub.go stub, so executing the handler returns an error — proving the
// wiring is in place without needing the real Wails implementation.
func TestInitDesktopDefault_DesktopBuildSetsRunE(t *testing.T) {
	orig := desktop.BuildDesktop
	desktop.BuildDesktop = "1"
	defer func() { desktop.BuildDesktop = orig }()

	origRunE := rootCmd.RunE
	defer func() { rootCmd.RunE = origRunE }()
	rootCmd.RunE = nil

	initDesktopDefault()
	require.NotNil(t, rootCmd.RunE, "desktop build must set a no-arg Run handler")
	err := rootCmd.RunE(rootCmd, nil)
	assert.Error(t, err, "stub desktop.Run returns an error in a CLI-build test binary")
}

// TestPersistentPreRun_DesktopBuildSetsPortableConfig verifies that in a
// desktop build, the root PersistentPreRun hook switches the --config flag to
// the portable user-data path and points JAVINIZER_DB/JAVINIZER_LOG_DIR at it,
// so config/db/logs land in a writable, CWD-independent location regardless of
// how the app was launched. Uses the `version` subcommand so shouldSkipConfigInit
// short-circuits before initConfig() runs, avoiding broader config side effects.
func TestPersistentPreRun_DesktopBuildSetsPortableConfig(t *testing.T) {
	orig := desktop.BuildDesktop
	desktop.BuildDesktop = "1"
	defer func() { desktop.BuildDesktop = orig }()

	origCfg := cfgFile
	defer func() { cfgFile = origCfg }()
	cfgFile = "configs/config.yaml"

	origDB := os.Getenv("JAVINIZER_DB")
	origLog := os.Getenv("JAVINIZER_LOG_DIR")
	defer func() {
		os.Setenv("JAVINIZER_DB", origDB)
		os.Setenv("JAVINIZER_LOG_DIR", origLog)
	}()

	cmd := &cobra.Command{Use: "version"}
	cmd.Flags().String("config", "configs/config.yaml", "config file path")

	rootCmd.PersistentPreRun(cmd, nil)

	got, _ := cmd.Flags().GetString("config")
	assert.NotEqual(t, "configs/config.yaml", got, "desktop build should redirect --config to the portable path")
	assert.Contains(t, got, "Javinizer", "portable config path should live under the Javinizer user-data dir")
	assert.NotEmpty(t, os.Getenv("JAVINIZER_DB"), "desktop build should set JAVINIZER_DB to a portable path")
	assert.NotEmpty(t, os.Getenv("JAVINIZER_LOG_DIR"), "desktop build should set JAVINIZER_LOG_DIR to a portable path")
}

// TestPersistentPreRun_CLIBuildLeavesConfigDefault verifies that in a CLI
// build, PersistentPreRun does not touch the --config flag (the desktop
// portable-path redirect is desktop-only).
func TestPersistentPreRun_CLIBuildLeavesConfigDefault(t *testing.T) {
	orig := desktop.BuildDesktop
	desktop.BuildDesktop = "0"
	defer func() { desktop.BuildDesktop = orig }()

	cmd := &cobra.Command{Use: "version"}
	cmd.Flags().String("config", "configs/config.yaml", "config file path")

	rootCmd.PersistentPreRun(cmd, nil)

	got, _ := cmd.Flags().GetString("config")
	assert.Equal(t, "configs/config.yaml", got, "CLI build must leave --config at its default")
}

// TestPersistentPreRun_DesktopBuildLogsPortableEnvError covers the
// SetupPortableEnv error branch in the desktop PersistentPreRun hook: when
// the portable env cannot be set up (no home dir discoverable), the hook
// must log to stderr and continue rather than panicking. It then falls
// through to shouldSkipConfigInit, which short-circuits for the `version`
// subcommand so initConfig() is not reached.
func TestPersistentPreRun_DesktopBuildLogsPortableEnvError(t *testing.T) {
	orig := desktop.BuildDesktop
	desktop.BuildDesktop = "1"
	defer func() { desktop.BuildDesktop = orig }()

	origCfg := cfgFile
	defer func() { cfgFile = origCfg }()
	cfgFile = "configs/config.yaml"

	// Force SetupPortableEnv -> UserDataDir to fail by clearing every env var
	// os.UserConfigDir / os.UserHomeDir consult.
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	t.Setenv("AppData", "")
	t.Setenv("USERPROFILE", "")

	cmd := &cobra.Command{Use: "version"}
	cmd.Flags().String("config", "configs/config.yaml", "config file path")

	// Must not panic; the error is logged and execution continues.
	assert.NotPanics(t, func() { rootCmd.PersistentPreRun(cmd, nil) })
}

// TestPersistentPreRun_DesktopBuildCustomConfigSkipsPortableEnv verifies the
// fix for a data-integrity regression: when the user passes a custom --config,
// PersistentPreRun must NOT call SetupPortableEnv. Otherwise the injected
// JAVINIZER_DB/JAVINIZER_LOG_DIR would override that file's DB/log settings
// via ApplyEnvironmentOverrides, silently redirecting the user's data. The
// custom config path is preserved as-is.
func TestPersistentPreRun_DesktopBuildCustomConfigSkipsPortableEnv(t *testing.T) {
	orig := desktop.BuildDesktop
	desktop.BuildDesktop = "1"
	defer func() { desktop.BuildDesktop = orig }()

	origCfg := cfgFile
	defer func() { cfgFile = origCfg }()
	customPath := "/custom/path/config.yaml"
	cfgFile = customPath

	origDB := os.Getenv("JAVINIZER_DB")
	origLog := os.Getenv("JAVINIZER_LOG_DIR")
	defer func() {
		os.Setenv("JAVINIZER_DB", origDB)
		os.Setenv("JAVINIZER_LOG_DIR", origLog)
	}()
	// Poison the env vars so any SetupPortableEnv call would overwrite them; if
	// the guard works, they stay empty.
	os.Setenv("JAVINIZER_DB", "")
	os.Setenv("JAVINIZER_LOG_DIR", "")

	cmd := &cobra.Command{Use: "version"}
	cmd.Flags().String("config", customPath, "config file path")

	rootCmd.PersistentPreRun(cmd, nil)

	got, _ := cmd.Flags().GetString("config")
	assert.Equal(t, customPath, got, "custom --config must be preserved untouched on desktop builds")
	assert.Empty(t, os.Getenv("JAVINIZER_DB"), "custom --config must skip SetupPortableEnv (no JAVINIZER_DB injection)")
	assert.Empty(t, os.Getenv("JAVINIZER_LOG_DIR"), "custom --config must skip SetupPortableEnv (no JAVINIZER_LOG_DIR injection)")
}

// TestDisableMousetrapForDesktopBuild_CLIBuildNoOp verifies that in a CLI
// build (BuildDesktop="0"), disableMousetrapForDesktopBuild leaves
// cobra.MousetrapHelpText untouched — the mousetrap only applies to the
// desktop (GUI) build.
func TestDisableMousetrapForDesktopBuild_CLIBuildNoOp(t *testing.T) {
	origBuild := desktop.BuildDesktop
	origMousetrap := cobra.MousetrapHelpText
	desktop.BuildDesktop = "0"
	defer func() {
		desktop.BuildDesktop = origBuild
		cobra.MousetrapHelpText = origMousetrap
	}()

	cobra.MousetrapHelpText = "preset non-empty value"
	disableMousetrapForDesktopBuild()
	assert.Equal(t, "preset non-empty value", cobra.MousetrapHelpText,
		"CLI build must not clear the mousetrap help text")
}

// TestDisableMousetrapForDesktopBuild_DesktopBuildClears verifies that in a
// desktop build (BuildDesktop="1"), disableMousetrapForDesktopBuild clears
// cobra.MousetrapHelpText so double-click launches open the GUI instead of
// showing cobra's "this is a command line tool" dialog.
func TestDisableMousetrapForDesktopBuild_DesktopBuildClears(t *testing.T) {
	origBuild := desktop.BuildDesktop
	origMousetrap := cobra.MousetrapHelpText
	desktop.BuildDesktop = "1"
	defer func() {
		desktop.BuildDesktop = origBuild
		cobra.MousetrapHelpText = origMousetrap
	}()

	cobra.MousetrapHelpText = "This is a command line tool..."
	disableMousetrapForDesktopBuild()
	assert.Empty(t, cobra.MousetrapHelpText,
		"desktop build must clear the mousetrap help text for double-click launches")
}
