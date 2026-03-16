package commandutil

import (
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

// TestRunWithDeps_Success tests successful execution with dependencies
func TestRunWithDeps_Success(t *testing.T) {
	tmpDir := t.TempDir()
	_, cfg := createTestConfig(t, WithDatabaseDSN(tmpDir+"/test.db"))

	loadConfigCalled := false
	newDepsCalled := false
	fnCalled := false

	loadConfig := func() (*config.Config, error) {
		loadConfigCalled = true
		return cfg, nil
	}

	newDeps := func(c *config.Config) (*Dependencies, error) {
		newDepsCalled = true
		return &Dependencies{
			Config:          c,
			DB:              nil, // nil DB is valid for testing Close
			ScraperRegistry: nil,
		}, nil
	}

	fn := func(cmd *cobra.Command, args []string, deps *Dependencies) error {
		fnCalled = true
		assert.NotNil(t, deps)
		assert.Equal(t, cfg, deps.Config)
		return nil
	}

	runFunc := RunWithDeps(loadConfig, newDeps, nil, fn)

	cmd := &cobra.Command{}
	err := runFunc(cmd, []string{})

	assert.NoError(t, err)
	assert.True(t, loadConfigCalled)
	assert.True(t, newDepsCalled)
	assert.True(t, fnCalled)
	// Note: we cannot directly track Close calls without modifying production code
}

// TestRunWithDeps_ConfigLoadError tests error when config loading fails
func TestRunWithDeps_ConfigLoadError(t *testing.T) {
	loadConfig := func() (*config.Config, error) {
		return nil, fmt.Errorf("config load error")
	}

	newDepsCalled := false
	newDeps := func(c *config.Config) (*Dependencies, error) {
		newDepsCalled = true
		return nil, nil
	}

	fnCalled := false
	fn := func(cmd *cobra.Command, args []string, deps *Dependencies) error {
		fnCalled = true
		return nil
	}

	runFunc := RunWithDeps(loadConfig, newDeps, nil, fn)

	cmd := &cobra.Command{}
	err := runFunc(cmd, []string{})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load config")
	assert.False(t, newDepsCalled) // Should not create deps
	assert.False(t, fnCalled)      // Should not call fn
}

// TestRunWithDeps_DependencyCreationError tests error when dependency creation fails
func TestRunWithDeps_DependencyCreationError(t *testing.T) {
	tmpDir := t.TempDir()
	_, cfg := createTestConfig(t, WithDatabaseDSN(tmpDir+"/test.db"))

	loadConfig := func() (*config.Config, error) {
		return cfg, nil
	}

	newDeps := func(c *config.Config) (*Dependencies, error) {
		return nil, fmt.Errorf("dependency creation error")
	}

	fnCalled := false
	fn := func(cmd *cobra.Command, args []string, deps *Dependencies) error {
		fnCalled = true
		return nil
	}

	runFunc := RunWithDeps(loadConfig, newDeps, nil, fn)

	cmd := &cobra.Command{}
	err := runFunc(cmd, []string{})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to initialize dependencies")
	assert.False(t, fnCalled) // Should not call fn
}

// TestRunWithDeps_WithApplyOverrides tests that overrides are applied
func TestRunWithDeps_WithApplyOverrides(t *testing.T) {
	tmpDir := t.TempDir()
	_, cfg := createTestConfig(t, WithDatabaseDSN(tmpDir+"/test.db"))

	loadConfig := func() (*config.Config, error) {
		return cfg, nil
	}

	overridesApplied := false
	applyOverrides := func(cmd *cobra.Command, c *config.Config) {
		overridesApplied = true
		c.Metadata.NFO.Enabled = true // Apply some override
	}

	newDeps := func(c *config.Config) (*Dependencies, error) {
		assert.True(t, c.Metadata.NFO.Enabled, "Overrides should be applied before newDeps")
		return &Dependencies{Config: c}, nil
	}

	fn := func(cmd *cobra.Command, args []string, deps *Dependencies) error {
		return nil
	}

	runFunc := RunWithDeps(loadConfig, newDeps, applyOverrides, fn)

	cmd := &cobra.Command{}
	err := runFunc(cmd, []string{})

	assert.NoError(t, err)
	assert.True(t, overridesApplied)
}

// TestRunWithDeps_NilApplyOverrides tests that nil applyOverrides works
func TestRunWithDeps_NilApplyOverrides(t *testing.T) {
	tmpDir := t.TempDir()
	_, cfg := createTestConfig(t, WithDatabaseDSN(tmpDir+"/test.db"))

	loadConfig := func() (*config.Config, error) {
		return cfg, nil
	}

	newDeps := func(c *config.Config) (*Dependencies, error) {
		return &Dependencies{Config: c}, nil
	}

	fn := func(cmd *cobra.Command, args []string, deps *Dependencies) error {
		return nil
	}

	// applyOverrides is nil - should work fine
	runFunc := RunWithDeps(loadConfig, newDeps, nil, fn)

	cmd := &cobra.Command{}
	err := runFunc(cmd, []string{})

	assert.NoError(t, err)
}

// TestRunWithDeps_FnError tests that errors from fn are propagated
func TestRunWithDeps_FnError(t *testing.T) {
	tmpDir := t.TempDir()
	_, cfg := createTestConfig(t, WithDatabaseDSN(tmpDir+"/test.db"))

	loadConfig := func() (*config.Config, error) {
		return cfg, nil
	}

	newDeps := func(c *config.Config) (*Dependencies, error) {
		return &Dependencies{Config: c}, nil
	}

	fn := func(cmd *cobra.Command, args []string, deps *Dependencies) error {
		return fmt.Errorf("function error")
	}

	runFunc := RunWithDeps(loadConfig, newDeps, nil, fn)

	cmd := &cobra.Command{}
	err := runFunc(cmd, []string{})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "function error")
}

