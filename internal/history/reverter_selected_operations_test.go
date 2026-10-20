package history

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type selectedRevertEntryPoint struct {
	name string
	run  func(*Reverter) (*RevertBatchResult, error)
}

func selectedRevertEntryPoints() []selectedRevertEntryPoint {
	return []selectedRevertEntryPoint{
		{"batch", func(r *Reverter) (*RevertBatchResult, error) {
			return r.RevertBatch(context.Background(), "requested-batch")
		}},
		{"scrape", func(r *Reverter) (*RevertBatchResult, error) {
			return r.RevertScrape(context.Background(), "requested-batch", "MOVIE")
		}},
	}
}

// Selection and empty-result semantics belong to the public entrypoints, not to
// their shared orchestration. In particular, noop + reverted differs by scope.
func TestSelectedRevert_EmptySelectionContracts(t *testing.T) {
	fetchErr := errors.New("selection unavailable")
	for _, tc := range []struct {
		name      string
		statuses  []models.RevertStatusEnum
		movie     string
		fetchErr  error
		batchErr  error
		scrapeErr string
	}{
		{name: "fetch failure", fetchErr: fetchErr, batchErr: fetchErr, scrapeErr: "failed to fetch batch operations: selection unavailable"},
		{name: "no rows", batchErr: ErrNoOperationsFound, scrapeErr: ErrNoOperationsFound.Error()},
		{name: "noop", statuses: []models.RevertStatusEnum{models.RevertStatusNoOp}},
		{name: "reverted", statuses: []models.RevertStatusEnum{models.RevertStatusReverted}, batchErr: ErrBatchAlreadyReverted, scrapeErr: "no processable operations found for movie MOVIE in batch requested-batch"},
		{name: "noop and reverted", statuses: []models.RevertStatusEnum{models.RevertStatusNoOp, models.RevertStatusReverted}, batchErr: ErrBatchAlreadyReverted},
		{name: "unknown status", statuses: []models.RevertStatusEnum{"unexpected"}, batchErr: ErrNoOperationsFound, scrapeErr: "no processable operations found for movie MOVIE in batch requested-batch"},
		{name: "unrelated noop", statuses: []models.RevertStatusEnum{models.RevertStatusNoOp}, movie: "OTHER", scrapeErr: "no processable operations found for movie MOVIE in batch requested-batch"},
	} {
		for _, entry := range selectedRevertEntryPoints() {
			t.Run(tc.name+"/"+entry.name, func(t *testing.T) {
				movie := tc.movie
				if movie == "" {
					movie = "MOVIE"
				}
				var rows []models.BatchFileOperation
				for i, status := range tc.statuses {
					rows = append(rows, models.BatchFileOperation{ID: uint(i + 1), MovieID: movie, RevertStatus: status})
				}
				repo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
				repo.On("FindByBatchJobID", mock.Anything, "requested-batch").Return(rows, tc.fetchErr).Once()
				r := &Reverter{batchFileOpRepo: repo}
				result, err := entry.run(r)
				switch {
				case entry.name == "batch" && tc.batchErr != nil:
					require.ErrorIs(t, err, tc.batchErr)
					require.Nil(t, result)
				case entry.name == "scrape" && tc.scrapeErr != "":
					require.EqualError(t, err, tc.scrapeErr)
					require.Nil(t, result)
				default:
					require.NoError(t, err)
					require.Equal(t, &RevertBatchResult{}, result)
				}
			})
		}
	}
}

