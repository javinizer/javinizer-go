package poster

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidatePosterID_MoreCases_Uncovered(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{"valid", "abc123", false},
		{"empty", "", true},
		{"dot", ".", true},
		{"double dot", "..", true},
		{"with slash", "a/b", true},
		{"with backslash", "a\\b", true},
		{"path traversal", "../etc/passwd", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePosterID(tt.id)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateJobID_MoreCases_Uncovered(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{"valid", "job-123", false},
		{"empty", "", true},
		{"dot", ".", true},
		{"double dot", "..", true},
		{"with slash", "a/b", true},
		{"with backslash", "a\\b", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateJobID(tt.id)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidatePathWithinDir_Uncovered(t *testing.T) {
	t.Run("path inside dir", func(t *testing.T) {
		err := validatePathWithinDir("/tmp/posters/job1/poster.jpg", "/tmp/posters/job1")
		assert.NoError(t, err)
	})

	t.Run("path escapes dir", func(t *testing.T) {
		err := validatePathWithinDir("/etc/passwd", "/tmp/posters/job1")
		assert.Error(t, err)
	})

	t.Run("path traversal", func(t *testing.T) {
		err := validatePathWithinDir("/tmp/posters/job1/../../etc/passwd", "/tmp/posters/job1")
		assert.Error(t, err)
	})
}

func TestDrainAndClose_Nil_Uncovered(t *testing.T) {
	err := drainAndClose(nil)
	assert.NoError(t, err)
}

func TestMaxPosterSize_Uncovered(t *testing.T) {
	assert.Equal(t, 50<<20, maxPosterSize)
}

func TestNewPosterManager_Uncovered(t *testing.T) {
	pm := NewPosterManager(nil, "/tmp", nil)
	assert.NotNil(t, pm)
}

func TestPosterManager_WithSSRFCheck_Uncovered(t *testing.T) {
	pm := NewPosterManager(nil, "/tmp", nil)
	pm2 := pm.WithSSRFCheck(func(rawURL string) error {
		return nil
	})
	assert.NotNil(t, pm2)
	// Should be a different instance
	assert.NotSame(t, pm, pm2)
}
