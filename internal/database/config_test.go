package database

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigFromAppConfig_Database(t *testing.T) {
	t.Run("nil config returns nil", func(t *testing.T) {
		assert.Nil(t, ConfigFromAppConfig(nil))
	})

	t.Run("returns database config", func(t *testing.T) {
		cfg := &config.Config{
			Database: config.DatabaseConfig{
				Type:     "sqlite",
				DSN:      ":memory:",
				LogLevel: "error",
			},
		}
		result := ConfigFromAppConfig(cfg)
		require.NotNil(t, result)
		assert.Equal(t, "sqlite", result.Type)
		assert.Equal(t, ":memory:", result.DSN)
	})
}
