package scrape

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRun_PanicRecovery_Internal(t *testing.T) {
	cmd := NewCommand()
	movie, results, err := Run(context.Background(), cmd, []string{"TEST-001"}, "/nonexistent/config.yaml", nil)

	assert.Error(t, err, "Should return error for nonexistent config")
	assert.Nil(t, movie)
	assert.Nil(t, results)
}

func TestRun_NeverPanics_BadConfigPath(t *testing.T) {
	cmd := NewCommand()

	// Multiple scenarios that should return errors, not panics
	tests := []struct {
		name       string
		configFile string
		args       []string
	}{
		{"nonexistent config", "/nonexistent/path/config.yaml", []string{"TEST-001"}},
		{"empty config path", "", []string{"TEST-001"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This should NOT panic — Run has defer/recover
			movie, results, err := Run(context.Background(), cmd, tt.args, tt.configFile, nil)

			// Should get an error, not a panic
			assert.Error(t, err)
			assert.Nil(t, movie)
			assert.Nil(t, results)
		})
	}
}
