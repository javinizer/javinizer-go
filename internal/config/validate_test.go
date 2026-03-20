package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name          string
		modifyConfig  func(*Config)
		expectError   bool
		errorContains string
	}{
		{
			name: "valid default config",
			modifyConfig: func(c *Config) {
				// Use default config
			},
			expectError: false,
		},
		{
			name: "database type unsupported",
			modifyConfig: func(c *Config) {
				c.Database.Type = "postgres"
			},
			expectError:   true,
			errorContains: "database.type must be 'sqlite'",
		},
		{
			name: "database type normalized to sqlite",
			modifyConfig: func(c *Config) {
				c.Database.Type = " SQLITE "
			},
			expectError: false,
		},
		{
			name: "database dsn required",
			modifyConfig: func(c *Config) {
				c.Database.DSN = "   "
			},
			expectError:   true,
			errorContains: "database.dsn is required",
		},
		{
			name: "timeout_seconds too low",
			modifyConfig: func(c *Config) {
				c.Scrapers.TimeoutSeconds = 0
			},
			expectError:   true,
			errorContains: "timeout_seconds must be between 1 and 300",
		},
		{
			name: "timeout_seconds too high",
			modifyConfig: func(c *Config) {
				c.Scrapers.TimeoutSeconds = 400
			},
			expectError:   true,
			errorContains: "timeout_seconds must be between 1 and 300",
		},
		{
			name: "request_timeout_seconds too low",
			modifyConfig: func(c *Config) {
				c.Scrapers.RequestTimeoutSeconds = 0
			},
			expectError:   true,
			errorContains: "request_timeout_seconds must be between 1 and 600",
		},
		{
			name: "request_timeout_seconds too high",
			modifyConfig: func(c *Config) {
				c.Scrapers.RequestTimeoutSeconds = 700
			},
			expectError:   true,
			errorContains: "request_timeout_seconds must be between 1 and 600",
		},
		{
			name: "browser_timeout too low",
			modifyConfig: func(c *Config) {
				c.Scrapers.DMM.BrowserTimeout = 0
			},
			expectError:   true,
			errorContains: "browser_timeout must be between 1 and 300",
		},
		{
			name: "browser_timeout too high",
			modifyConfig: func(c *Config) {
				c.Scrapers.DMM.BrowserTimeout = 400
			},
			expectError:   true,
			errorContains: "browser_timeout must be between 1 and 300",
		},
		{
			name: "invalid referer URL",
			modifyConfig: func(c *Config) {
				c.Scrapers.Referer = "not-a-valid-url"
			},
			expectError:   true,
			errorContains: "scrapers.referer must be a valid http(s) URL with a host",
		},
		{
			name: "referer without scheme",
			modifyConfig: func(c *Config) {
				c.Scrapers.Referer = "www.example.com"
			},
			expectError:   true,
			errorContains: "scrapers.referer must be a valid http(s) URL with a host",
		},
		{
			name: "referer with ftp scheme",
			modifyConfig: func(c *Config) {
				c.Scrapers.Referer = "ftp://example.com"
			},
			expectError:   true,
			errorContains: "scrapers.referer must be a valid http(s) URL with a host",
		},
		{
			name: "max_workers too low",
			modifyConfig: func(c *Config) {
				c.Performance.MaxWorkers = 0
			},
			expectError:   true,
			errorContains: "max_workers must be between 1 and 100",
		},
		{
			name: "max_workers too high",
			modifyConfig: func(c *Config) {
				c.Performance.MaxWorkers = 150
			},
			expectError:   true,
			errorContains: "max_workers must be between 1 and 100",
		},
		{
			name: "worker_timeout too low",
			modifyConfig: func(c *Config) {
				c.Performance.WorkerTimeout = 5
			},
			expectError:   true,
			errorContains: "worker_timeout must be between 10 and 3600",
		},
		{
			name: "worker_timeout too high",
			modifyConfig: func(c *Config) {
				c.Performance.WorkerTimeout = 4000
			},
			expectError:   true,
			errorContains: "worker_timeout must be between 10 and 3600",
		},
		{
			name: "update_interval too low",
			modifyConfig: func(c *Config) {
				c.Performance.UpdateInterval = 5
			},
			expectError:   true,
			errorContains: "update_interval must be between 10 and 5000",
		},
		{
			name: "update_interval too high",
			modifyConfig: func(c *Config) {
				c.Performance.UpdateInterval = 6000
			},
			expectError:   true,
			errorContains: "update_interval must be between 10 and 5000",
		},
		{
			name: "empty referer gets default",
			modifyConfig: func(c *Config) {
				c.Scrapers.Referer = ""
			},
			expectError: false,
		},
		{
			name: "timeout_seconds at minimum valid value",
			modifyConfig: func(c *Config) {
				c.Scrapers.TimeoutSeconds = 1
			},
			expectError: false,
		},
		{
			name: "timeout_seconds at maximum valid value",
			modifyConfig: func(c *Config) {
				c.Scrapers.TimeoutSeconds = 300
			},
			expectError: false,
		},
		{
			name: "max_workers at minimum valid value",
			modifyConfig: func(c *Config) {
				c.Performance.MaxWorkers = 1
			},
			expectError: false,
		},
		{
			name: "max_workers at maximum valid value",
			modifyConfig: func(c *Config) {
				c.Performance.MaxWorkers = 100
			},
			expectError: false,
		},
		{
			name: "worker_timeout at boundaries",
			modifyConfig: func(c *Config) {
				c.Performance.WorkerTimeout = 10
			},
			expectError: false,
		},
		{
			name: "translation enabled with invalid provider",
			modifyConfig: func(c *Config) {
				c.Metadata.Translation.Enabled = true
				c.Metadata.Translation.Provider = "unknown"
			},
			expectError:   true,
			errorContains: "metadata.translation.provider must be one of",
		},
		{
			name: "translation openai missing api key allowed at startup",
			modifyConfig: func(c *Config) {
				c.Metadata.Translation.Enabled = true
				c.Metadata.Translation.Provider = "openai"
				c.Metadata.Translation.OpenAI.APIKey = ""
			},
			expectError: false,
		},
		{
			name: "translation deepl invalid mode",
			modifyConfig: func(c *Config) {
				c.Metadata.Translation.Enabled = true
				c.Metadata.Translation.Provider = "deepl"
				c.Metadata.Translation.DeepL.Mode = "invalid"
				c.Metadata.Translation.DeepL.APIKey = "k"
			},
			expectError:   true,
			errorContains: "metadata.translation.deepl.mode must be either 'free' or 'pro'",
		},
		{
			name: "translation google paid missing api key allowed at startup",
			modifyConfig: func(c *Config) {
				c.Metadata.Translation.Enabled = true
				c.Metadata.Translation.Provider = "google"
				c.Metadata.Translation.Google.Mode = "paid"
				c.Metadata.Translation.Google.APIKey = ""
			},
			expectError: false,
		},
		{
			name: "translation openai valid config",
			modifyConfig: func(c *Config) {
				c.Metadata.Translation.Enabled = true
				c.Metadata.Translation.Provider = "openai"
				c.Metadata.Translation.OpenAI.APIKey = "sk-test"
			},
			expectError: false,
		},
		{
			name: "javlibrary language valid - default",
			modifyConfig: func(c *Config) {
				c.Scrapers.JavLibrary.Language = ""
			},
			expectError: false,
		},
		{
			name: "javlibrary language valid - en",
			modifyConfig: func(c *Config) {
				c.Scrapers.JavLibrary.Language = "en"
			},
			expectError: false,
		},
		{
			name: "javlibrary language valid - ja",
			modifyConfig: func(c *Config) {
				c.Scrapers.JavLibrary.Language = "ja"
			},
			expectError: false,
		},
		{
			name: "javlibrary language valid - cn",
			modifyConfig: func(c *Config) {
				c.Scrapers.JavLibrary.Language = "cn"
			},
			expectError: false,
		},
		{
			name: "javlibrary language valid - tw",
			modifyConfig: func(c *Config) {
				c.Scrapers.JavLibrary.Language = "tw"
			},
			expectError: false,
		},
		{
			name: "javlibrary language invalid - Korean",
			modifyConfig: func(c *Config) {
				c.Scrapers.JavLibrary.Language = "ko"
			},
			expectError:   true,
			errorContains: "scrapers.javlibrary.language must be one of",
		},
		{
			name: "javlibrary language invalid - French",
			modifyConfig: func(c *Config) {
				c.Scrapers.JavLibrary.Language = "fr"
			},
			expectError:   true,
			errorContains: "scrapers.javlibrary.language must be one of",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.modifyConfig(cfg)

			err := cfg.Validate()

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
			} else {
				assert.NoError(t, err)
				// Verify default referer was set if it was empty
				if tt.name == "empty referer gets default" {
					assert.Equal(t, "https://www.dmm.co.jp/", cfg.Scrapers.Referer)
				}
				if tt.name == "database type normalized to sqlite" {
					assert.Equal(t, "sqlite", cfg.Database.Type)
				}
			}
		})
	}
}
