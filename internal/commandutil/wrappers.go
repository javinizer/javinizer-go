package commandutil

import (
	"fmt"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/spf13/cobra"
)

// RunFunc is a function that accepts dependencies
type RunFunc func(*cobra.Command, []string, *Dependencies) error

// ConfigFunc is a function that only needs config
type ConfigFunc func(*cobra.Command, []string, *config.Config) error

// ConfigLoader is a function that loads configuration
type ConfigLoader func() (*config.Config, error)

// DependencyFactory creates dependencies from config
type DependencyFactory func(*config.Config) (*Dependencies, error)

// OverrideApplier applies command-specific overrides to config
type OverrideApplier func(*cobra.Command, *config.Config)

// RunWithDeps wraps a command function with dependency injection
// This is the factory that creates the actual wrapper
func RunWithDeps(
	loadConfig ConfigLoader,
	newDeps DependencyFactory,
	applyOverrides OverrideApplier,
	fn RunFunc,
) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		// Apply command-specific overrides if provided
		if applyOverrides != nil {
			applyOverrides(cmd, cfg)
			if _, err := config.Prepare(cfg); err != nil {
				return fmt.Errorf("invalid configuration after overrides: %w", err)
			}
		}

		deps, err := newDeps(cfg)
		if err != nil {
			return fmt.Errorf("failed to initialize dependencies: %w", err)
		}
		defer func() {
			if closeErr := deps.Close(); closeErr != nil {
				logging.Warnf("Failed to close dependencies: %v", closeErr)
			}
		}()

		return fn(cmd, args, deps)
	}
}

// RunWithConfig wraps a command that only needs config
func RunWithConfig(
	loadConfig ConfigLoader,
	fn ConfigFunc,
) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		return fn(cmd, args, cfg)
	}
}
