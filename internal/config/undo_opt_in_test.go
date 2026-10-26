package config

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestUndoOptInDefaultAndPersistence(t *testing.T) {
	require.False(t, DefaultConfig(nil, nil).Output.Operation.AllowRevert)
	for _, tc := range []struct {
		name, yaml string
		want       bool
	}{
		{"omitted", "output: {}\n", false},
		{"disabled", "output:\n  allow_revert: false\n", false},
		{"enabled", "output:\n  allow_revert: true\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := afero.NewOsFs()
			cs := NewConfigStorage(fs, nil)
			path := filepath.Join(t.TempDir(), "settings.yaml")
			require.NoError(t, fs.MkdirAll(filepath.Dir(path), 0755))
			require.NoError(t, afero.WriteFile(fs, path, []byte(tc.yaml), 0600))
			cfg, err := cs.Load(path)
			require.NoError(t, err)
			require.Equal(t, tc.want, cfg.Output.Operation.AllowRevert)
			require.NoError(t, cs.Save(cfg, path))
			cfg, err = cs.Load(path)
			require.NoError(t, err)
			require.Equal(t, tc.want, cfg.Output.Operation.AllowRevert)
			require.NoError(t, cs.SaveSparse(cfg, path, BuildSparseSaveContext()))
			cfg, err = cs.Load(path)
			require.NoError(t, err)
			require.Equal(t, tc.want, cfg.Output.Operation.AllowRevert)
			wire, err := json.Marshal(cfg.Output)
			require.NoError(t, err)
			var decoded OutputConfig
			require.NoError(t, json.Unmarshal(wire, &decoded))
			require.Equal(t, tc.want, decoded.Operation.AllowRevert)
		})
	}
}
