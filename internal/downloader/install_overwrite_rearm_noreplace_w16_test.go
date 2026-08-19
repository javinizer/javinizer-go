package downloader

// POSTER-WRITE-HARDENING codex PR#215 wave-16 (P2) — "refuse occupied
// downloader backup names when re-arming": the rollback that prompts a
// ReleaseReplacement re-arm REMOVED the journal's verified backup first, so
// any object occupying the backup name afterwards is foreign. The pre-wave-16
// re-arm treated a Stat-success there as success — arming the journal entry
// against those unrelated bytes, which a later revert/sweep would restore
// over the destination and then delete — and its staged publish would
// replace a racer claiming the name inside the copy window. The re-arm now
// refusES occupied names (typed fsutil.ErrPublishCollision, foreign bytes
// intact, journal entry kept in its prior armed/pending state by the re-arm
// itself) and publishes its staged copy with fsutil.PublishNoReplace. These
// tests replay both claim timings through installOverwriting's ReplaceFile-
// rollback leg.
//
// Wave-19 (codex P2) refines the journal outcome of exactly these refusals:
// the caller (markRollbackRearmFailed — wave-21 renamed it when the
// conversion generalized to EVERY re-arm failure class) converts the
// still-armed entry into the rearm-refused RESTORE-PENDING state so a later
// revert consumes it without ever touching the occupied name — the
// byte-safety assertions below are unchanged (see
// install_overwrite_pending_w19_test.go for the refusal-kind pins and
// install_overwrite_rearm_pending_w21_test.go for the generalization).

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
)

var errW16StagedInstall = errors.New("w16 staged install wedged")

// w16RearmRaceFs drives the ReplaceFile-failure rollback with a foreign
// claim on the backup name: rollbackAt claims it inside Rename right after
// the rollback restore consumed the real backup (Stat at re-arm time sees it
// occupied → refusal shortcut); stageAt claims it inside OpenFile when the
// re-arm's staged copy is created (the no-replace publish collides). Exactly
// one leg is armed per test.
type w16RearmRaceFs struct {
	afero.Fs
	dest     string
	foreign  []byte
	rollback bool
	stage    bool
	fired    bool
}

func (f *w16RearmRaceFs) Rename(oldname, newname string) error {
	if filepath.Clean(newname) == filepath.Clean(f.dest) && strings.HasSuffix(filepath.Base(oldname), ".tmp") {
		return errW16StagedInstall // force the ReplaceFile-failure rollback leg
	}
	if f.rollback && !f.fired && filepath.Clean(newname) == filepath.Clean(f.dest) && strings.Contains(filepath.Base(oldname), ".dlbak.") {
		// The rollback restore (backup → dest) succeeded — its rename CONSUMED
		// the backup, so claim the just-vacated name with foreign bytes.
		if err := f.Fs.Rename(oldname, newname); err != nil {
			return err
		}
		f.fired = true
		return afero.WriteFile(f.Fs, oldname, f.foreign, 0o600)
	}
	return f.Fs.Rename(oldname, newname)
}

func (f *w16RearmRaceFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	file, err := f.Fs.OpenFile(name, flag, perm)
	if err == nil && f.stage && !f.fired && strings.Contains(filepath.Base(name), ".dlrstr.") {
		// The re-arm staging name derives from its publish target: claim the
		// backup name with foreign bytes AFTER the re-arm's Stat said ENOENT.
		f.fired = true
		target := strings.Split(name, ".dlrstr.")[0]
		if wErr := afero.WriteFile(f.Fs, target, f.foreign, 0o600); wErr != nil {
			return nil, wErr
		}
	}
	return file, err
}

