package database

import "github.com/javinizer/javinizer-go/internal/config"

// Config holds the subset of application configuration needed by the Database.
// Directly aliases config.DatabaseConfig — the bridge is a field-for-field copy
// with no transformation, so aliasing eliminates the stale-copy risk.
type Config = config.DatabaseConfig

// ConfigFromAppConfig extracts Database-relevant fields from the application config.
//
// Config-bridge reads: cfg.Database.Type, cfg.Database.DSN, cfg.Database.LogLevel
func ConfigFromAppConfig(cfg *config.Config) *Config {
	if cfg == nil {
		return nil
	}
	return &cfg.Database
}
