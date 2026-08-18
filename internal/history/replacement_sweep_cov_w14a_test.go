package history

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// W14A regression coverage for cross-process replacement ownership. The
// process-local SharedDestLocks guard is intentionally bypassed by these
// fixtures: the durable .dlbusy marker is the only live-owner signal.
func TestReplacementSweepW14A_LiveBusySkipsCLI(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dest := "/out/W14A-CLI/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, fs.MkdirAll("/out/W14A-CLI", 0o755))
	writeSweepFile(t, fs, backup, "old", time.Hour)
	writeW14ABusy(t, fs, dest, os.Getpid())
	journalRow(t, repo, "job-w14a-cli", "W14A-CLI", dest, backup, 1, models.RevertStatusApplied)

	healed, err := NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, healed, "a live API owner must make the CLI sweep skip")
	require.Equal(t, "old", string(mustRead2(t, fs, backup)))
	_, err = fs.Stat(dest)
	require.ErrorIs(t, err, os.ErrNotExist)
	row, err := repo.FindByID(ctx, 1)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Len(t, gf.Replacements, 1, "the live owner's armed journal entry stays intact")
}

func TestReplacementSweepW14A_LiveBusySkipsStartup(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	dest := "/out/W14A-STARTUP/poster.jpg"
	backup := dest + ".dlbak." + p3HexB
	require.NoError(t, fs.MkdirAll("/out/W14A-STARTUP", 0o755))
	writeSweepFile(t, fs, backup, "old", time.Hour)
	writeW14ABusy(t, fs, dest, os.Getpid())
	journalRow(t, repo, "job-w14a-startup", "W14A-STARTUP", dest, backup, 1, models.RevertStatusApplied)

	SweepOnStartup(fs, repo)
	require.Equal(t, "old", string(mustRead2(t, fs, backup)))
	_, err := fs.Stat(dest)
	require.ErrorIs(t, err, os.ErrNotExist)
	row, err := repo.FindByID(context.Background(), 1)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Len(t, gf.Replacements, 1, "startup uses the same live-owner exclusion")
}

func TestReplacementSweepW14A_DeadBusyIsReclaimed(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	ctx := context.Background()
	dest := "/out/W14A-STALE/poster.jpg"
	backup := dest + ".dlbak." + p3HexC
	require.NoError(t, fs.MkdirAll("/out/W14A-STALE", 0o755))
	writeSweepFile(t, fs, backup, "old", time.Hour)
	writeW14ABusy(t, fs, dest, 999999999)
	op := journalRow(t, repo, "job-w14a-stale", "W14A-STALE", dest, backup, 1, models.RevertStatusFailed)

	healed, err := NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, healed, "an old foreign marker is stale and remains sweepable")
	require.Equal(t, "old", string(mustRead2(t, fs, dest)))
	_, err = fs.Stat(backup)
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = fs.Stat(dest + ".dlbusy")
	require.ErrorIs(t, err, os.ErrNotExist)
	row, err := repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	gf, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Empty(t, gf.Replacements, "stale armed entries retain the existing restore/consume posture")
}

func TestReplacementSweepW14A_BusyMarkerErrorKeepsBackup(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	dest := "/out/W14A-ERROR/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, base.MkdirAll("/out/W14A-ERROR", 0o755))
	writeSweepFile(t, base, backup, "old", time.Hour)
	journalRow(t, repo, "job-w14a-error", "W14A-ERROR", dest, backup, 1, models.RevertStatusApplied)

	fs := afero.NewReadOnlyFs(base)
	healed, err := NewReplacementSweeper(fs, repo).Sweep(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, healed)
	require.Equal(t, "old", string(mustRead2(t, base, backup)))
}

func writeW14ABusy(t *testing.T, fs afero.Fs, dest string, pid int) {
	t.Helper()
	created := time.Now()
	if runtime.GOOS == "windows" && pid != os.Getpid() {
		// Windows cannot reliably probe a foreign PID; stale ownership is
		// time-based there, so make this synthetic dead marker old enough.
		created = created.Add(-time.Hour)
	}
	content := fmt.Sprintf("pid=%d,time=%d", pid, created.UnixNano())
	require.NoError(t, afero.WriteFile(fs, dest+".dlbusy", []byte(content), 0o600))
}
