package scanner

import "github.com/javinizer/javinizer-go/internal/config"

// Config holds the subset of application configuration needed by the Scanner.
type Config struct {
	Extensions      []string
	MinSizeMB       int
	ExcludePatterns []string
}

// ConfigFromAppConfig extracts Scanner-relevant fields from the application config.
//
// Config-bridge reads: cfg.Matching.Extensions, cfg.Matching.MinSizeMB, cfg.Matching.ExcludePatterns
func ConfigFromAppConfig(cfg *config.Config) *Config {
	if cfg == nil {
		return nil
	}
	return &Config{
		Extensions:      cfg.Matching.Extensions,
		MinSizeMB:       cfg.Matching.MinSizeMB,
		ExcludePatterns: cfg.Matching.ExcludePatterns,
	}
}
