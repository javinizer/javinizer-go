package downloader

// POSTER-WRITE-HARDENING codex local review round 4 (PR#215 finding F3) —
// the downloader rollback's staged-cleanup arm: a publish failure carrying
// fsutil.ErrPublishCompleted (wave-33's POSIX hard-link-fallback staged
// cleanup refusal fsutil.ErrPublishNoReplaceStagedUnverified, joined into the
// completed class, or wave-20's cleanup+rollback double failure) proves the
// DESTINATION already carries the staged bytes while fsutil DELIBERATELY left
// the staged name in place — it may now address a foreign object swapped on
// inside the link→cleanup window. copyBackupToDestPublish's publish-error arm
// used to unlink the staged name unconditionally (deleting the possibly
// foreign object); it now removes only staged copies whose publish provably
// installed NOTHING (collision/unsupported refusals, plain failures), keeps
// the "swap rollback" wrap with both sentinels unwrap-reachable, and leaves
// the caller's pending-kind routing (rollbackRearmPendingKind → clean for the
// completed class) unchanged.

import (
	"bytes"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

// w34RollbackStagedNames lists the ".dlrstr." staged leftovers in dir.
func w34RollbackStagedNames(t *testing.T, fs afero.Fs, dir string) []string {
	t.Helper()
	entries, err := afero.ReadDir(fs, dir)
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		if strings.Contains(e.Name(), ".dlrstr.") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// The completed-carrying class keeps the staged name byte-intact.
func TestCopyBackupToDestPublishW34_PublishCompletedKeepsStagedName(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/w34-dl", 0o755))
	backup := "/w34-dl/poster.jpg.dlbak.0123456789abcdef"
	dest := "/w34-dl/poster.jpg"
	require.NoError(t, afero.WriteFile(base, backup, []byte("rollback bytes"), 0o644))

	pubErr := errors.Join(fsutil.ErrPublishNoReplaceStagedUnverified, fsutil.ErrPublishCompleted)
	stub := func(afero.Fs, string, string) error { return pubErr }

	var logs bytes.Buffer
	restoreLog := logging.SetOutput(&logs)
	defer restoreLog()

	_, err := copyBackupToDestPublish(base, backup, dest, stub, true, nil)
	require.Error(t, err)
	require.ErrorContains(t, err, "swap rollback")
	require.ErrorIs(t, err, fsutil.ErrPublishCompleted)
	require.ErrorIs(t, err, fsutil.ErrPublishNoReplaceStagedUnverified)

	staged := w34RollbackStagedNames(t, base, "/w34-dl")
	require.Len(t, staged, 1,
		"the possibly-foreign staged name is LEFT in place — the pre-wave-34 arm unlinked it")
	require.Equal(t, "rollback bytes", string(mustReadDownloaderW7(t, base, "/w34-dl/"+staged[0])))
	require.Equal(t, "rollback bytes", string(mustReadDownloaderW7(t, base, backup)),
		"the rollback source backup is untouched")
	require.Contains(t, logs.String(), "left in place")
	require.Equal(t, models.RestorePendingKindClean, rollbackRearmPendingKind(err),
		"the caller's routing still reads the completed class as an OWNED name (the pending retry reaps it)")
}

// Control: publish failures that prove NOTHING was installed keep dropping
// the caller's own staged copy (the pre-wave-34 cleanup), so no staging
// litter accumulates on refusals/plain failures.
func TestCopyBackupToDestPublishW34_NonCompletedFailuresDropStagedCopy(t *testing.T) {
	for _, tc := range []struct {
		name   string
		pubErr error
	}{
		{"collision refusal", fsutil.ErrPublishCollision},
		{"unsupported refusal", fsutil.ErrPublishNoReplaceUnsupported},
		{"plain publish failure", errors.New("w34 rollback publish wedged")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := afero.NewMemMapFs()
			require.NoError(t, base.MkdirAll("/w34-dlc", 0o755))
			backup := "/w34-dlc/poster.jpg.dlbak.0123456789abcdef"
			dest := "/w34-dlc/poster.jpg"
			require.NoError(t, afero.WriteFile(base, backup, []byte("rollback bytes"), 0o644))
			stub := func(afero.Fs, string, string) error { return tc.pubErr }

			_, err := copyBackupToDestPublish(base, backup, dest, stub, true, nil)
			require.ErrorContains(t, err, "swap rollback")
			require.NotErrorIs(t, err, fsutil.ErrPublishCompleted)
			require.Empty(t, w34RollbackStagedNames(t, base, "/w34-dlc"),
				"an unpublished staged copy is still dropped — only the completed class retains")
			require.Equal(t, "rollback bytes", string(mustReadDownloaderW7(t, base, backup)))
		})
	}
}