func w16InstallOverwritingRollback(t *testing.T, fs afero.Fs, dir string, firedRecorder **covW14BReleaseFailingLedger) error {
	t.Helper()
	dest := filepath.Join(dir, "poster.jpg")
	staged := filepath.Join(dir, "poster.tmp")
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, dest, []byte("original bytes"), 0o640))
	require.NoError(t, afero.WriteFile(fs, staged, []byte("new bytes"), 0o644))

	recorder := &covW14BReleaseFailingLedger{armedTestLedger: &armedTestLedger{}, releaseErr: covW14BReleaseErr}
	*firedRecorder = recorder
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	_, _, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w16-rearm-race", recorder: recorder})
	return err
}

func w16CaptureLogging(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	restore := logging.SetOutput(&buf)
	t.Cleanup(restore)
	return &buf
}

// A foreign object ALREADY at the backup name when the re-arm runs is
// refused at the classification: the journal entry stays armed against
// its prior state, nothing is staged, and both files keep their bytes.
func TestInstallOverwritingW16_RearmRefusesForeignOccupiedBackupName(t *testing.T) {
	logs := w16CaptureLogging(t)
	base := afero.NewMemMapFs()
	dir := "/out/W16-REARM-OCCUPIED"
	dest := filepath.Join(dir, "poster.jpg")
	fs := &w16RearmRaceFs{Fs: base, dest: dest, foreign: []byte("foreign bytes"), rollback: true}

	var recorder *covW14BReleaseFailingLedger
	err := w16InstallOverwritingRollback(t, fs, dir, &recorder)
	require.ErrorIs(t, err, errW16StagedInstall, "the rollback error stays the surfaced failure")
	require.Equal(t, 1, recorder.releases)
	require.True(t, fs.fired, "the injected foreign claim fired")

	records := recorder.get()
	require.Len(t, records, 1, "the journal entry is kept in its prior armed state, not consumed")
	backup := records[0].backupPath

	require.Equal(t, "foreign bytes", string(mustReadDownloaderW7(t, base, backup)),
		"the foreign object at the backup name is never accepted or clobbered")
	require.Equal(t, "original bytes", string(mustReadDownloaderW7(t, base, dest)),
		"the restored destination bytes are untouched")
	entries, readErr := afero.ReadDir(base, dir)
	require.NoError(t, readErr)
	for _, e := range entries {
		require.NotContains(t, e.Name(), ".dlrstr.", "no staged re-arm copy was left behind")
	}
	require.Contains(t, logs.String(), "refused", "the refusal reason reaches the log seam")
	require.Contains(t, logs.String(), "journal entry remains armed")
}

// A foreign claim landing INSIDE the re-arm copy→publish window (after the
// Stat said the name was free) collides at the no-replace publish: same
// kept posture, and the staged re-arm copy is cleaned up.
func TestInstallOverwritingW16_RearmPublishCollisionKeepsForeignBytes(t *testing.T) {
	logs := w16CaptureLogging(t)
	base := afero.NewMemMapFs()
	dir := "/out/W16-REARM-COLLIDE"
	dest := filepath.Join(dir, "poster.jpg")
	fs := &w16RearmRaceFs{Fs: base, dest: dest, foreign: []byte("window racer bytes"), stage: true}

	var recorder *covW14BReleaseFailingLedger
	err := w16InstallOverwritingRollback(t, fs, dir, &recorder)
	require.ErrorIs(t, err, errW16StagedInstall)
	require.Equal(t, 1, recorder.releases)
	require.True(t, fs.fired, "the injected mid-window claim fired")

	records := recorder.get()
	require.Len(t, records, 1, "the journal entry stays armed/pending")
	backup := records[0].backupPath

	require.Equal(t, "window racer bytes", string(mustReadDownloaderW7(t, base, backup)),
		"the mid-window foreign bytes survive the publish collision")
	require.Equal(t, "original bytes", string(mustReadDownloaderW7(t, base, dest)))
	entries, readErr := afero.ReadDir(base, dir)
	require.NoError(t, readErr)
	for _, e := range entries {
		require.NotContains(t, e.Name(), ".dlrstr.", "the staged re-arm copy is cleaned up on collision")
	}
	require.Contains(t, logs.String(), "re-arm of rolled-back backup")
}
