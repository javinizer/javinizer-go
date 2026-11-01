package history_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

const crossProcessUndoDB = "JAVINIZER_TEST_UNDO_TERMINAL_DB"
const crossProcessUndoBatch = "cross-process-terminal-contract"

type terminalUndoReceipt struct {
	output          string
	pid             int
	err             error
	row             *models.BatchFileOperation
	restoredPrimary os.FileInfo
	restoredPoster  os.FileInfo
}

// This adapter delegates every repository operation. At the real destination
// preflight boundary it allows another process to finish Undo after this
// process has already committed its own Undo admission. No row is fabricated
// or modified by the adapter, and no package-private lock is bypassed.
type terminalUndoBoundaryRepo struct {
	database.BatchFileOperationRepositoryInterface
	id                 uint
	fired              atomic.Bool
	finishOtherProcess func(context.Context) terminalUndoReceipt
	receipt            chan terminalUndoReceipt
}

func (r *terminalUndoBoundaryRepo) FindOperationsByDestination(ctx context.Context, destination string) ([]models.BatchFileOperation, error) {
	rows, err := r.BatchFileOperationRepositoryInterface.FindOperationsByDestination(ctx, destination)
	if err != nil {
		return nil, err
	}
	current, err := r.BatchFileOperationRepositoryInterface.FindByID(ctx, r.id)
	if err != nil {
		return nil, err
	}
	ledger, err := models.ParseGeneratedFiles(current.GeneratedFiles)
	if err != nil {
		return nil, err
	}
	// The preliminary sweep runs before admission and must not trigger this
	// boundary. The actual replacement replay has admitted Undo but has not
	// acquired the destination's durable busy marker yet.
	if current.RevertStatus == models.RevertStatusApplied && ledger.UndoStarted && len(ledger.Replacements) > 0 && r.fired.CompareAndSwap(false, true) {
		receipt := r.finishOtherProcess(ctx)
		r.receipt <- receipt
		if receipt.err != nil {
			return nil, fmt.Errorf("independent Undo process failed: %w; output: %s", receipt.err, receipt.output)
		}
	}
	return rows, nil
}

func TestUndoCrossProcessTerminalHelper(t *testing.T) {
	path := os.Getenv(crossProcessUndoDB)
	if path == "" {
		t.Skip("subprocess helper")
	}
	db, err := database.New(&database.Config{Type: "sqlite", DSN: path, LogLevel: "silent"})
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()
	result, err := history.NewReverter(afero.NewOsFs(), database.NewBatchFileOperationRepository(db)).RevertBatch(t.Context(), crossProcessUndoBatch)
	require.NoError(t, err)
	require.Equal(t, 1, result.Succeeded)
	require.Zero(t, result.Failed)
	require.Zero(t, result.Skipped)
	t.Logf("independent Undo winner pid=%d succeeded=%d", os.Getpid(), result.Succeeded)
}

