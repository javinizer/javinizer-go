package fsutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- MoveFileNoReplace ---

func TestMoveFileNoReplace_HappyPath(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/in/movie.mp4", []byte("content"), 0644))

	require.NoError(t, MoveFileNoReplace(fs, "/in/movie.mp4", "/out/movie.mp4"))

	content, err := afero.ReadFile(fs, "/out/movie.mp4")
	require.NoError(t, err)
	assert.Equal(t, "content", string(content))
	_, err = fs.Stat("/in/movie.mp4")
	assert.True(t, os.IsNotExist(err), "source must be consumed")
}

func TestMoveFileNoReplace_LexicalSelfNoOp(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/a.mp4", []byte("content"), 0644))

	require.NoError(t, MoveFileNoReplace(fs, "/a.mp4", "/a.mp4"))
	content, err := afero.ReadFile(fs, "/a.mp4")
	require.NoError(t, err)
	assert.Equal(t, "content", string(content))
}

func TestMoveFileNoReplace_SameInodeNoOp(t *testing.T) {
	// Adoption: the destination is a hardlink alias of the source.
	dir := t.TempDir()
	fs := afero.NewOsFs()
	src := filepath.Join(dir, "a.mp4")
	require.NoError(t, os.WriteFile(src, []byte("content"), 0644))
	require.NoError(t, os.Link(src, filepath.Join(dir, "alias.mp4")))

	require.NoError(t, MoveFileNoReplace(fs, src, filepath.Join(dir, "alias.mp4")))

	content, err := os.ReadFile(src)
	require.NoError(t, err, "source survives (idempotent no-op)")
	assert.Equal(t, "content", string(content))
	content, err = os.ReadFile(filepath.Join(dir, "alias.mp4"))
	require.NoError(t, err)
	assert.Equal(t, "content", string(content))
	names, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, names, 2, "both aliases remain")
}

func TestMoveFileNoReplace_OccupiedRefusesForeignPreserved(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/in/movie.mp4", []byte("ours"), 0644))
	require.NoError(t, afero.WriteFile(fs, "/out/movie.mp4", []byte("foreign"), 0644))

	err := MoveFileNoReplace(fs, "/in/movie.mp4", "/out/movie.mp4")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrPublishCollision, "occupied destination refuses (never overwritten)")

	content, _ := afero.ReadFile(fs, "/out/movie.mp4")
	assert.Equal(t, "foreign", string(content), "foreign destination preserved")
	content, _ = afero.ReadFile(fs, "/in/movie.mp4")
	assert.Equal(t, "ours", string(content), "source preserved after refusal")
}

func TestMoveFileNoReplace_ForeignPlantInWindow(t *testing.T) {
	// Real-Fs + seam-instrumented: the publish seams are planted right before
	// they act on dst, reproducing a foreign win inside the classification→
	// publish window. The no-replace leg (kernel rename or link fallback)
	// must refuse with the collision class, never overwrite. (Virtual fs's
	// classify-then-rename leg cannot demonstrate this; same convention as
	// the fsutil per-platform publish tests.)
	dir := t.TempDir()
	fs := afero.NewOsFs()
	inPath := filepath.Join(dir, "in.mp4")
	outPath := filepath.Join(dir, "out.mp4")
	require.NoError(t, os.WriteFile(inPath, []byte("ours"), 0644))

	plant := func() {
		if _, err := os.Stat(outPath); os.IsNotExist(err) {
			_ = os.WriteFile(outPath, []byte("foreign"), 0644)
		}
	}
	if !hookNoReplacePlantSeams(t, plant) {
		t.Skip("no publish seams to instrument on this platform")
	}

	err := MoveFileNoReplace(fs, inPath, outPath)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrPublishCollision)

	content, _ := os.ReadFile(outPath)
	assert.Equal(t, "foreign", string(content), "the plant wins the name; our content must not overwrite it")
	content, _ = os.ReadFile(inPath)
	assert.Equal(t, "ours", string(content), "our source is preserved")
}

// --- CopyFileNoReplace ---

