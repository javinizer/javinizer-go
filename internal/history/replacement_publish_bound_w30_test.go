//go:build !windows

package history

// POSTER-WRITE-HARDENING codex PR#215 wave-30 (P1) — the staged-identity
// bind ACROSS the publish, through the real history callers: the directory
// writer's move lands BETWEEN the helper's verify and the path publish
// (wedged into the publish function/seam — the deterministic mid-window
// instant), and every flow must end with the GENUINE bytes at the
// destination (reverify republish) or a typed refusal with nothing consumed
// and the foreign plant never surviving.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// w30SwapPlant renames the staged name away and plants foreign bytes on it —
// the directory writer's substitution, replayed at the publish instant.
func w30SwapPlant(t *testing.T, staged string) {
	t.Helper()
	require.NoError(t, os.Rename(staged, staged+".w30-away"))
	require.NoError(t, os.WriteFile(staged, []byte("foreign window plant"), 0o644))
}

// Restore (rollback direction): one mid-window substitution; the loop
// republishes the genuine backup bytes over whatever the swap installed.
func TestRestoreW30_PlantedBetweenVerifyAndPublishRepublishesGenuine(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, os.WriteFile(dest, []byte("current-bytes"), 0o644))
	require.NoError(t, os.WriteFile(backup, []byte("original-bytes"), 0o640))

	attacked := 0
	wedge := func(fsys afero.Fs, src, dst string) error {
		if attacked == 0 {
			w30SwapPlant(t, src)
			attacked++
		}
		return fsutil.ReplaceFile(fsys, src, dst)
	}
	_, err := copyRestoreBytesPublish(fs, backup, dest, wedge, false, nil)
	require.NoError(t, err, "the mismatch is recovered — the republish puts the genuine bytes over the plant")
	require.Equal(t, 1, attacked)
	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, "original-bytes", string(got), "dest holds the GENUINE backup bytes after the reverify republish")
	require.Equal(t, "original-bytes", string(mustReadW29File(t, backup)),
		"the backup is unconsumed — consumption remains the caller's success-only step")
	entries, derr := os.ReadDir(dir)
	require.NoError(t, derr)
	for _, e := range entries {
		body, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		require.NotContains(t, string(body), "foreign window plant", "the plant never survives")
	}
}

// Restore, no-replace sweep direction under wave-38 (finding F1): the
// substitution's plant — moved onto dest BY the publish — is a post-publish
// SUCCESS→mismatch occupant with no pre-publish existence tie, so recovery
// is REFUSED TYPED at the first mismatch through the foreign-occupant class
// (wrapped in "swap staged restore"), the plant stands at dest byte-intact
// (never deleted — indistinguishable from a legitimate publish→reverify gap
// file), and the backup is retained unconsumed for the caller.
func TestRestoreW30_PersistentPlantExhaustsTyped(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	backup := dest + ".dlbak." + p3HexB
	require.NoError(t, os.WriteFile(backup, []byte("original-bytes"), 0o640))

	wedge := func(fsys afero.Fs, src, dst string) error {
		w30SwapPlant(t, src)
		return fsutil.PublishNoReplace(fsys, src, dst)
	}
	_, err := copyRestoreBytesPublish(fs, backup, dest, wedge, true, nil)
	require.ErrorIs(t, err, fsutil.ErrPublishStagedForeignOccupant)
	require.ErrorIs(t, err, fsutil.ErrPublishStagedIdentityBreak)
	require.ErrorContains(t, err, "swap staged restore")
	require.NotErrorIs(t, err, fsutil.ErrPublishStagedExhausted,
		"the refusal lands at the first mismatch — the budget never ratchets")
	require.Equal(t, "foreign window plant", string(mustReadW29File(t, dest)),
		"the post-publish occupant is preserved byte-intact — never displaced")
	require.Equal(t, "original-bytes", string(mustReadW29File(t, backup)),
		"the backup is retained on the typed refusal")
}