func TestUndoCrossProcessTerminalWinnerStopsStalePrimaryReplay(t *testing.T) {
	ctx := t.Context()
	dbPath := filepath.Join(t.TempDir(), "history.sqlite")
	db, err := database.New(&database.Config{Type: "sqlite", DSN: dbPath, LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, db.RunMigrationsOnStartup(ctx))
	repo := database.NewBatchFileOperationRepository(db)
	root := t.TempDir()
	source := filepath.Join(root, "incoming", "movie.mp4")
	installed := filepath.Join(root, "library", "movie", "movie.mp4")
	poster := filepath.Join(filepath.Dir(installed), "poster.jpg")
	backup := poster + ".dlbak.0123456789abcdef"
	require.NoError(t, os.MkdirAll(filepath.Dir(source), 0700))
	require.NoError(t, os.MkdirAll(filepath.Dir(installed), 0700))
	primaryBytes := []byte("owned primary bytes to restore")
	posterBytes := []byte("owned previous poster bytes")
	require.NoError(t, os.WriteFile(installed, primaryBytes, 0600))
	require.NoError(t, os.WriteFile(poster, []byte("replacement poster bytes"), 0600))
	require.NoError(t, os.WriteFile(backup, posterBytes, 0600))
	stamp := time.Unix(1700000000, 0)
	require.NoError(t, os.Chtimes(backup, stamp, stamp))
	info, err := os.Stat(backup)
	require.NoError(t, err)
	digest := sha256.Sum256(posterBytes)
	retain := true
	op := &models.BatchFileOperation{BatchJobID: crossProcessUndoBatch, MovieID: "TERMINAL-001", OriginalPath: source, NewPath: installed, OriginalDirPath: filepath.Dir(source), OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied,
		GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{Roots: []string{filepath.Dir(installed)}, UndoRetention: &retain, Replacements: []models.ReplacementEntry{{Destination: poster, Backup: backup, DestSeq: 1, Installed: true, BackupSize: info.Size(), BackupModUnix: info.ModTime().Unix(), BackupSHA256: hex.EncodeToString(digest[:])}}})}
	require.NoError(t, repo.Create(ctx, op))
	childCoverage := t.TempDir()
	gated := &terminalUndoBoundaryRepo{BatchFileOperationRepositoryInterface: repo, id: op.ID, receipt: make(chan terminalUndoReceipt, 1)}
	gated.finishOtherProcess = func(parent context.Context) terminalUndoReceipt {
		childCtx, cancel := context.WithTimeout(parent, 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(childCtx, os.Args[0], "-test.run=^TestUndoCrossProcessTerminalHelper$", "-test.v", "-test.timeout=20s")
		// Keep child coverage separate: the newly exercised late-return branch
		// must be proved by the parent process profile, not credited from a child.
		env := make([]string, 0, len(os.Environ())+2)
		for _, entry := range os.Environ() {
			if !strings.HasPrefix(entry, crossProcessUndoDB+"=") && !strings.HasPrefix(entry, "GOCOVERDIR=") {
				env = append(env, entry)
			}
		}
		cmd.Env = append(env, crossProcessUndoDB+"="+dbPath, "GOCOVERDIR="+childCoverage)
		output, err := cmd.CombinedOutput()
		receipt := terminalUndoReceipt{output: string(output), err: err}
		if cmd.Process != nil {
			receipt.pid = cmd.Process.Pid
		}
		if err != nil {
			return receipt
		}
		receipt.row, receipt.err = repo.FindByID(parent, op.ID)
		if receipt.err != nil {
			return receipt
		}
		receipt.restoredPrimary, receipt.err = os.Stat(source)
		if receipt.err != nil {
			return receipt
		}
		receipt.restoredPoster, receipt.err = os.Stat(poster)
		return receipt
	}
	result, err := history.NewReverter(afero.NewOsFs(), gated).RevertBatch(ctx, op.BatchJobID)
	require.NoError(t, err, "selected-operation errors remain in the public per-file outcomes")
	require.True(t, gated.fired.Load(), "another process must finish after durable Undo admission")
	receipt := <-gated.receipt
	t.Logf("independent process receipt:\n%s", receipt.output)
	require.NoError(t, receipt.err)
	require.NotEqual(t, os.Getpid(), receipt.pid)
	require.Contains(t, receipt.output, "independent Undo winner")
	require.Equal(t, 1, result.Total)
	require.Zero(t, result.Succeeded, "the stale caller must not report a second Undo")
	require.Equal(t, 1, result.Failed)
	require.Zero(t, result.Skipped)
	require.Len(t, result.Outcomes, 1)
	require.Equal(t, op.ID, result.Outcomes[0].OperationID)
	require.Equal(t, history.ErrBatchAlreadyReverted.Error(), result.Outcomes[0].Error)
	require.Equal(t, models.RevertOutcomeFailed, result.Outcomes[0].Outcome)
	row, err := repo.FindByID(ctx, op.ID)
	require.NoError(t, err)
	require.Equal(t, receipt.row, row, "stale replay must not downgrade or rewrite the winner's durable terminal row")
	require.Equal(t, models.RevertStatusReverted, row.RevertStatus)
	require.NotNil(t, row.RevertedAt)
	ledger, err := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, err)
	require.Empty(t, ledger.Replacements)
	for path, want := range map[string][]byte{source: primaryBytes, poster: posterBytes} {
		got, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
	primaryInfo, err := os.Stat(source)
	require.NoError(t, err)
	posterInfo, err := os.Stat(poster)
	require.NoError(t, err)
	require.True(t, os.SameFile(receipt.restoredPrimary, primaryInfo), "parent must not replace the restored primary object")
	require.True(t, os.SameFile(receipt.restoredPoster, posterInfo), "parent must not replace the restored poster object")
	for _, path := range []string{installed, backup} {
		_, err := os.Stat(path)
		require.ErrorIs(t, err, os.ErrNotExist)
	}
	retry, err := history.NewReverter(afero.NewOsFs(), repo).RevertBatch(ctx, op.BatchJobID)
	require.Nil(t, retry)
	require.ErrorIs(t, err, history.ErrBatchAlreadyReverted, "a fresh public retry retains the typed terminal verdict")
}
