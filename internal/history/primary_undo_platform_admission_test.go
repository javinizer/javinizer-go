package history

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestPrimaryUndoPlatformAdmissionPolicy(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "dragonfly", "freebsd", "netbsd", "openbsd"} {
		t.Run(goos, func(t *testing.T) {
			require.NoError(t, primaryUndoDurabilitySupportedOn(goos))
		})
	}
	for _, goos := range []string{"solaris", "illumos", "aix", "windows", "android", "ios", "js", "plan9", "wasip1", "", "future-os"} {
		t.Run("refuse-"+goos, func(t *testing.T) {
			err := primaryUndoDurabilitySupportedOn(goos)
			require.ErrorContains(t, err, "manual recovery")
			require.ErrorContains(t, err, "retain source, stage, and history")
		})
	}
}

func TestPrimaryUndoUnsupportedPlatformSameVolumeBypassesPolicy(t *testing.T) {
	if primaryUndoDurabilitySupported() == nil {
		t.Skip("this host has an admitted primary Undo platform policy")
	}
	root := t.TempDir()
	source := filepath.Join(root, "source.mkv")
	destination := filepath.Join(root, "incoming", "original.mkv")
	require.NoError(t, os.WriteFile(source, []byte("synthetic same-volume source"), 0600))
	op := &models.BatchFileOperation{OperationType: models.OperationTypeMove, NewPath: source, OriginalPath: destination}
	result, err := revertPrimaryFileFS(context.Background(), afero.NewOsFs(), newP3OpRepo(), op, func(string, string) {})
	require.NoError(t, err)
	require.Nil(t, result)
	require.Empty(t, op.GeneratedFiles)
	require.NoFileExists(t, source)
	got, err := os.ReadFile(destination)
	require.NoError(t, err)
	require.Equal(t, "synthetic same-volume source", string(got))
}

// This runs on a genuinely refused host, or in a separately labelled policy
// simulation. Linux execution with its normal admitted policy must skip it;
// neither that skip nor a simulated GOOS is native Solaris validation.
func TestPrimaryUndoUnsupportedPlatformRefusesBeforeIO(t *testing.T) {
	if primaryUndoDurabilitySupported() == nil {
		if os.Getenv("JAVINIZER_TEST_EXPECT_UNSUPPORTED_PRIMARY_UNDO") == "1" {
			t.Fatal("expected unsupported-platform admission refusal, but the runtime gate admitted it")
		}
		t.Skip("this host has an admitted primary Undo platform policy")
	}
	root := t.TempDir()
	source := filepath.Join(root, "source.mkv")
	destination := filepath.Join(root, "original.mkv")
	stage := destination + ".undo.previous"
	require.NoError(t, os.WriteFile(source, []byte("synthetic retained source"), 0600))
	require.NoError(t, os.WriteFile(stage, []byte("synthetic previous stage"), 0600))
	j := &models.PrimaryUndo{State: "copying", Source: source, Destination: destination, Stage: stage}
	op := &models.BatchFileOperation{OperationType: models.OperationTypeMove, NewPath: source, OriginalPath: destination, GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{PrimaryUndo: j})}
	before := op.GeneratedFiles
	for attempt := 0; attempt < 2; attempt++ {
		// Nil collaborators make any access past the admission boundary fail
		// the test rather than silently permit stage creation or a journal write.
		require.ErrorContains(t, beginPrimaryUndo(context.Background(), nil, nil, op), "manual recovery")
		require.ErrorContains(t, resumePrimaryUndo(context.Background(), nil, nil, op, j), "manual recovery")
		require.Equal(t, before, op.GeneratedFiles)
		require.Empty(t, j.SupersededStages)
		require.Equal(t, "copying", j.State)
	}
	got, err := os.ReadFile(source)
	require.NoError(t, err)
	require.Equal(t, "synthetic retained source", string(got))
	got, err = os.ReadFile(stage)
	require.NoError(t, err)
	require.Equal(t, "synthetic previous stage", string(got))
	require.NoFileExists(t, destination)
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Len(t, entries, 2, "refused retries must not create additional stage generations")
}
