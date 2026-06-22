package matcher

import "github.com/javinizer/javinizer-go/internal/config"

// Config holds the subset of application configuration needed by the Matcher.
type Config struct {
	RegexEnabled bool
	RegexPattern string
}

// ConfigFromAppConfig extracts Matcher-relevant fields from the application config.
//
// Config-bridge reads: cfg.Matching.RegexEnabled, cfg.Matching.RegexPattern
func ConfigFromAppConfig(cfg *config.Config) *Config {
	if cfg == nil {
		return nil
	}
	return &Config{
		RegexEnabled: cfg.Matching.RegexEnabled,
		RegexPattern: cfg.Matching.RegexPattern,
	}
}
