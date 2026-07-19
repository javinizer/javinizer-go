package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrepareRuntime_NilConfig(t *testing.T) {
	changed, err := PrepareRuntime(nil)
	assert.False(t, changed)
	assert.NoError(t, err)
}

func TestPrepareRuntime_ValidConfig(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	changed, err := PrepareRuntime(cfg)
	assert.NoError(t, err)
	assert.False(t, changed)
}

func TestPrepareRuntime_InvalidConfigReturnsError(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	cfg.Database.DSN = ""
	cfg.Database.Type = "postgres"
	_, err := PrepareRuntime(cfg)
	require.Error(t, err)
}