func TestSelectedRevert_RefreshPreservesSelection(t *testing.T) {
	for _, entry := range selectedRevertEntryPoints() {
		for _, scenario := range []string{"refresh error", "empty refresh", "missing selected row", "reordered and unrelated", "terminal status and changed movie"} {
			t.Run(entry.name+"/"+scenario, func(t *testing.T) {
				initial := []models.BatchFileOperation{
					{ID: 99, MovieID: "MOVIE", RevertStatus: models.RevertStatusNoOp},
					{ID: 20, BatchJobID: "selected-batch", MovieID: "MOVIE", RevertStatus: models.RevertStatusApplied,
						OriginalPath: "/old/source/first.mp4", NewPath: "/old/output/first/video.mp4"},
					{ID: 10, BatchJobID: "different-batch", MovieID: "MOVIE", RevertStatus: models.RevertStatusFailed,
						OriginalPath: "/old/source/second.mp4", NewPath: "/old/output/second/video.mp4"},
					{ID: 98, MovieID: "MOVIE", RevertStatus: models.RevertStatusReverted},
					{ID: 97, MovieID: "MOVIE", RevertStatus: "unexpected"},
				}
				// A real sweep scope pins sweep-before-refresh. All journal
				// sequences tie, so revertOperations retains selection order.
				for i := 1; i <= 2; i++ {
					initial[i].GeneratedFiles = models.MarshalLedgerJSON(models.GeneratedFilesJSON{
						Replacements: []models.ReplacementEntry{{Destination: initial[i].NewPath, DestSeq: 1}},
					})
				}
				if entry.name == "scrape" {
					initial = append(initial, models.BatchFileOperation{ID: 96, MovieID: "OTHER", RevertStatus: models.RevertStatusApplied})
				}
				first, second := initial[1], initial[2]
				first.OriginalPath, first.NewPath = "/fresh/source/first.mp4", "/fresh/output/first/video.mp4"
				second.OriginalPath, second.NewPath = "/fresh/source/second.mp4", "/fresh/output/second/video.mp4"
				// The refresh is by selected ID only, not returned position,
				// movie, status, or the caller's batch ID.
				unrelated := models.BatchFileOperation{ID: 999, MovieID: "MOVIE", RevertStatus: models.RevertStatusApplied}
				fresh := []models.BatchFileOperation{unrelated, second, first}
				want := []models.BatchFileOperation{first, second}
				var refreshErr error
				switch scenario {
				case "refresh error":
					refreshErr = errors.New("refresh unavailable")
					want = []models.BatchFileOperation{initial[1], initial[2]}
				case "empty refresh":
					fresh = nil
					want = []models.BatchFileOperation{initial[1], initial[2]}
				case "missing selected row":
					fresh = []models.BatchFileOperation{unrelated, second}
					want = []models.BatchFileOperation{initial[1], second}
				case "terminal status and changed movie":
					first.RevertStatus, second.RevertStatus = models.RevertStatusNoOp, models.RevertStatusReverted
					first.MovieID, second.MovieID = "FRESH-FIRST", "FRESH-SECOND"
					fresh = []models.BatchFileOperation{second, unrelated, first}
					want = []models.BatchFileOperation{first, second}
				}

				var stages []string
				var swept [][]string
				t.Cleanup(SwapReverterSweepForTest(func(_ context.Context, _ *ReplacementSweeper, paths []string) (int, error) {
					stages = append(stages, "sweep")
					swept = append(swept, append([]string(nil), paths...))
					return 0, nil
				}, 0))
				repo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
				repo.On("FindByBatchJobID", mock.Anything, "requested-batch").Return(initial, nil).Once().Run(func(mock.Arguments) {
					stages = append(stages, "select")
				})
				repo.On("FindByBatchJobID", mock.Anything, "selected-batch").Return(fresh, refreshErr).Once().Run(func(mock.Arguments) {
					stages = append(stages, "refresh")
				})
				// Fail the per-operation lock refresh so the outcome exposes
				// exactly the outer refresh snapshot, rather than overwriting it
				// with another repository read before we can observe it.
				for _, id := range []uint{20, 10} {
					repo.On("FindByID", mock.Anything, id).Return(nil, errors.New("locked read unavailable")).Once().Run(func(args mock.Arguments) {
						stages = append(stages, fmt.Sprintf("revert:%d", args.Get(1).(uint)))
					})
				}
				r := NewReverter(afero.NewMemMapFs(), repo)
				result, err := entry.run(r)
				require.NoError(t, err)
				require.Equal(t, []string{"select", "sweep", "sweep", "refresh", "revert:20", "revert:10"}, stages)
				require.Equal(t, []string{initial[1].NewPath, initial[2].NewPath}, swept[0])
				require.Len(t, swept, 2)
				expected := &RevertBatchResult{Total: 2, Failed: 2}
				for _, op := range want {
					expected.Outcomes = append(expected.Outcomes, RevertFileResult{
						OperationID: op.ID, MovieID: op.MovieID, OriginalPath: op.OriginalPath, NewPath: op.NewPath,
						Outcome: models.RevertOutcomeFailed,
						Error:   fmt.Sprintf("failed to re-read operation %d after acquiring revert lock: locked read unavailable", op.ID),
					})
				}
				require.Equal(t, expected, result)
			})
		}
	}
}

type selectedRevertCleanupRecorder struct {
	fileSystemReverter
	ancestorCalls []string
}

func (r *selectedRevertCleanupRecorder) cleanupEmptyDir(dirPath, stopAt string) {
	if stopAt == "" {
		r.ancestorCalls = append(r.ancestorCalls, dirPath)
	}
	r.fileSystemReverter.cleanupEmptyDir(dirPath, stopAt)
}

