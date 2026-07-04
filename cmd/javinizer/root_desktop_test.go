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
