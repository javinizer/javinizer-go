//go:build !windows

package downloader

// POSTER-WRITE-HARDENING codex PR#215 wave-30 (P1) — the downloader rollback
// staging under the wave-30 bound publish: the directory writer's
// substitution lands between the verify and the publish (wedged at the exact
// instants: the ownership seam rides pre-verify; a publish-func wrapper is
// the verify→publish window), and the flow must end with genuine bytes at
// dest or a typed refusal with the plant never surviving — the same
// discipline history's callers now share through fsutil.PublishStagedBound.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// w30SwapPlant replays the directory writer's move against a downloader
// staging name.
func w30SwapPlant(t *testing.T, staged string) {
	t.Helper()
	require.NoError(t, os.Rename(staged, staged+".w30-away"))
	require.NoError(t, os.WriteFile(staged, []byte("foreign window plant"), 0o644))
}

// swapOwnershipW30 records/replays the ownership-seam instant (mid-flow,
// pre-verify) for the downloader's rollback staging.
func swapOwnershipW30(t *testing.T, hook func(staged string)) *[]string {
	t.Helper()
	calls := new([]string)
	prev := restoreStagingOwnershipFn
	restoreStagingOwnershipFn = func(fs afero.Fs, staged afero.File, source os.FileInfo) {
		if staged != nil {
			*calls = append(*calls, staged.Name())
		}
		prev(fs, staged, source)
		if hook != nil && staged != nil {
			hook(staged.Name())
		}
	}
	t.Cleanup(func() { restoreStagingOwnershipFn = prev })
	return calls
}

// Rollback restore (confirm-failure rollback): one mid-window substitution;
// the loop republishes the genuine backup bytes.
func TestRollbackW30_PlantedBetweenVerifyAndPublishRepublishesGenuine(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	backup := dest + ".dlbak.0123"
	require.NoError(t, os.WriteFile(dest, []byte("new-download"), 0o644))
	require.NoError(t, os.WriteFile(backup, []byte("pre-existing-bytes"), 0o640))

	attacked := 0
	wedge := func(fsys afero.Fs, src, dst string) error {
		if attacked == 0 {
			w30SwapPlant(t, src)
			attacked++
		}
		return fsutil.ReplaceFile(fsys, src, dst)
	}
	facts, err := copyBackupToDestPublish(fs, backup, dest, wedge, false, nil)
	require.NoError(t, err, "the reverify republish recovers the rollback")
	require.True(t, facts.restored.known, "the recovery loop's final published inode is the returned identity")
	require.NotNil(t, facts.copied, "the streamed backup object's binding rides back for the removal gate")
	curInfo, serr := os.Lstat(dest)
	require.NoError(t, serr)
	require.Equal(t, facts.restored.size, curInfo.Size())
	require.Equal(t, 1, attacked)
	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, "pre-existing-bytes", string(got))
	require.Equal(t, "pre-existing-bytes", string(readW30File(t, backup)),
		"the backup is unconsumed")
	entries, derr := os.ReadDir(dir)
	require.NoError(t, derr)
	for _, e := range entries {
		body, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		require.NotContains(t, string(body), "foreign window plant")
	}
}

// Re-arm direction: no-replace publish onto the vacated backup name under a
// substitution — wave-38 (finding F1): the post-publish mismatch occupant is
// NEVER displaced (plant vs legitimate gap file indistinguishable), so the
// first mismatch refuses typed through the foreign-occupant class, the plant
// stands at the backup name byte-intact, and the re-arm source is untouched.
func TestRollbackW30_RearmPlantPreservedTypedForeignOccupantRefusal(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	backup := dest + ".dlbak.4567"
	require.NoError(t, os.WriteFile(dest, []byte("dest-bytes"), 0o644))

	wedge := func(fsys afero.Fs, src, dst string) error {
		w30SwapPlant(t, src)
		return fsutil.PublishNoReplace(fsys, src, dst)
	}
	err := copyBackupToDestNoReplaceW30(fs, dest, backup, wedge)
	require.ErrorIs(t, err, fsutil.ErrPublishStagedForeignOccupant)
	require.ErrorIs(t, err, fsutil.ErrPublishStagedIdentityBreak)
	require.ErrorContains(t, err, "swap rollback")
	require.NotErrorIs(t, err, fsutil.ErrPublishStagedExhausted)
	require.Equal(t, "foreign window plant", string(readW30File(t, backup)),
		"the planted occupant is preserved at the backup name — never displaced")
	got, rerr := os.ReadFile(dest)
	require.NoError(t, rerr)
	require.Equal(t, "dest-bytes", string(got), "the re-arm source is untouched")
}

// copyBackupToDestNoReplace with a wedged publish for the attack tests.
func copyBackupToDestNoReplaceW30(fsys afero.Fs, backup, dest string, wedge func(afero.Fs, string, string) error) error {
	_, err := copyBackupToDestPublish(fsys, backup, dest, wedge, true, nil)
	return err
}

// A name swapped BEFORE the helper's own verify refuses typed through the
// rollback identity wrap; the foreign plant stays untouched, nothing is
// published or removed.
func TestRollbackW30_PreVerifyPlantRefusesIdentity(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	dest := filepath.Join(dir, "poster.jpg")
	backup := dest + ".dlbak.89ab"
	victim := filepath.Join(dir, "victim.txt")
	require.NoError(t, os.WriteFile(dest, []byte("new-download"), 0o644))
	require.NoError(t, os.WriteFile(backup, []byte("pre-existing-bytes"), 0o640))
	require.NoError(t, os.WriteFile(victim, []byte("victim"), 0o600))

	calls := swapOwnershipW30(t, func(staged string) {
		require.NoError(t, os.Rename(staged, staged+".planted-away"))
		require.NoError(t, os.Symlink(victim, staged))
	})
	err := copyBackupToDest(fs, backup, dest)
	require.ErrorIs(t, err, fsutil.ErrPublishStagedVerify)
	require.ErrorIs(t, err, fsutil.ErrStagedIdentityMismatch)
	require.True(t, strings.HasPrefix(err.Error(), "stage rollback identity:"), "got %v", err)
	require.NotEmpty(t, *calls)
	staged := (*calls)[0]
	linkInfo, lerr := os.Lstat(staged)
	require.NoError(t, lerr)
	require.NotZero(t, linkInfo.Mode()&os.ModeSymlink, "the foreign plant is never removed by the refusal")
	require.Equal(t, "new-download", string(readW30File(t, dest)))
	require.Equal(t, "pre-existing-bytes", string(readW30File(t, backup)))
}

func readW30File(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}
