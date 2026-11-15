package server

import (
	"io"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

// Embedding MapFS preserves its successful Stat implementation while ReadFile
// fails, modeling an entry that exists but cannot be read.
type unreadableWebUIIndexFS struct {
	fstest.MapFS
	reads int
}

func (f *unreadableWebUIIndexFS) ReadFile(name string) ([]byte, error) {
	f.reads++
	return nil, &fs.PathError{Op: "read", Path: name, Err: fs.ErrPermission}
}

func TestLoadWebUIAssetsFromFS(t *testing.T) {
	const index = "<!doctype html><title>Offline fixture</title>"
	const icon = "icon bytes"
	unreadable := &unreadableWebUIIndexFS{MapFS: fstest.MapFS{
		"index.html":  &fstest.MapFile{Data: []byte(index)},
		"favicon.ico": &fstest.MapFile{Data: []byte(icon)},
	}}
	cases := []struct {
		name      string
		dist      fs.FS
		available bool
		index     string
	}{
		{name: "missing index", dist: fstest.MapFS{"favicon.ico": &fstest.MapFile{Data: []byte(icon)}}},
		{name: "unreadable index", dist: unreadable},
		{name: "available index", dist: fstest.MapFS{
			"index.html":  &fstest.MapFile{Data: []byte(index)},
			"favicon.ico": &fstest.MapFile{Data: []byte(icon)},
		}, available: true, index: index},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assets := loadWebUIAssetsFromFS(tc.dist)
			require.Equal(t, tc.available, assets.uiAvailable)
			require.Equal(t, tc.index, string(assets.indexHTML))
			if !tc.available {
				require.Nil(t, assets.indexHTML)
			}
			require.Equal(t, tc.dist, assets.distFS)
			require.NotNil(t, assets.staticFS)
			file, err := assets.staticFS.Open("/favicon.ico")
			require.NoError(t, err)
			defer file.Close()
			data, err := io.ReadAll(file)
			require.NoError(t, err)
			require.Equal(t, icon, string(data), "static assets remain accessible even when the UI entry point is unavailable")
		})
	}
	require.Equal(t, 1, unreadable.reads, "the existing entry must reach the read-failure boundary")
}
