package workflow

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type applyColumnBarrierRepo struct {
	database.BatchFileOperationRepositoryInterface
	ready  chan models.BatchFileOperation
	resume chan struct{}
	result chan error
	calls  atomic.Int32
}

func (r *applyColumnBarrierRepo) UpdateNonJournalFields(ctx context.Context, row *models.BatchFileOperation) error {
	r.calls.Add(1)
	if r.ready != nil {
		r.ready <- *row
		<-r.resume
	}
	err := r.BatchFileOperationRepositoryInterface.UpdateNonJournalFields(ctx, row)
	r.result <- err
	return err
}

type applyColumnSnapshotFS struct {
	afero.Fs
	path  string
	reads atomic.Int32
}

func (f *applyColumnSnapshotFS) Open(path string) (afero.File, error) {
	if filepath.Clean(path) == filepath.Clean(f.path) {
		f.reads.Add(1)
	}
	return f.Fs.Open(path)
}

type applyColumnSnapshotResolver struct {
	nfo.NFOFieldMerger
	path  string
	calls atomic.Int32
}

func (r *applyColumnSnapshotResolver) ResolveNFOPath(string, *models.Movie, nfo.NFONameConfig, string) (string, []string) {
	r.calls.Add(1)
	return r.path, nil
}

type applyColumnFixture struct {
	repo, other *database.BatchFileOperationRepository
	barrier     *applyColumnBarrierRepo
	fs          *applyColumnSnapshotFS
	resolver    *applyColumnSnapshotResolver
	log         RevertLog
	row         *models.BatchFileOperation
	id          OperationID
	cmd         ApplyCmd
	result      *ApplyResult
}

func newApplyColumnFixture(t *testing.T) *applyColumnFixture {
	t.Helper()
	db, dsn := pr260ArtifactDB(t)
	otherDB, err := database.New(&database.Config{Type: "sqlite", DSN: dsn, LogLevel: "error"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = otherDB.Close() })
	root := t.TempDir()
	fs := &applyColumnSnapshotFS{Fs: afero.NewOsFs(), path: filepath.Join(root, "source.nfo")}
	require.NoError(t, afero.WriteFile(fs, fs.path, []byte("newly read NFO snapshot"), 0644))
	source := filepath.Join(root, "source.mp4")
	require.NoError(t, afero.WriteFile(fs, source, []byte("source primary bytes"), 0644))
	f := &applyColumnFixture{repo: database.NewBatchFileOperationRepository(db), other: database.NewBatchFileOperationRepository(otherDB), fs: fs, resolver: &applyColumnSnapshotResolver{path: fs.path}}
	f.row = &models.BatchFileOperation{BatchJobID: "column-admission", MovieID: "COLUMN-001", OriginalPath: source, NewPath: filepath.Join(root, "prior-primary.mp4"), OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied, NFOSnapshot: "original snapshot", GeneratedFiles: models.MarshalLedgerJSON(models.GeneratedFilesJSON{Roots: []string{root}})}
	require.NoError(t, f.repo.Create(t.Context(), f.row))
	f.id = strconv.FormatUint(uint64(f.row.ID), 10)
	f.barrier = &applyColumnBarrierRepo{BatchFileOperationRepositoryInterface: f.repo, result: make(chan error, 1)}
	f.log = NewDBRevertLog(f.barrier, nil, f.row.BatchJobID, fs, nil, f.resolver, nil)
	movie := &models.Movie{ID: f.row.MovieID}
	f.cmd = ApplyCmd{Movie: movie, Match: models.FileMatchInfo{Path: source}}
	f.result = &ApplyResult{PublicationFinalized: true, Movie: movie, NFOPath: filepath.Join(root, "completed.nfo"), OrganizeResult: &organizer.OrganizeResult{OriginalPath: source, NewPath: filepath.Join(root, "completed-primary.mp4"), FolderPath: root, OldDirectoryPath: filepath.Dir(source), Moved: true}}
	return f
}

// An independent connection supplies both the authoritative state change and
// unmistakable metadata. A stale snapshot would overwrite each of these fields
// if the trailing column write were not guarded inside its SQLite transaction.
func (f *applyColumnFixture) changeWinner(t *testing.T, change string) *models.BatchFileOperation {
	t.Helper()
	row, err := f.other.FindByID(t.Context(), f.row.ID)
	require.NoError(t, err)
	row.MovieID = "WINNER-002"
	row.OriginalPath = filepath.Join(filepath.Dir(f.row.OriginalPath), "winner-source.mp4")
	row.NewPath = filepath.Join(filepath.Dir(f.row.OriginalPath), "winner-primary.mp4")
	row.NFOSnapshot = "winning snapshot"
	row.NFOPath = filepath.Join(filepath.Dir(f.row.OriginalPath), "winner.nfo")
	row.OriginalDirPath = filepath.Join(filepath.Dir(f.row.OriginalPath), "winner-directory")
	row.InPlaceRenamed = true
	row.OperationType = models.OperationTypeCopy
	require.NoError(t, f.other.UpdateNonJournalFields(t.Context(), row))
	if change == "malformed" {
		// A malformed legacy/corrupted ledger must stop snapshot capture
		// before path resolution, filesystem reads, or column publication.
		row.GeneratedFiles = "{malformed-ledger"
		require.NoError(t, f.other.Update(t.Context(), row))
	} else if change == "reverted" || change == "noop" || change == "failed" {
		status := map[string]models.RevertStatusEnum{"reverted": models.RevertStatusReverted, "noop": models.RevertStatusNoOp, "failed": models.RevertStatusFailed}[change]
		require.NoError(t, f.other.UpdateRevertStatus(t.Context(), row.ID, status))
	} else {
		require.NoError(t, f.other.UpdateJournalInTx(t.Context(), row.ID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
			ledger, err := models.ParseGeneratedFiles(current.GeneratedFiles)
			switch change {
			case "undo":
				ledger.UndoStarted = true
			case "terminal-success":
				ledger.ApplyTerminal = &models.ApplyTerminal{Outcome: "success", Fingerprint: "independent-success-terminal"}
			case "terminal-failure":
				ledger.ApplyTerminal = &models.ApplyTerminal{Outcome: "failure", Fingerprint: "independent-failure-terminal"}
			case "journal":
				ledger.Roots = append(ledger.Roots, filepath.Join(filepath.Dir(f.row.OriginalPath), "independent-root"))
			default:
				return models.GeneratedFilesJSON{}, false, fmt.Errorf("unexpected test change %q", change)
			}
			return ledger, true, err
		}))
	}
	winner, err := f.other.FindByID(t.Context(), row.ID)
	require.NoError(t, err)
	return winner
}

