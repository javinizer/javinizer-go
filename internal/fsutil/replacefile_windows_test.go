//go:build windows

package fsutil

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReplaceFileWindows_AtomicReplace(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source.tmp")
	dst := filepath.Join(dir, "destination.jpg")
	fs := afero.NewOsFs()
	require.NoError(t, afero.WriteFile(fs, src, []byte("new"), 0644))
	require.NoError(t, afero.WriteFile(fs, dst, []byte("old"), 0644))

	require.NoError(t, ReplaceFile(fs, src, dst))
	got, err := afero.ReadFile(fs, dst)
	require.NoError(t, err)
	assert.Equal(t, []byte("new"), got)
}
