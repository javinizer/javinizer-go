package poster

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAssetRevision pins the token shape: mtime-nanoseconds + "-" + size —
// opaque to clients, but deterministic per file generation so the crop
// endpoint can detect a same-URL cache refresh.
func TestAssetRevision(t *testing.T) {
	fs := afero.NewMemMapFs()
	p := "a.jpg"
	require.NoError(t, afero.WriteFile(fs, p, []byte("poster-bytes"), 0o644))
	fi, err := fs.Stat(p)
	require.NoError(t, err)

	rev := AssetRevision(fi)
	assert.Equal(t, rev, AssetRevision(fi), "token must be deterministic for the same generation")
	assert.Contains(t, rev, "-")
	assert.Equal(t, "", AssetRevision(nil))

	// A content refresh that changes the byte count must change the token
	// even when the path (source URL identity) stays the same.
	require.NoError(t, afero.WriteFile(fs, p, []byte("poster-bytes-with-more"), 0o644))
	fi2, err := fs.Stat(p)
	require.NoError(t, err)
	assert.NotEqual(t, rev, AssetRevision(fi2))
}

func TestFullSourceRevision(t *testing.T) {
	fs := afero.NewMemMapFs()
	pm := NewPosterManager(fs, "temproot", nil)
	posterDir := filepath.Join("temproot", "posters", "job-1")
	require.NoError(t, fs.MkdirAll(posterDir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(posterDir, "ABC-001-full.jpg"), []byte("full-source"), 0o644))

	rev, err := pm.FullSourceRevision("job-1", "ABC-001")
	require.NoError(t, err)
	require.NotEmpty(t, rev)

	// Same generation → same token.
	rev2, err := pm.FullSourceRevision("job-1", "ABC-001")
	require.NoError(t, err)
	assert.Equal(t, rev, rev2)

	// Same-URL refresh (bytes replaced under the same filename) → new token.
	require.NoError(t, afero.WriteFile(fs, filepath.Join(posterDir, "ABC-001-full.jpg"), []byte("full-source-REFRESHED"), 0o644))
	rev3, err := pm.FullSourceRevision("job-1", "ABC-001")
	require.NoError(t, err)
	assert.NotEqual(t, rev, rev3)

	// Missing file → error (the crop endpoint maps this to 409 for a
	// presented revision: the generation the client measured is gone).
	_, err = pm.FullSourceRevision("job-1", "MISSING-001")
	require.Error(t, err)
	assert.True(t, os.IsNotExist(err))

	// Input validation parity with the other manager methods. A traversal
	// jobID fails ValidateJobID (a merely unusual-but-safe name like
	// "bad job" would pass validation and only fail at Stat).
	_, err = pm.FullSourceRevision("../evil", "ABC-001")
	require.Error(t, err)
	_, err = pm.FullSourceRevision("job-1", "../etc")
	require.Error(t, err)
}
