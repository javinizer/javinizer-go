package fsutil

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestAferoRemoveAll(t *testing.T) {
	fs := afero.NewMemMapFs()

	require.NoError(t, fs.MkdirAll("/library/movie/extras", 0755))
	require.NoError(t, afero.WriteFile(fs, "/library/root.txt", []byte("root"), 0644))
	require.NoError(t, afero.WriteFile(fs, "/library/movie/info.nfo", []byte("movie"), 0644))
	require.NoError(t, afero.WriteFile(fs, "/library/movie/extras/trailer.url", []byte("trailer"), 0644))

	require.NoError(t, AferoRemoveAll(fs, "/library"))

	exists, err := afero.Exists(fs, "/library")
	require.NoError(t, err)
	require.False(t, exists)
}

func TestAferoRemoveAll_Nonexistent(t *testing.T) {
	fs := afero.NewMemMapFs()

	require.NoError(t, AferoRemoveAll(fs, "/missing"))
}

func TestCanonicalizePath(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "simple path", input: "/tmp/test"},
		{name: "dot segments", input: "/tmp/../tmp/./test"},
		{name: "empty string", input: ""},
		{name: "relative path", input: "relative/path"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CanonicalizePath(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			// Result should use forward slashes (normalized)
			require.NotContains(t, got, "\\", "should not contain backslashes")
		})
	}
}