// Re-arm direction through the REAL flow (rearmPublishFn seam carries the
// attack) under wave-38 (finding F1): the plant the publish moved onto the
// backup name is never displaced — a legitimate publish→reverify gap file
// would be indistinguishable — so the substitution answers a typed
// foreign-occupant refusal, the backup name keeps the (foreign) plant
// byte-intact, and the destination the re-arm staged is untouched. The
// wedge classifies rearm-refused exactly like the other unproven-name
// failures.
func TestRearmW30_PlantedBetweenVerifyAndPublishPreservesForeignPlant(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, os.WriteFile(dest, []byte("rearm-bytes"), 0o664))
	info, err := os.Stat(dest)
	require.NoError(t, err)

	attacked := 0
	prevPub := rearmPublishFn
	rearmPublishFn = func(fsys afero.Fs, src, dst string) error {
		if attacked == 0 {
			w30SwapPlant(t, src)
			attacked++
		}
		return prevPub(fsys, src, dst)
	}
	t.Cleanup(func() { rearmPublishFn = prevPub })

	err = rearmReplacementBackup(fs, dest, backup, info)
	require.ErrorIs(t, err, fsutil.ErrPublishStagedForeignOccupant)
	require.ErrorIs(t, err, fsutil.ErrPublishStagedIdentityBreak)
	require.Equal(t, 1, attacked)
	require.Equal(t, models.RestorePendingKindRearmRefused, rearmPendingKind(err),
		"an identity break proves nothing about the backup name — rearm-refused like every pre-publish failure")
	got, rerr := os.ReadFile(backup)
	require.NoError(t, rerr)
	require.Equal(t, "foreign window plant", string(got),
		"the planted backup-name occupant is preserved byte-intact — never displaced")
	require.Equal(t, "rearm-bytes", string(mustReadW29File(t, dest)),
		"the re-arm source (the live destination) is untouched")
}

// Re-arm under a persistent substitution (wave-38): the FIRST post-publish
// mismatch already refuses typed through the foreign-occupant class (no
// displacement, no budget ratchet) — classifying exactly like every other
// unproven-name failure (rearm-refused) — and the planted occupant survives
// at the backup name byte-intact.
func TestRearmW30_PersistentPlantRefusesAndClassifiesRearmRefused(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	backup := dest + ".dlbak." + p3HexB
	require.NoError(t, os.WriteFile(dest, []byte("rearm-bytes"), 0o664))
	info, err := os.Stat(dest)
	require.NoError(t, err)

	prevPub := rearmPublishFn
	rearmPublishFn = func(fsys afero.Fs, src, dst string) error {
		w30SwapPlant(t, src)
		return prevPub(fsys, src, dst)
	}
	t.Cleanup(func() { rearmPublishFn = prevPub })

	err = rearmReplacementBackup(fs, dest, backup, info)
	require.ErrorIs(t, err, fsutil.ErrPublishStagedForeignOccupant)
	require.ErrorIs(t, err, fsutil.ErrPublishStagedIdentityBreak)
	require.ErrorContains(t, err, "re-arm install backup")
	require.False(t, fsutil.PublishRefusal(err))
	require.False(t, fsutil.PublishCompleted(err))
	require.Equal(t, models.RestorePendingKindRearmRefused, rearmPendingKind(err),
		"an identity break proves nothing about the backup name — same class as every pre-publish failure")
	log := mustReadW29File(t, backup)
	require.Equal(t, "foreign window plant", string(log), "the plant is preserved at the backup name")
	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, "rearm-bytes", string(got), "the re-arm source is untouched")
}

// The downloader-relevant claim through the same wrap: the helper identity
// refusal text stays on its own wrap (never the install/close one).
func TestRestoreW30_VerifyRefusalKeepsIdentityWrap(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, os.WriteFile(dest, []byte("current-bytes"), 0o644))
	require.NoError(t, os.WriteFile(backup, []byte("original-bytes"), 0o640))
	victim := filepath.Join(dir, "victim.txt")
	require.NoError(t, os.WriteFile(victim, []byte("victim"), 0o600))

	calls := swapRestoreOwnershipW8(t, func(h ownershipHandoffW8) {
		require.NoError(t, os.Rename(h.staged, h.staged+".planted-away"))
		require.NoError(t, os.Symlink(victim, h.staged))
	})
	err := copyRestoreBytes(fs, backup, dest)
	require.ErrorIs(t, err, fsutil.ErrPublishStagedVerify)
	require.ErrorIs(t, err, fsutil.ErrStagedIdentityMismatch)
	require.True(t, strings.HasPrefix(err.Error(), "stage restore identity:"), "got %v", err)
	require.Len(t, *calls, 1)
}
