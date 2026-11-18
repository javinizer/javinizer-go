package update

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestStatePersistencePathsKeepIdenticalFormat(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state *updateState
	}{
		{name: "nil"},
		{name: "empty", state: &updateState{}},
		{name: "complete", state: &updateState{Version: "v2.0.0", CheckedAt: "2026-11-18T00:00:00Z", Available: true, Prerelease: true, Source: UpdateSourceFresh, Error: "quoted \"error\"\nwith 日本語", ETag: "W/\"release\"", NoStableLatest: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			storePath := filepath.Join("store", "state.json")
			helperPath := filepath.Join("helper", "state.json")
			store := newStateStoreWithFs(storePath, defaultCheckInterval, fs)
			require.NoError(t, store.SaveState(tc.state))
			require.NoError(t, saveStateToFile(fs, helperPath, tc.state))
			storeBytes, err := afero.ReadFile(fs, storePath)
			require.NoError(t, err)
			helperBytes, err := afero.ReadFile(fs, helperPath)
			require.NoError(t, err)
			require.Equal(t, helperBytes, storeBytes)
			var decoded *updateState
			require.NoError(t, json.Unmarshal(storeBytes, &decoded))
			require.Equal(t, tc.state, decoded)
			require.Equal(t, tc.state, store.GetState())
			for _, path := range []string{storePath, helperPath} {
				_, err := fs.Stat(path + ".tmp")
				require.True(t, os.IsNotExist(err))
			}
		})
	}
}