func TestCopyFileNoReplace_HappyPath(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/in/poster.jpg", []byte("img"), 0644))

	require.NoError(t, CopyFileNoReplace(fs, "/in/poster.jpg", "/out/poster.jpg"))
	content, err := afero.ReadFile(fs, "/out/poster.jpg")
	require.NoError(t, err)
	assert.Equal(t, "img", string(content))
	content, err = afero.ReadFile(fs, "/in/poster.jpg")
	require.NoError(t, err)
	assert.Equal(t, "img", string(content), "source untouched by copy")
}

func TestCopyFileNoReplace_OccupiedRefusesForeignPreserved(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/in/poster.jpg", []byte("ours"), 0644))
	require.NoError(t, afero.WriteFile(fs, "/out/poster.jpg", []byte("foreign"), 0644))

	err := CopyFileNoReplace(fs, "/in/poster.jpg", "/out/poster.jpg")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrPublishCollision)

	content, _ := afero.ReadFile(fs, "/out/poster.jpg")
	assert.Equal(t, "foreign", string(content))
	content, _ = afero.ReadFile(fs, "/in/poster.jpg")
	assert.Equal(t, "ours", string(content))
}

func TestCopyFileNoReplace_StagingLeftoverFree(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/in/x", []byte("x"), 0644))
	require.NoError(t, CopyFileNoReplace(fs, "/in/x", "/out/x"))
	entries, err := afero.ReadDir(fs, "/out")
	require.NoError(t, err)
	require.Len(t, entries, 1, "no staging remnants at the destination")
}

func TestMoveFileNoReplace_EXDEVLeg(t *testing.T) {
	if !forceNoReplaceEXDEV(t, 1) {
		t.Skip("no publish seams to instrument on this platform")
	}
	dir := t.TempDir()
	fs := afero.NewOsFs()
	src := filepath.Join(dir, "a.mp4")
	dst := filepath.Join(dir, "sub", "a.mp4")
	require.NoError(t, os.WriteFile(src, []byte("move me"), 0644))

	require.NoError(t, MoveFileNoReplace(fs, src, dst))

	content, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, "move me", string(content))
	_, err = os.Stat(src)
	assert.True(t, os.IsNotExist(err), "identity-checked source cleanup consumed src")
	entries, err := os.ReadDir(filepath.Dir(dst))
	require.NoError(t, err)
	require.Len(t, entries, 1, "no staging remnants after EXDEV publish")
}

func TestMoveFileNoReplace_EXDEV_SourceSwapRefusesCleanup(t *testing.T) {
	// After the EXDEV publish succeeds, someone swaps the SOURCE; the
	// identity-checked cleanup must refuse (both objects survive).
	dir := t.TempDir()
	fs := afero.NewOsFs()
	src := filepath.Join(dir, "a.mp4")
	dst := filepath.Join(dir, "sub", "a.mp4")
	require.NoError(t, os.WriteFile(src, []byte("original"), 0644))

	if !forceNoReplaceEXDEV(t, 1) {
		t.Skip("no publish seams to instrument on this platform")
	}
	// First seam call = the failed same-volume publish (EXDEV). Second = the
	// staged publish; swap the source right as it lands.
	if !swapFileAfterNPublishCalls(t, 2, src, []byte("foreign swap")) {
		t.Skip("no publish seams to instrument on this platform")
	}

	err := MoveFileNoReplace(fs, src, dst)
	require.Error(t, err, "source-swap must surface as an ambiguous failure")
	assert.Contains(t, err.Error(), "source cleanup refused")
	// #224 P2: the post-publish ambiguity is marked ErrPublishCompleted so
	// callers never mis-map it as a pre-publish refusal.
	assert.True(t, PublishCompleted(err))

	content, rerr := os.ReadFile(dst)
	require.NoError(t, rerr)
	assert.Equal(t, "original", string(content), "published bytes intact")
	content, rerr = os.ReadFile(src)
	require.NoError(t, rerr)
	assert.Equal(t, "foreign swap", string(content), "swapped source never unlinked")
}
