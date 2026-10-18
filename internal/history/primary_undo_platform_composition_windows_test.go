//go:build windows

package history

import (
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// These exercise native primitives directly. The complete cross-volume Undo
// protocol remains unsupported on Windows and its admission gate stays closed.
func TestPrimaryUndoWindowsNativeIdentityAndMutationWitness(t *testing.T) {
	path := filepath.Join(t.TempDir(), "synthetic-identity")
	require.NoError(t, os.WriteFile(path, []byte("initial!"), 0600))
	file, err := os.Open(path)
	require.NoError(t, err)
	id, info, err := primaryUndoIdentity(file)
	require.NoError(t, err)
	require.True(t, id.Strong)
	require.Len(t, id.FileID128, 32)
	before, err := primaryUndoObserveChange(file)
	require.NoError(t, err)
	time.Sleep(10 * time.Millisecond)
	require.NoError(t, os.WriteFile(path, []byte("changed!"), 0600))
	require.NoError(t, os.Chtimes(path, info.ModTime(), info.ModTime()))
	after, err := primaryUndoObserveChange(file)
	require.NoError(t, err)
	require.NotEqual(t, before, after, "native change time must expose a same-size, restamped edit")
	current, _, err := primaryUndoIdentity(file)
	require.NoError(t, err)
	require.True(t, primaryUndoSameObject(id, current))
	require.NoError(t, file.Close())
	_, err = primaryUndoObserveChange(file)
	require.Error(t, err, "a closed native handle must fail closed")
}

func TestPrimaryUndoWindowsNativePublicationCollisionAndSyncRefusal(t *testing.T) {
	fs := afero.NewOsFs()
	root := t.TempDir()
	source, dest := filepath.Join(root, "stage"), filepath.Join(root, "original")
	require.NoError(t, os.WriteFile(source, []byte("synthetic-owned"), 0600))
	require.NoError(t, os.WriteFile(dest, []byte("synthetic-foreign"), 0600))
	require.ErrorIs(t, primaryUndoPublishNoReplace(fs, source, dest), fsutil.ErrPublishCollision)
	got, err := os.ReadFile(dest)
	require.NoError(t, err)
	require.Equal(t, "synthetic-foreign", string(got))
	got, err = os.ReadFile(source)
	require.NoError(t, err)
	require.Equal(t, "synthetic-owned", string(got))
	require.NoError(t, os.Remove(dest)) // only this test's synthetic occupant
	require.NoError(t, primaryUndoPublishNoReplace(fs, source, dest))
	require.NoFileExists(t, source)
	got, err = os.ReadFile(dest)
	require.NoError(t, err)
	require.Equal(t, "synthetic-owned", string(got))
	require.ErrorContains(t, primaryUndoSyncDir(dest), "manual recovery")
}

func TestPrimaryUndoWindowsVirtualPublicationFallback(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "stage", []byte("synthetic"), 0600))
	require.NoError(t, primaryUndoPublishNoReplace(fs, "stage", "destination"))
	got, err := afero.ReadFile(fs, "destination")
	require.NoError(t, err)
	require.Equal(t, "synthetic", string(got))
	_, err = fs.Stat("stage")
	require.True(t, os.IsNotExist(err))
}
