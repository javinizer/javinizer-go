//go:build !windows

package fsutil

// POSTER-WRITE-HARDENING codex PR#215 wave-29 (P1) — the publish-time
// identity proof: O_EXCL pins the staged INODE, not its NAME. A directory
// writer can rename the staged name away inside the stage→publish window and
// plant a symlink (or a foreign file) on it; VerifyStagedIdentity compares
// the open handle's fstat with an Lstat of the name, and on any mismatch the
// publish must be refused with the typed ErrStagedIdentityMismatch — the
// planted name is FOREIGN and is never removed by the helper.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func w29StageFile(t *testing.T, fs afero.Fs, dir, suffix string) (string, afero.File) {
	t.Helper()
	staged, fh, err := CreateExclusiveStagingFile(fs, filepath.Join(dir, "poster.jpg"), suffix, 3, 0o640)
	require.NoError(t, err)
	_, err = fh.Write([]byte("staged bytes"))
	require.NoError(t, err)
	return staged, fh
}

func TestVerifyStagedIdentityW29_UntouchedNameProvesIdentity(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	staged, fh := w29StageFile(t, fs, dir, ".rstr")
	defer func() { _ = fh.Close() }()

	require.NoError(t, VerifyStagedIdentity(fs, staged, fh))
}

// The codex attack itself: the staged name is renamed away and a symlink is
// planted on it before the publish — the proof refuses typed, the planted
// link is intact, and the caller's inode (renamed away) is untouched.
func TestVerifyStagedIdentityW29_SymlinkPlantRefusesAndStaysForeign(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim.txt")
	require.NoError(t, os.WriteFile(victim, []byte("victim"), 0o600))

	staged, fh := w29StageFile(t, fs, dir, ".rstr")
	defer func() { _ = fh.Close() }()
	away := staged + ".planted-away"
	require.NoError(t, os.Rename(staged, away))
	require.NoError(t, os.Symlink(victim, staged), "the directory writer's plant")

	err := VerifyStagedIdentity(fs, staged, fh)
	require.ErrorIs(t, err, ErrStagedIdentityMismatch, "fstat(handle) vs lstat(name) diverged — refuse the publish")

	linkInfo, lerr := os.Lstat(staged)
	require.NoError(t, lerr)
	require.NotZero(t, linkInfo.Mode()&os.ModeSymlink, "the planted symlink is foreign — the helper never removes it")
	got, rerr := os.ReadFile(away)
	require.NoError(t, rerr)
	require.Equal(t, "staged bytes", string(got), "the staged inode survives under the attacker's chosen name")
}

// A foreign REGULAR file planted on the staged name (symlink-less rename
// dance) is the same identity failure.
func TestVerifyStagedIdentityW29_ForeignRegularFilePlantRefuses(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	staged, fh := w29StageFile(t, fs, dir, ".dlrstr")
	defer func() { _ = fh.Close() }()
	require.NoError(t, os.Rename(staged, staged+".away"))
	require.NoError(t, os.WriteFile(staged, []byte("foreign"), 0o644))

	err := VerifyStagedIdentity(fs, staged, fh)
	require.ErrorIs(t, err, ErrStagedIdentityMismatch)
	got, rerr := os.ReadFile(staged)
	require.NoError(t, rerr)
	require.Equal(t, "foreign", string(got), "the foreign plant is never touched")
}

// An outright-missing staged name refuses the publish WITHOUT the mismatch
// sentinel — the name may be mid-arbitration (indeterminate), never proven
// foreign, and the publish stays off either way.
func TestVerifyStagedIdentityW29_MissingNameRefusesUntyped(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	staged, fh := w29StageFile(t, fs, dir, ".rstr")
	defer func() { _ = fh.Close() }()
	require.NoError(t, os.Rename(staged, staged+".away"))

	err := VerifyStagedIdentity(fs, staged, fh)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrStagedIdentityMismatch)
	require.ErrorIs(t, err, os.ErrNotExist, "the lookup failure stays unwrap-reachable")
}

// Virtual filesystems have no rename-away/symlink threat model — the proof
// is a no-op skip even when the staged name moved (MemMapFs has no os.SameFile
// identity ground truth).
func TestVerifyStagedIdentityW29_VirtualFsSkipsProof(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/out/W29-IDENT", 0o755))
	staged, fh := w29StageFile(t, fs, "/out/W29-IDENT", ".rstr")
	defer func() { _ = fh.Close() }()
	require.NoError(t, fs.Rename(staged, staged+".away"))

	require.NoError(t, VerifyStagedIdentity(fs, staged, fh))
}