func TestSelectedRevert_AncestorCleanupUsesRefreshedSelectionSnapshot(t *testing.T) {
	for _, entry := range selectedRevertEntryPoints() {
		t.Run(entry.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			initialRoot := filepath.FromSlash("/output/initial")
			refreshedRoot := filepath.FromSlash("/output/refreshed")
			admittedRoot := filepath.FromSlash("/output/admitted")
			for _, root := range []string{initialRoot, refreshedRoot, admittedRoot} {
				require.NoError(t, fs.MkdirAll(root, 0755))
			}
			initial := models.BatchFileOperation{
				ID: 42, BatchJobID: "requested-batch", MovieID: "MOVIE", RevertStatus: models.RevertStatusApplied,
				OperationType: models.OperationTypeCopy, OriginalPath: "/source/video.mp4", NewPath: filepath.Join(initialRoot, "movie", "video.mp4"),
			}
			refreshed := initial
			refreshed.NewPath = filepath.Join(refreshedRoot, "movie", "video.mp4")
			admitted := initial
			admitted.NewPath = filepath.Join(admittedRoot, "movie", "video.mp4")

			repo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
			repo.On("FindByBatchJobID", mock.Anything, "requested-batch").Return([]models.BatchFileOperation{initial}, nil).Once()
			repo.On("FindByBatchJobID", mock.Anything, "requested-batch").Return([]models.BatchFileOperation{refreshed}, nil).Once()
			repo.On("FindByID", mock.Anything, admitted.ID).Return(&admitted, nil).Twice()
			expectMockUndoAdmission(t, repo, &admitted)
			repo.On("UpdateRevertStatus", mock.Anything, admitted.ID, models.RevertStatusReverted).Return(nil).Once()
			r := NewReverter(fs, repo)
			recorder := &selectedRevertCleanupRecorder{fileSystemReverter: r.fsReverter}
			r.fsReverter = recorder
			result, err := entry.run(r)
			require.NoError(t, err)
			require.Equal(t, &RevertBatchResult{Total: 1, Succeeded: 1, Outcomes: []RevertFileResult{{
				OperationID: admitted.ID, MovieID: admitted.MovieID, OriginalPath: admitted.OriginalPath,
				NewPath: admitted.NewPath, Outcome: models.RevertOutcomeReverted,
			}}}, result)
			// Per-file lock/admission reloads mutate revertOperations' copy,
			// not the selected snapshot used for the final ancestor sweep.
			require.Equal(t, []string{refreshedRoot}, recorder.ancestorCalls)
			for _, root := range []string{initialRoot, refreshedRoot, admittedRoot} {
				exists, statErr := afero.DirExists(fs, root)
				require.NoError(t, statErr)
				require.Equal(t, root != refreshedRoot, exists, root)
			}
		})
	}
}

func TestSelectedRevert_AncestorCleanupRequiresAllSuccess(t *testing.T) {
	for _, entry := range selectedRevertEntryPoints() {
		for _, outcome := range []models.RevertOutcomeEnum{models.RevertOutcomeReverted, models.RevertOutcomeSkipped, models.RevertOutcomeFailed} {
			t.Run(entry.name+"/"+outcome.String(), func(t *testing.T) {
				ctx := context.Background()
				repo := newP3OpRepo()
				fs := afero.NewMemMapFs()
				successRoot := filepath.FromSlash("/output/success")
				otherRoot := filepath.FromSlash("/output/other")
				require.NoError(t, fs.MkdirAll(successRoot, 0755))
				require.NoError(t, fs.MkdirAll(otherRoot, 0755))
				first := models.BatchFileOperation{
					BatchJobID: "requested-batch", MovieID: "MOVIE", RevertStatus: models.RevertStatusApplied,
					OperationType: models.OperationTypeCopy, OriginalPath: "/source/first.mp4", NewPath: filepath.Join(successRoot, "movie", "first.mp4"),
				}
				second := models.BatchFileOperation{
					BatchJobID: "requested-batch", MovieID: "MOVIE", RevertStatus: models.RevertStatusFailed,
					OperationType: models.OperationTypeCopy, OriginalPath: "/source/second.mp4", NewPath: filepath.Join(otherRoot, "movie", "second.mp4"),
				}
				switch outcome {
				case models.RevertOutcomeSkipped:
					second.OperationType = models.OperationTypeMove // Missing anchor.
				case models.RevertOutcomeFailed:
					second.OperationType = "unknown"
				}
				require.NoError(t, repo.Create(ctx, &first))
				require.NoError(t, repo.Create(ctx, &second))
				r := NewReverter(fs, repo)
				recorder := &selectedRevertCleanupRecorder{fileSystemReverter: r.fsReverter}
				r.fsReverter = recorder
				result, err := entry.run(r)
				require.NoError(t, err)
				require.Equal(t, 2, result.Total)
				require.Len(t, result.Outcomes, 2)
				require.Equal(t, models.RevertOutcomeReverted, result.Outcomes[0].Outcome)
				require.Equal(t, outcome, result.Outcomes[1].Outcome)
				if outcome == models.RevertOutcomeReverted {
					require.Equal(t, 2, result.Succeeded)
					require.Zero(t, result.Skipped)
					require.Zero(t, result.Failed)
					require.ElementsMatch(t, []string{successRoot, otherRoot}, recorder.ancestorCalls)
					exists, statErr := afero.DirExists(fs, successRoot)
					require.NoError(t, statErr)
					require.False(t, exists)
				} else {
					require.Equal(t, 1, result.Succeeded)
					require.Equal(t, 1, result.Skipped+result.Failed)
					require.Empty(t, recorder.ancestorCalls, "one unresolved sibling suppresses every ancestor cleanup")
					for _, root := range []string{successRoot, otherRoot} {
						exists, statErr := afero.DirExists(fs, root)
						require.NoError(t, statErr)
						require.True(t, exists, "even the successful operation's empty root must remain")
					}
				}
			})
		}
	}
}
