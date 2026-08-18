package history

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// W18B regression coverage: backup mtime is not an in-flight signal. The
// durable destination-adjacent marker is the only cross-process arbitration
// signal; absent/dead markers are claimable and live/malformed markers retain.
func TestReplacementSweepCovW18B_FreshBackupWithoutMarkerIsProcessed(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dir := "/out/W18B-NOMARKER"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, fs.MkdirAll(dir, 0o755))

	// Construct the long-lived sweeper before the cross-process backup is
	// created. Its future mtime is deliberately newer than that construction.
	sweeper := NewReplacementSweeper(fs, repo)
	writeSweepFile(t, fs, backup, "pre-crash", -time.Minute)
	journalRow(t, repo, "job-w18b", "W18B-NOMARKER", dest, backup, 1, models.RevertStatusApplied)

	healed, err := sweeper.SweepDestinations(ctx, []string{dest})
	require.NoError(t, err)
	require.Equal(t, 1, healed, "an absent busy marker makes a fresh targeted backup eligible")
	require.Equal(t, "pre-crash", string(mustRead2(t, fs, dest)))
	_, err = fs.Stat(backup)
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = fs.Stat(fsutil.ReplacementBusyPath(dest))
	require.ErrorIs(t, err, os.ErrNotExist, "the sweeper releases its temporary claim")
}

func TestReplacementSweepCovW18B_FreshBackupWithLiveMarkerIsSkipped(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dir := "/out/W18B-LIVE"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexB
	require.NoError(t, fs.MkdirAll(dir, 0o755))

	sweeper := NewReplacementSweeper(fs, repo)
	writeSweepFile(t, fs, backup, "in-flight", -time.Minute)
	writeW18BBusy(t, fs, dest, os.Getpid(), time.Now())
	journalRow(t, repo, "job-w18b", "W18B-LIVE", dest, backup, 1, models.RevertStatusApplied)

	healed, err := sweeper.SweepDestinations(ctx, []string{dest})
	require.NoError(t, err)
	require.Equal(t, 0, healed, "a live durable marker keeps a fresh targeted backup untouched")
	require.Equal(t, "in-flight", string(mustRead2(t, fs, backup)))
	_, err = fs.Stat(dest)
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = fs.Stat(fsutil.ReplacementBusyPath(dest))
	require.NoError(t, err, "the live owner's marker remains")
}

func TestReplacementSweepCovW18B_FreshBackupWithDeadMarkerIsProcessed(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dir := "/out/W18B-DEAD"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexC
	require.NoError(t, fs.MkdirAll(dir, 0o755))

	sweeper := NewReplacementSweeper(fs, repo)
	writeSweepFile(t, fs, backup, "dead-owner", -time.Minute)
	deadAt := time.Now()
	if runtime.GOOS == "windows" {
		deadAt = deadAt.Add(-time.Hour)
	}
	writeW18BBusy(t, fs, dest, 999999999, deadAt)
	journalRow(t, repo, "job-w18b", "W18B-DEAD", dest, backup, 1, models.RevertStatusApplied)

	healed, err := sweeper.Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, healed, "a dead durable marker is reclaimed even for a fresh backup")
	require.Equal(t, "dead-owner", string(mustRead2(t, fs, dest)))
	_, err = fs.Stat(backup)
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = fs.Stat(fsutil.ReplacementBusyPath(dest))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestReplacementSweepCovW18B_FreshBackupMarkerClaimErrorRetains(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dir := "/out/W18B-ERROR"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll(dir, 0o755))
	writeSweepFile(t, base, backup, "claim-error", -time.Minute)
	journalRow(t, repo, "job-w18b", "W18B-ERROR", dest, backup, 1, models.RevertStatusApplied)

	fs := afero.NewReadOnlyFs(base)
	healed, err := NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, healed, "busy-marker claim errors fail closed")
	require.Equal(t, "claim-error", string(mustRead2(t, base, backup)))
	_, err = base.Stat(dest)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestReplacementSweepCovW18B_FreshBackupWithMalformedMarkerIsRetained(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dir := "/out/W18B-MALFORMED"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexB
	marker := fsutil.ReplacementBusyPath(dest)
	require.NoError(t, fs.MkdirAll(dir, 0o755))

	sweeper := NewReplacementSweeper(fs, repo)
	writeSweepFile(t, fs, backup, "malformed-owner", -time.Minute)
	require.NoError(t, afero.WriteFile(fs, marker, []byte("partial"), 0o600))
	journalRow(t, repo, "job-w18b", "W18B-MALFORMED", dest, backup, 1, models.RevertStatusApplied)

	healed, err := sweeper.Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, healed, "malformed ownership markers retain the candidate")
	require.Equal(t, "malformed-owner", string(mustRead2(t, fs, backup)))
	require.Equal(t, "partial", string(mustRead2(t, fs, marker)))
	_, err = fs.Stat(dest)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func writeW18BBusy(t *testing.T, fs afero.Fs, dest string, pid int, created time.Time) {
	t.Helper()
	content := fmt.Sprintf("pid=%d,time=%d", pid, created.UnixNano())
	require.NoError(t, afero.WriteFile(fs, fsutil.ReplacementBusyPath(dest), []byte(content), 0o600))
}
