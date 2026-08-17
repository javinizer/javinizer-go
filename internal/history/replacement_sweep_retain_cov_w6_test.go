package history

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestReplacementSweepRetainCovW6_UnjournaledMarkerWarns(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	dir := "/out/W6-RETAIN"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexA
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	writeSweepFile(t, fs, dest, "current", time.Hour)
	writeSweepFile(t, fs, backup, "user-owned", time.Hour)
	covCreateRootRow(t, repo, dir)

	var logs bytes.Buffer
	restoreLogOutput := logging.SetOutput(&logs)
	defer restoreLogOutput()

	healed, err := NewReplacementSweeper(fs, repo).Sweep(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, healed)
	exists, err := afero.Exists(fs, backup)
	require.NoError(t, err)
	require.True(t, exists, "an unjournaled marker-shaped file must be retained")
	require.Equal(t, "user-owned", string(mustRead2(t, fs, backup)))

	absoluteBackup, err := filepath.Abs(backup)
	require.NoError(t, err)
	requireLogPathContains(t, logs.String(), absoluteBackup)
	require.Contains(t, logs.String(), "no journal entry proves ownership")
	require.Contains(t, logs.String(), "user can delete it manually")
}

func TestReplacementSweepRetainCovW6_UnjournaledRestoreKeepsMarker(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	dir := "/out/W6-RESTORE"
	dest := dir + "/poster.jpg"
	backup := dest + ".dlbak." + p3HexB
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	writeSweepFile(t, fs, backup, "last-copy", time.Hour)
	covCreateRootRow(t, repo, dir)

	var logs bytes.Buffer
	restoreLogOutput := logging.SetOutput(&logs)
	defer restoreLogOutput()

	healed, err := NewReplacementSweeper(fs, repo).Sweep(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, healed)
	require.Equal(t, "last-copy", string(mustRead2(t, fs, dest)))
	exists, err := afero.Exists(fs, backup)
	require.NoError(t, err)
	require.True(t, exists, "the unjournaled source must remain after last-copy restore")

	absoluteBackup, err := filepath.Abs(backup)
	require.NoError(t, err)
	requireLogPathContains(t, logs.String(), absoluteBackup)
	require.Contains(t, logs.String(), "no journal entry proves ownership")
}
