package config

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestActressCandidateRetentionDefault(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	require.Equal(t, 30, cfg.Metadata.ActressDatabase.CandidateRetentionDays)
	loaded, err := Load(filepath.Join("..", "..", "configs", "config.yaml.example"))
	require.NoError(t, err)
	require.Equal(t, 30, loaded.Metadata.ActressDatabase.CandidateRetentionDays)
}
