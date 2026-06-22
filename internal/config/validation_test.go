package config

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestValidateFlareSolverrConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     models.FlareSolverrConfig
		wantErr bool
		errMsg  string
	}{
		{
			name:    "disabled is valid",
			cfg:     models.FlareSolverrConfig{Enabled: false},
			wantErr: false,
		},
		{
			name:    "enabled without URL returns error",
			cfg:     models.FlareSolverrConfig{Enabled: true, URL: ""},
			wantErr: true,
			errMsg:  "flaresolverr.url is required when flaresolverr is enabled",
		},
		{
			name: "enabled with all valid fields is valid",
			cfg: models.FlareSolverrConfig{
				Enabled:    true,
				URL:        "http://localhost:8191/v1",
				Timeout:    30,
				MaxRetries: 3,
				SessionTTL: 300,
			},
			wantErr: false,
		},
		{
			name: "timeout 0 returns error",
			cfg: models.FlareSolverrConfig{
				Enabled:    true,
				URL:        "http://localhost:8191/v1",
				Timeout:    0,
				MaxRetries: 3,
				SessionTTL: 300,
			},
			wantErr: true,
			errMsg:  "flaresolverr.timeout must be between 1 and 300",
		},
		{
			name: "timeout 301 returns error",
			cfg: models.FlareSolverrConfig{
				Enabled:    true,
				URL:        "http://localhost:8191/v1",
				Timeout:    301,
				MaxRetries: 3,
				SessionTTL: 300,
			},
			wantErr: true,
			errMsg:  "flaresolverr.timeout must be between 1 and 300",
		},
		{
			name: "timeout 1 is valid",
			cfg: models.FlareSolverrConfig{
				Enabled:    true,
				URL:        "http://localhost:8191/v1",
				Timeout:    1,
				MaxRetries: 3,
				SessionTTL: 300,
			},
			wantErr: false,
		},
		{
			name: "timeout 300 is valid",
			cfg: models.FlareSolverrConfig{
				Enabled:    true,
				URL:        "http://localhost:8191/v1",
				Timeout:    300,
				MaxRetries: 3,
				SessionTTL: 300,
			},
			wantErr: false,
		},
		{
			name: "max_retries -1 returns error",
			cfg: models.FlareSolverrConfig{
				Enabled:    true,
				URL:        "http://localhost:8191/v1",
				Timeout:    30,
				MaxRetries: -1,
				SessionTTL: 300,
			},
			wantErr: true,
			errMsg:  "flaresolverr.max_retries must be between 0 and 10",
		},
		{
			name: "max_retries 11 returns error",
			cfg: models.FlareSolverrConfig{
				Enabled:    true,
				URL:        "http://localhost:8191/v1",
				Timeout:    30,
				MaxRetries: 11,
				SessionTTL: 300,
			},
			wantErr: true,
			errMsg:  "flaresolverr.max_retries must be between 0 and 10",
		},
		{
			name: "max_retries 0 is valid",
			cfg: models.FlareSolverrConfig{
				Enabled:    true,
				URL:        "http://localhost:8191/v1",
				Timeout:    30,
				MaxRetries: 0,
				SessionTTL: 300,
			},
			wantErr: false,
		},
		{
			name: "max_retries 10 is valid",
			cfg: models.FlareSolverrConfig{
				Enabled:    true,
				URL:        "http://localhost:8191/v1",
				Timeout:    30,
				MaxRetries: 10,
				SessionTTL: 300,
			},
			wantErr: false,
		},
		{
			name: "session_ttl 59 returns error",
			cfg: models.FlareSolverrConfig{
				Enabled:    true,
				URL:        "http://localhost:8191/v1",
				Timeout:    30,
				MaxRetries: 3,
				SessionTTL: 59,
			},
			wantErr: true,
			errMsg:  "flaresolverr.session_ttl must be between 60 and 3600",
		},
		{
			name: "session_ttl 3601 returns error",
			cfg: models.FlareSolverrConfig{
				Enabled:    true,
				URL:        "http://localhost:8191/v1",
				Timeout:    30,
				MaxRetries: 3,
				SessionTTL: 3601,
			},
			wantErr: true,
			errMsg:  "flaresolverr.session_ttl must be between 60 and 3600",
		},
		{
			name: "session_ttl 60 is valid",
			cfg: models.FlareSolverrConfig{
				Enabled:    true,
				URL:        "http://localhost:8191/v1",
				Timeout:    30,
				MaxRetries: 3,
				SessionTTL: 60,
			},
			wantErr: false,
		},
		{
			name: "session_ttl 3600 is valid",
			cfg: models.FlareSolverrConfig{
				Enabled:    true,
				URL:        "http://localhost:8191/v1",
				Timeout:    30,
				MaxRetries: 3,
				SessionTTL: 3600,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate("flaresolverr")
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Equal(t, tt.errMsg, err.Error())
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateBrowserConfig(t *testing.T) {
	t.Run("disabled is valid", func(t *testing.T) {
		cfg := models.BrowserConfig{Enabled: false}
		assert.NoError(t, cfg.Validate("browser"))
	})

	t.Run("enabled with valid fields", func(t *testing.T) {
		cfg := models.BrowserConfig{
			Enabled:      true,
			Timeout:      30,
			MaxRetries:   3,
			WindowWidth:  1280,
			WindowHeight: 720,
			SlowMo:       100,
		}
		assert.NoError(t, cfg.Validate("browser"))
	})

	t.Run("timeout 0 returns error", func(t *testing.T) {
		cfg := models.BrowserConfig{Enabled: true, Timeout: 0, MaxRetries: 3, WindowWidth: 1280, WindowHeight: 720}
		err := cfg.Validate("browser")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "timeout")
	})

	t.Run("timeout 301 returns error", func(t *testing.T) {
		cfg := models.BrowserConfig{Enabled: true, Timeout: 301, MaxRetries: 3, WindowWidth: 1280, WindowHeight: 720}
		err := cfg.Validate("browser")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "timeout")
	})

	t.Run("max_retries -1 returns error", func(t *testing.T) {
		cfg := models.BrowserConfig{Enabled: true, Timeout: 30, MaxRetries: -1, WindowWidth: 1280, WindowHeight: 720}
		err := cfg.Validate("browser")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "max_retries")
	})

	t.Run("max_retries 11 returns error", func(t *testing.T) {
		cfg := models.BrowserConfig{Enabled: true, Timeout: 30, MaxRetries: 11, WindowWidth: 1280, WindowHeight: 720}
		err := cfg.Validate("browser")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "max_retries")
	})

	t.Run("window_width 639 returns error", func(t *testing.T) {
		cfg := models.BrowserConfig{Enabled: true, Timeout: 30, MaxRetries: 3, WindowWidth: 639, WindowHeight: 720}
		err := cfg.Validate("browser")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "window_width")
	})

	t.Run("window_height 479 returns error", func(t *testing.T) {
		cfg := models.BrowserConfig{Enabled: true, Timeout: 30, MaxRetries: 3, WindowWidth: 1280, WindowHeight: 479}
		err := cfg.Validate("browser")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "window_height")
	})

	t.Run("slow_mo 5001 returns error", func(t *testing.T) {
		cfg := models.BrowserConfig{Enabled: true, Timeout: 30, MaxRetries: 3, WindowWidth: 1280, WindowHeight: 720, SlowMo: 5001}
		err := cfg.Validate("browser")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "slow_mo")
	})

	t.Run("binary_path nonexistent returns error", func(t *testing.T) {
		cfg := models.BrowserConfig{Enabled: true, Timeout: 30, MaxRetries: 3, WindowWidth: 1280, WindowHeight: 720, BinaryPath: "/nonexistent/browser"}
		err := cfg.Validate("browser")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "binary_path")
	})
}

