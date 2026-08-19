package history

// POSTER-WRITE-HARDENING codex PR#215 wave-24 (P2, #discussion_r3808360868)
// — end-to-end at the CLI: the wave-8 pre-revert sweep was bounded, but the
// reverter's INNER sweep (sweepJournaledDestinations → SweepDestinations)
// ran under the caller's unbounded ctx, so a stalled destination filesystem
// hung the WHOLE command right after the bounded pre-sweep passed. The inner
// sweep now carries its own derived deadline (internal/history); this test
// wedges the sweep through the exported seam and drives the real command
// path: the command must complete within the budget while the "filesystem"
// never answers.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	historypkg "github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunHistoryRevertW24_WedgedInnerSweepReleasesCommand(t *testing.T) {
	configPath, db := setupRevertMissDB(t)
	defer db.Close()

	batchID := "w24-wedged-batch"
	createOrganizedJob(t, db, batchID)

	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "lib", "W24-001", "poster.jpg")
	rawJournal, err := json.Marshal(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{{
		Destination: dest,
		Backup:      dest + ".dlbak.0123456789abcdef",
		DestSeq:     1,
	}}})
	require.NoError(t, err)

	ctx := context.Background()
	batchRepo := database.NewBatchFileOperationRepository(db)
	require.NoError(t, batchRepo.Create(ctx, &models.BatchFileOperation{
		BatchJobID:     batchID,
		MovieID:        "W24-001",
		OriginalPath:   filepath.Join(tmpDir, "old", "W24-001.mp4"),
		NewPath:        filepath.Join(tmpDir, "lib", "W24-001", "W24-001.mp4"),
		OperationType:  models.OperationTypeMove,
		RevertStatus:   models.RevertStatusApplied,
		GeneratedFiles: string(rawJournal),
	}))

	entered := make(chan struct{})
	unblock := make(chan struct{})
	unblocked := make(chan struct{})
	// Wave-34 (finding F4): the substituted seam answers BOTH pre-sweep
	// invocations (destinations, then roots) — the closes must stay
	// idempotent for the second call once the first one drained.
	var enteredOnce, unblockedOnce sync.Once
	restore := historypkg.SwapReverterSweepForTest(
		func(sweepCtx context.Context, _ *historypkg.ReplacementSweeper, dests []string) (int, error) {
			// Wedged-network-filesystem stand-in: deliberately never observes
			// sweepCtx, exactly like afero.ReadDir cannot.
			defer unblockedOnce.Do(func() { close(unblocked) })
			enteredOnce.Do(func() { close(entered) })
			<-unblock
			return 0, nil
		}, 100*time.Millisecond)
	defer restore()

	rootCmd := &cobra.Command{Use: "root"}
	rootCmd.PersistentFlags().String("config", configPath, "config file")
	cmd := NewCommand()
	rootCmd.AddCommand(cmd)
	rootCmd.SetArgs([]string{"history", "revert", batchID})

	start := time.Now()
	output := captureMissOutput(t, func() {
		execErr := rootCmd.Execute()
		require.NoError(t, execErr)
	})
	elapsed := time.Since(start)

	// Without the derived deadline the command would park here forever (the
	// seam never returns until unblocked below): completing at all within a
	// few seconds IS the regression pin.
	require.GreaterOrEqual(t, elapsed, 100*time.Millisecond,
		"the inner sweep observes its budget before the revert proceeds")
	require.Less(t, elapsed, 5*time.Second,
		"the command proceeds past the wedged sweep instead of hanging")
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the inner sweep must have engaged before the deadline released the command")
	}
	assert.Contains(t, output, "Reverted batch")

	// Drain the abandoned sweep goroutine (the accepted leak tradeoff): once
	// the "filesystem" answers it must finish cleanly.
	close(unblock)
	select {
	case <-unblocked:
	case <-time.After(2 * time.Second):
		t.Fatal("the abandoned sweep goroutine must drain once the filesystem answers")
	}
}