func TestApplyColumnAdmissionRejectsChangedSnapshot(t *testing.T) {
	for _, writer := range []string{"Complete", "CompleteIntermediate", "CompleteFailed", "CompleteNil", "CompleteFailedNil", "CaptureSnapshot"} {
		for _, change := range []string{"undo", "terminal-failure", "reverted", "journal"} {
			t.Run(writer+"/"+change, func(t *testing.T) {
				f := newApplyColumnFixture(t)
				f.barrier.ready = make(chan models.BatchFileOperation, 1)
				f.barrier.resume = make(chan struct{})
				done := make(chan error, 1)
				var release sync.Once
				go func() {
					defer close(done)
					switch writer {
					case "Complete":
						done <- f.log.Complete(t.Context(), f.id, f.result)
					case "CompleteIntermediate":
						intermediate := *f.result
						intermediate.PublicationFinalized = false
						done <- f.log.Complete(t.Context(), f.id, &intermediate)
					case "CompleteFailed":
						done <- f.log.CompleteFailed(t.Context(), f.id, f.result)
					case "CompleteNil":
						done <- f.log.Complete(t.Context(), f.id, nil)
					case "CompleteFailedNil":
						done <- f.log.CompleteFailed(t.Context(), f.id, nil)
					case "CaptureSnapshot":
						f.log.CaptureSnapshot(t.Context(), f.id, f.cmd)
						done <- nil
					}
				}()
				defer func() {
					release.Do(func() { close(f.barrier.resume) })
					for range done {
					}
				}()
				var prepared models.BatchFileOperation
				select {
				case prepared = <-f.barrier.ready:
				case err := <-done:
					t.Fatalf("%s returned before the column-write barrier: %v", writer, err)
				}
				if writer == "CaptureSnapshot" {
					require.Equal(t, int32(1), f.fs.reads.Load(), "snapshot file was read before admission changed")
					require.Equal(t, "newly read NFO snapshot", prepared.NFOSnapshot)
					require.Equal(t, f.fs.path, prepared.NFOPath)
				} else {
					merged := p3Ledger(t, f.other, f.id)
					if writer == "CompleteIntermediate" {
						require.Nil(t, merged.ApplyTerminal, "intermediate completion keeps Apply open")
						require.Contains(t, merged.Delete, f.result.NFOPath, "intermediate journal merge committed before the column barrier")
					} else {
						require.NotNil(t, merged.ApplyTerminal, "terminal journal committed before the column barrier")
					}
					if writer == "Complete" || writer == "CompleteIntermediate" || writer == "CompleteFailed" {
						require.Equal(t, f.result.OrganizeResult.NewPath, prepared.NewPath)
						require.Equal(t, f.result.NFOPath, prepared.NFOPath)
					}
				}
				winner := f.changeWinner(t, change)
				release.Do(func() { close(f.barrier.resume) })
				publicErr := <-done
				columnErr := <-f.barrier.result
				require.ErrorContains(t, columnErr, "Apply columns refused")
				if writer == "CaptureSnapshot" {
					require.NoError(t, publicErr, "snapshot refusal remains warn-only")
				} else {
					require.ErrorContains(t, publicErr, "Apply columns refused")
				}
				after, err := f.other.FindByID(t.Context(), f.row.ID)
				require.NoError(t, err)
				require.Equal(t, winner, after, "no primary path, snapshot, metadata, status, timestamp, or journal may be overwritten")
				require.Equal(t, int32(1), f.barrier.calls.Load())
				require.Equal(t, "source primary bytes", string(mustReadStagedTxn(t, f.fs, f.cmd.Match.Path)))
			})
		}
	}
}

func TestCaptureSnapshotClosedRowDoesNotReadOrWrite(t *testing.T) {
	for _, change := range []string{"undo", "terminal-success", "terminal-failure", "reverted", "noop", "failed", "malformed"} {
		t.Run(change, func(t *testing.T) {
			f := newApplyColumnFixture(t)
			before := f.changeWinner(t, change)
			f.log.CaptureSnapshot(t.Context(), f.id, f.cmd)
			require.Zero(t, f.resolver.calls.Load(), "closed rows must be rejected before resolving NFO paths")
			require.Zero(t, f.fs.reads.Load(), "closed rows must be rejected before reading NFO bytes")
			require.Zero(t, f.barrier.calls.Load(), "closed rows must not reach a column write")
			after, err := f.other.FindByID(t.Context(), f.row.ID)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}
