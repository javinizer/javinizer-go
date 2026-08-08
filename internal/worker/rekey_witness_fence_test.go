package worker

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// openFailSuffixFS wedges Open for paths with the given suffix (afero.ReadFile
// and ReadDir both funnel through Open).
type openFailSuffixFS struct {
	afero.Fs
	suffix string
}

func (f openFailSuffixFS) Open(n string) (afero.File, error) {
	if strings.HasSuffix(n, f.suffix) {
		return nil, errors.New("open wedged")
	}
	return f.Fs.Open(n)
}

// codex P2: the promotion writer names witnesses with url.PathEscape
// (.promote-A%20B.json); the rekey fence must probe the SAME encoded name or
// an unresolved promotion on an ID containing spaces slips past the fence.
func TestRekeyBlockedByEscapedPromoteWitness(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "AB 12", "")
	fs := afero.NewMemMapFs()
	dir := filepath.Join("/tmp", "posters", "JOB-9")
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".promote-AB%2012.json"), []byte("{}"), 0o644))
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9"})
	m := &LockedMovieOps{pe: pe, movieID: "AB 12"}
	err := m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "AB 13"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "promote witness unresolved", "encoded promote witness must fence the rekey")
}

// codex P2 fail-closed: a directory LISTING error (not mere absence) also
// fails the scan closed.
func TestRekeyCropWitnessScanDirErrorFailsClosed(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "SSNI-R1", "")
	base := afero.NewMemMapFs()
	dir := filepath.Join("/tmp", "posters", "JOB-9")
	require.NoError(t, base.MkdirAll(dir, 0o755))
	fs := openFailSuffixFS{Fs: base, suffix: "JOB-9"}
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9"})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	err := m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-N9"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "crop witness scan")
}
