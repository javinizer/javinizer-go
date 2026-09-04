package organizer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
)

// destMaybeLstat must work through a wrapper that doesn't implement
// afero.Lstater (reports didLstat=false); it falls back on plain Stat.
type plainLstatFs struct{ afero.Fs }

func TestLidPin_DestMaybeLstat_WrappedPlane(t *testing.T) {
	dir := t.TempDir()
	fs := afero.NewOsFs()
	fp := filepath.Join(dir, "x.mp4")
	require.NoError(t, os.WriteFile(fp, []byte("v"), 0o644))
	wrapped := plainLstatFs{Fs: fs}
	info, followed, err := destMaybeLstat(wrapped, fp)
	require.NoError(t, err)
	_ = followed
	assert.False(t, info.IsDir())
}

// Authorized LinkModeNone copy onto a symlink destination must refuse (never
// silently replacing it) — even when classification happens in-flow at execute.
func TestLidPin_AuthorizedMeCopyOntoSymlinkRefused(t *testing.T) {
	dir := t.TempDir()
	fs := afero.NewOsFs()
	src := filepath.Join(dir, "in", "V.mp4")
	require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, os.WriteFile(src, []byte("v"), 0o644))
	foreign := filepath.Join(dir, "foreign.mp4")
	require.NoError(t, os.WriteFile(foreign, []byte("foreign"), 0o644))
	dst := filepath.Join(dir, "out", "V.mp4")
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, os.Symlink(foreign, dst))

	strategy := newOrganizeStrategy(plainStatFs{Fs: fs}, &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}, nil, &MemLinker{})
	plan := &OrganizePlan{
		Match:      models.FileMatchInfo{Path: src, Name: "V.mp4", Extension: ".mp4", MovieID: "V"},
		SourcePath: src,
		TargetDir:  filepath.Dir(dst),
		TargetFile: "V.mp4",
		TargetPath: dst,
		WillMove:   true,
		moveFiles:  false,
		// Force run the authorized lane so execute classifies the symlink dest
		// rather than plan conflicts.
		Conflicts:           []PlanConflict{},
		overwriteAuthorized: true,
	}
	_, err := strategy.Execute(plan)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "regular file")
	// Symlink object must be intact.
	info, lerr := os.Lstat(dst)
	require.NoError(t, lerr)
	assert.NotZero(t, info.Mode()&os.ModeSymlink)
}
