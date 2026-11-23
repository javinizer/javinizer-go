package commandutil

import (
	"fmt"

	"github.com/javinizer/javinizer-go/internal/config"
)

// LoadCLIConfig loads or creates the persisted config, reapplies runtime
// environment overrides, and prepares the result after command flags. Flags
// take precedence over the environment; the callback does not run on load
// failure. prepareContext preserves the command's validation error prefix.
func LoadCLIConfig(path, prepareContext string, applyFlags func(*config.Config)) (*config.Config, error) {
	cfg, err := config.LoadOrCreate(path)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	config.ApplyEnvironmentOverrides(cfg)
	if applyFlags != nil {
		applyFlags(cfg)
	}
	if _, err := config.Prepare(cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", prepareContext, err)
	}
	return cfg, nil
}
