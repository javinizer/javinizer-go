package history

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// W15 covers the explicit reverter's durable destination ownership claim. The
// sweep contract is skip/keep for a live marker; the explicit restore path
// surfaces that refusal as an error so its operation remains retryable.
func TestRestoreReplacementJournalW15_LiveBusyRefusesDestination(t *testing.T) {
	fixture := newP3Fixture()
	op, dest := fixture.addAppliedOp(t, "job-w15", "W15-LIVE", false, "new", p3Replacement{seq: 1, backupBytes: "old"})

	// The parent is a separate live process; the durable marker is the only
	// cross-process signal this fixture needs, so no process-local lock is held.
	writeW15Busy(t, fixture.fs, dest, os.Getppid(), time.Now())

	restored, err := NewReverter(fixture.fs, fixture.repo).restoreReplacementJournal(context.Background(), op)
	require.ErrorIs(t, err, fsutil.ErrReplacementBusy)
	require.Empty(t, restored)
	require.Equal(t, "new", p3ReadFile(t, fixture.fs, dest), "a live owner keeps the destination untouched")
	_, err = fixture.fs.Stat(dest + ".dlbak.a")
	require.NoError(t, err, "the live owner's backup remains armed")
	row, err := fixture.repo.FindByID(context.Background(), op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Len(t, gf.Replacements, 1, "a busy refusal does not consume the journal")

	require.NoError(t, fixture.fs.Remove(fsutil.ReplacementBusyPath(dest)))
	_, err = fixture.fs.Stat(fsutil.ReplacementBusyPath(dest))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestRestoreReplacementJournalW15_ReleasedOrDeadMarkerProceeds(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*testing.T, afero.Fs, string)
	}{
		{
			name: "released",
			setup: func(t *testing.T, fs afero.Fs, dest string) {
				release, err := fsutil.AcquireReplacementBusy(fs, dest)
				require.NoError(t, err)
				release()
			},
		},
		{
			name: "dead-before-boot",
			setup: func(t *testing.T, fs afero.Fs, dest string) {
				writeW15Busy(t, fs, dest, os.Getpid(), time.Now().Add(-time.Hour))
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newP3Fixture()
			op, dest := fixture.addAppliedOp(t, "job-w15", "W15-PROCEED", false, "new", p3Replacement{seq: 1, backupBytes: "old"})
			tc.setup(t, fixture.fs, dest)

			restored, err := NewReverter(fixture.fs, fixture.repo).restoreReplacementJournal(context.Background(), op)
			require.NoError(t, err)
			require.True(t, restored[dest])
			require.Equal(t, "old", p3ReadFile(t, fixture.fs, dest))
			_, err = fixture.fs.Stat(dest + ".dlbak.a")
			require.ErrorIs(t, err, os.ErrNotExist, "successful restore consumes the backup")
			_, err = fixture.fs.Stat(fsutil.ReplacementBusyPath(dest))
			require.ErrorIs(t, err, os.ErrNotExist, "the reverter releases its marker")
			row, err := fixture.repo.FindByID(context.Background(), op.ID)
			require.NoError(t, err)
			gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
			require.NoError(t, err)
			require.Empty(t, gf.Replacements)
		})
	}
}

func TestRestoreReplacementJournalW15_MarkerClaimErrorFailsClosed(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	fixture := &p3Fixture{fs: base, repo: repo}
	op, dest := fixture.addAppliedOp(t, "job-w15", "W15-ERROR", false, "new", p3Replacement{seq: 1, backupBytes: "old"})
	markerErr := errors.New("w15 marker claim wedged")
	fs := &w15BusyClaimErrorFs{Fs: base, err: markerErr}

	restored, err := NewReverter(fs, repo).restoreReplacementJournal(context.Background(), op)
	require.ErrorIs(t, err, markerErr)
	require.Empty(t, restored)
	require.Equal(t, "new", p3ReadFile(t, base, dest), "marker arbitration failure must not install bytes")
	_, err = base.Stat(dest + ".dlbak.a")
	require.NoError(t, err, "marker arbitration failure must retain the backup")
	_, err = base.Stat(fsutil.ReplacementBusyPath(dest))
	require.ErrorIs(t, err, os.ErrNotExist, "a failed claim must not leave a marker")
	row, err := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Len(t, gf.Replacements, 1, "marker arbitration failure must not consume the journal")
}

func TestRestoreReplacementJournalW15_MarkerHeldThroughConsumptionAndReleased(t *testing.T) {
	fixture := newP3Fixture()
	op, dest := fixture.addAppliedOp(t, "job-w15", "W15-ORDER", false, "new", p3Replacement{seq: 1, backupBytes: "old"})
	repo := &w15ObserveUpdateRepo{p3OpRepo: fixture.repo, fs: fixture.fs, dest: dest}

	restored, err := NewReverter(fixture.fs, repo).restoreReplacementJournal(context.Background(), op)
	require.NoError(t, err)
	require.True(t, restored[dest])
	require.True(t, repo.updateSawBusy, "the marker must remain through journal consumption")
	_, err = fixture.fs.Stat(fsutil.ReplacementBusyPath(dest))
	require.ErrorIs(t, err, os.ErrNotExist, "the marker is released after restore and consumption")
}

func writeW15Busy(t *testing.T, fs afero.Fs, dest string, pid int, created time.Time) {
	t.Helper()
	content := fmt.Sprintf("pid=%d,time=%d", pid, created.UnixNano())
	require.NoError(t, afero.WriteFile(fs, fsutil.ReplacementBusyPath(dest), []byte(content), 0o600))
}

type w15BusyClaimErrorFs struct {
	afero.Fs
	err error
}

func (f *w15BusyClaimErrorFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if strings.HasSuffix(filepath.ToSlash(name), fsutil.ReplacementBusySuffix) {
		return nil, f.err
	}
	return f.Fs.OpenFile(name, flag, perm)
}

// w15ObserveUpdateRepo observes whether the durable busy marker is still held
// when the journal consumption lands; that persistence moved from Update to
// UpdateJournalInTx (review 4960250562), so the observation point follows.
type w15ObserveUpdateRepo struct {
	*p3OpRepo
	fs            afero.Fs
	dest          string
	updateSawBusy bool
}

func (r *w15ObserveUpdateRepo) UpdateJournalInTx(ctx context.Context, id uint, fn database.JournalUpdateFn) error {
	if _, err := r.fs.Stat(fsutil.ReplacementBusyPath(r.dest)); err == nil {
		r.updateSawBusy = true
	}
	return r.p3OpRepo.UpdateJournalInTx(ctx, id, fn)
}
