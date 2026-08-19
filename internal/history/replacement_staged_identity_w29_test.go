//go:build !windows

package history

// POSTER-WRITE-HARDENING codex PR#215 wave-29 (P1) — the re-arm/restore
// staging flow under a mid-flow NAME SWAP: the O_EXCL staging creation pins
// the inode, not the name, so a directory writer can rename the staged name
// away and plant a symlink on it inside the copy→publish window. Metadata
// now runs through the open handle (wave-29 fsutil legs) and the path-based
// publish may run only after fsutil.VerifyStagedIdentity proves the staged
// name still addresses the handle's inode. These tests drive the plant
// through the ownership-seam hook — the exact mid-flow instant between copy
// and publish — and pin the refusal: typed identity error, no publish, the
// planted name untouched.

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// The re-arm refuses BEFORE the publish seam fires: the planted symlink at
// the staged name stays byte-intact, the backup name never materializes, and
// the destination is unmolested.
func TestRearmW29_SymlinkPlantOnStagedNameRefusesBeforePublish(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	backup := dest + ".dlbak." + p3HexA
	victim := filepath.Join(dir, "victim.txt")
	require.NoError(t, os.WriteFile(dest, []byte("rearm-bytes"), 0o664))
	require.NoError(t, os.WriteFile(victim, []byte("victim"), 0o600))
	info, err := os.Stat(dest)
	require.NoError(t, err)

	// The ownership seam hook executes inside the staging window — after the
	// copy, before the identity proof — and performs the directory writer's
	// rename-away + symlink plant on the staged name.
	calls := swapRestoreOwnershipW8(t, func(h ownershipHandoffW8) {
		require.NoError(t, os.Rename(h.staged, h.staged+".planted-away"))
		require.NoError(t, os.Symlink(victim, h.staged))
	})
	publishes := 0
	prevPub := rearmPublishFn
	rearmPublishFn = func(fsys afero.Fs, src, dst string) error {
		publishes++
		return prevPub(fsys, src, dst)
	}
	t.Cleanup(func() { rearmPublishFn = prevPub })

	err = rearmReplacementBackup(fs, dest, backup, info)
	require.ErrorIs(t, err, fsutil.ErrStagedIdentityMismatch,
		"the publish-time identity proof refuses the swapped staged name")
	require.Equal(t, 0, publishes, "no publish ever ran against the foreign name")
	require.Len(t, *calls, 1, "the ownership leg ran inside the window")

	staged := (*calls)[0].staged
	linkInfo, lerr := os.Lstat(staged)
	require.NoError(t, lerr)
	require.NotZero(t, linkInfo.Mode()&os.ModeSymlink,
		"the planted symlink is foreign — the refusal leg never removes the staged name")
	_, serr := os.Lstat(backup)
	require.ErrorIs(t, serr, os.ErrNotExist, "no backup was ever published")
	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, "rearm-bytes", string(got), "the re-arm source is untouched")
	away, rerr := os.ReadFile(staged + ".planted-away")
	require.NoError(t, rerr)
	require.Equal(t, "rearm-bytes", string(away), "the staged inode survives under the attacker's name")

	// The failed publish classifies exactly like every other pre-publish
	// failure: the name is unproven (rearm-refused), never owned.
	require.False(t, fsutil.PublishRefusal(err))
	require.False(t, fsutil.PublishCompleted(err))
	require.Equal(t, models.RestorePendingKindRearmRefused, rearmPendingKind(err))
}

// The same swap plant on the RESTORE staging (copyRestoreBytesPublish —
// rollback direction) refuses through the same proof, typed identically.
func TestRestoreW29_SymlinkPlantOnStagedNameRefusesBeforeSwap(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	backup := dest + ".dlbak." + p3HexB
	victim := filepath.Join(dir, "victim.txt")
	require.NoError(t, os.WriteFile(dest, []byte("current-bytes"), 0o644))
	require.NoError(t, os.WriteFile(backup, []byte("original-bytes"), 0o640))
	require.NoError(t, os.WriteFile(victim, []byte("victim"), 0o600))

	calls := swapRestoreOwnershipW8(t, func(h ownershipHandoffW8) {
		require.NoError(t, os.Rename(h.staged, h.staged+".planted-away"))
		require.NoError(t, os.Symlink(victim, h.staged))
	})

	err := copyRestoreBytes(fs, backup, dest)
	require.ErrorIs(t, err, fsutil.ErrStagedIdentityMismatch)
	require.Len(t, *calls, 1)

	staged := (*calls)[0].staged
	linkInfo, lerr := os.Lstat(staged)
	require.NoError(t, lerr)
	require.NotZero(t, linkInfo.Mode()&os.ModeSymlink)
	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, "current-bytes", string(got), "the destination is untouched by the refused swap")
	away, rerr := os.ReadFile(staged + ".planted-away")
	require.NoError(t, rerr)
	require.Equal(t, "original-bytes", string(away), "the staged restore copy survives under the attacker's name")
	require.Equal(t, "original-bytes", string(mustReadW29File(t, backup)),
		"the backup is unconsumed — the caller's retry posture holds")
}

func mustReadW29File(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

// Classifier pin: the wave-29 identity failure AND the wave-29 link-failure
// classes both resolve to the rearm-refused pending kind (nothing is proven
// about the name), matching rearmPendingKind's trichotomy.
func TestRearmPendingKindW29_IdentityAndLinkFailureClassesAreRearmRefused(t *testing.T) {
	require.Equal(t, models.RestorePendingKindRearmRefused,
		rearmPendingKind(fmt.Errorf("re-arm stage backup identity /b: %w",
			fsutil.ErrStagedIdentityMismatch)),
		"a swapped staged name proves nothing about the backup name — refused")
	require.Equal(t, models.RestorePendingKindRearmRefused,
		rearmPendingKind(fmt.Errorf("re-arm install backup /b: %w",
			fsutil.ErrPublishNoReplaceLinkFailed)),
		"a fail-closed link error (EMLINK & friends) never installed anything — refused")
}