// TestRunWithConfig_Success tests successful execution with config only
func TestRunWithConfig_Success(t *testing.T) {
	tmpDir := t.TempDir()
	_, cfg := createTestConfig(t, WithDatabaseDSN(tmpDir+"/test.db"))

	loadConfigCalled := false
	loadConfig := func() (*config.Config, error) {
		loadConfigCalled = true
		return cfg, nil
	}

	fnCalled := false
	fn := func(cmd *cobra.Command, args []string, c *config.Config) error {
		fnCalled = true
		assert.NotNil(t, c)
		assert.Equal(t, cfg, c)
		return nil
	}

	runFunc := RunWithConfig(loadConfig, fn)

	cmd := &cobra.Command{}
	err := runFunc(cmd, []string{})

	assert.NoError(t, err)
	assert.True(t, loadConfigCalled)
	assert.True(t, fnCalled)
}

// TestRunWithConfig_ConfigLoadError tests error when config loading fails
func TestRunWithConfig_ConfigLoadError(t *testing.T) {
	loadConfig := func() (*config.Config, error) {
		return nil, fmt.Errorf("config load error")
	}

	fnCalled := false
	fn := func(cmd *cobra.Command, args []string, c *config.Config) error {
		fnCalled = true
		return nil
	}

	runFunc := RunWithConfig(loadConfig, fn)

	cmd := &cobra.Command{}
	err := runFunc(cmd, []string{})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load config")
	assert.False(t, fnCalled) // Should not call fn
}

// TestRunWithConfig_FnError tests that errors from fn are propagated
func TestRunWithConfig_FnError(t *testing.T) {
	tmpDir := t.TempDir()
	_, cfg := createTestConfig(t, WithDatabaseDSN(tmpDir+"/test.db"))

	loadConfig := func() (*config.Config, error) {
		return cfg, nil
	}

	fn := func(cmd *cobra.Command, args []string, c *config.Config) error {
		return fmt.Errorf("function error")
	}

	runFunc := RunWithConfig(loadConfig, fn)

	cmd := &cobra.Command{}
	err := runFunc(cmd, []string{})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "function error")
}