func TestValidateProxyProfileRef(t *testing.T) {
	profiles := map[string]models.ProxyProfile{
		"main": {URL: "http://proxy:8080"},
	}

	t.Run("nil proxy config is valid", func(t *testing.T) {
		assert.NoError(t, validateProxyProfileRef("test.proxy", nil, profiles))
	})

	t.Run("disabled proxy is valid", func(t *testing.T) {
		cfg := &models.ProxyConfig{Enabled: false}
		assert.NoError(t, validateProxyProfileRef("test.proxy", cfg, profiles))
	})

	t.Run("enabled with valid profile is valid", func(t *testing.T) {
		cfg := &models.ProxyConfig{Enabled: true, Profile: "main"}
		assert.NoError(t, validateProxyProfileRef("test.proxy", cfg, profiles))
	})

	t.Run("enabled with unknown profile returns error", func(t *testing.T) {
		cfg := &models.ProxyConfig{Enabled: true, Profile: "nonexistent"}
		err := validateProxyProfileRef("test.proxy", cfg, profiles)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unknown profile")
	})

	t.Run("enabled with empty profile is valid (inherit mode)", func(t *testing.T) {
		cfg := &models.ProxyConfig{Enabled: true, Profile: ""}
		assert.NoError(t, validateProxyProfileRef("test.proxy", cfg, profiles))
	})
}
