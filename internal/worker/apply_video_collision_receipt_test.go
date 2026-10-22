package worker

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/videocollision"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

func TestInterpretSkippedVideoRequiresPersistedReceipt(t *testing.T) {
	for _, missing := range []bool{false, true} {
		for _, failureCallback := range []bool{false, true} {
			t.Run(fmt.Sprintf("missing=%t/failure-callback=%t", missing, failureCallback), func(t *testing.T) {
				const path = "/source/IPX-777.mp4"
				storedPath := path
				if missing {
					storedPath = "/source/resident.mp4"
				}
				store := resultstore.New(1, []string{storedPath})
				movie := &models.Movie{ID: "IPX-777", Title: "Reviewed title"}
				store.UpdateFileResult(storedPath, &resultstore.MovieResult{
					FileMatchInfo: models.FileMatchInfo{Path: storedPath, MovieID: movie.ID},
					Movie:         movie, Status: models.JobStatusFailed, Error: "prior collision", ErrorCode: "VIDEO_COLLISION_STALE",
					VideoCollision: &videocollision.ConflictError{Code: "VIDEO_COLLISION_STALE"},
				})
				store.SetProvenance(storedPath, &resultstore.ProvenanceData{FieldSources: map[string]string{"title": "user"}})
				before := store.CloneResults()
				provenance := store.GetProvenance(storedPath)
				history := mocks.NewMockHistoryRepositoryInterface(t)
				if !missing {
					history.EXPECT().Create(mock.Anything, mock.MatchedBy(func(h *models.History) bool {
						return h.MovieID == movie.ID && h.OriginalPath == path && h.NewPath == "" && h.Status == models.HistoryStatusSkipped && h.Operation == models.HistoryOpOrganize
					})).Return(nil).Once()
				}
				inputs := makeApplyInputs(nil)
				inputs.Updater = store
				inputs.HistoryRepo = history
				var skipped, failed, organized int
				cfg := ApplyPhaseConfig{
					OnFileSkipped: func(gotPath string) {
						skipped++
						require.Equal(t, path, gotPath)
						current, err := store.GetMovieResult(path)
						require.NoError(t, err)
						require.Equal(t, "skipped", current.VideoOutcome)
					},
					OnFileOrganized: func(string) { organized++ },
				}
				wantError := "persist skipped Apply receipt: file result not found: " + path
				if failureCallback {
					cfg.OnFileFailed = func(gotPath, message string) {
						failed++
						require.Equal(t, path, gotPath)
						require.Equal(t, wantError, message)
					}
				}
				afc := &ApplyFileContext{FilePath: path, Movie: movie}
				outcome := interpretApplyResult(path, movie, time.Now(), time.Minute, inputs, cfg, t.Context(), afc, &workflow.ApplyResult{Movie: movie, VideoOutcome: "skipped"}, nil)
				require.Equal(t, missing, outcome.Failed)
				require.Equal(t, !missing, outcome.Success)
				require.Zero(t, organized)
				require.Empty(t, inputs.Broadcaster.(*stubBroadcaster).events)
				require.Equal(t, provenance, store.GetProvenance(storedPath))
				if missing {
					require.Equal(t, wantError, outcome.ErrorMsg)
					require.Zero(t, skipped)
					if failureCallback {
						require.Equal(t, 1, failed)
					}
					require.Equal(t, before, store.CloneResults())
				} else {
					require.Empty(t, outcome.ErrorMsg)
					require.Equal(t, 1, skipped)
					require.Zero(t, failed)
					current, err := store.GetMovieResult(path)
					require.NoError(t, err)
					expected := before[path].Clone()
					expected.VideoOutcome = "skipped"
					expected.VideoCollision = nil
					expected.Status = models.JobStatusCompleted
					expected.Error, expected.ErrorCode = "", ""
					expected.Revision++
					require.Equal(t, expected, current)
				}
			})
		}
	}
}

func TestInterpretVideoConflictPreservesReviewEvidence(t *testing.T) {
	for _, wrapped := range []bool{false, true} {
		t.Run(fmt.Sprintf("wrapped=%t", wrapped), func(t *testing.T) {
			const path = "/source/IPX-777.mp4"
			movie := &models.Movie{ID: "IPX-777", Title: "Reviewed title"}
			store := resultstore.New(1, []string{path})
			store.UpdateFileResult(path, &resultstore.MovieResult{
				FileMatchInfo: models.FileMatchInfo{Path: path, MovieID: movie.ID},
				Movie:         movie, Status: models.JobStatusCompleted, VideoOutcome: "skipped",
			})
			store.SetProvenance(path, &resultstore.ProvenanceData{FieldSources: map[string]string{"title": "user"}})
			before, err := store.GetMovieResult(path)
			require.NoError(t, err)
			provenance := store.GetProvenance(path)
			conflict := &videocollision.ConflictError{Code: "VIDEO_COLLISION_STALE", Collision: videocollision.Collision{
				Source: path, Destination: "/library/IPX-777.mp4", SourceSize: 17, DestinationSize: 29, Kind: "file", ReviewToken: "refreshed-receipt",
			}}
			var applyErr error = conflict
			if wrapped {
				applyErr = fmt.Errorf("publish video: %w", conflict)
			}
			inputs := makeApplyInputs(nil)
			inputs.Updater = store
			afc := &ApplyFileContext{FilePath: path, Movie: movie, MovieResult: before, Match: before.FileMatchInfo, reviewBaseline: movie.Clone()}
			var failed, succeeded int
			cfg := ApplyPhaseConfig{
				OnFileFailed: func(gotPath, message string) {
					failed++
					require.Equal(t, path, gotPath)
					require.Equal(t, applyErr.Error(), message)
				},
				OnFileSkipped:   func(string) { succeeded++ },
				OnFileOrganized: func(string) { succeeded++ },
			}
			start := time.Now().Add(-time.Second)
			outcome := interpretApplyResult(path, movie, start, time.Minute, inputs, cfg, t.Context(), afc, nil, applyErr)
			require.True(t, outcome.Failed)
			require.False(t, outcome.Success)
			require.Equal(t, applyErr.Error(), outcome.ErrorMsg)
			require.Equal(t, 1, failed)
			require.Zero(t, succeeded)
			current, err := store.GetMovieResult(path)
			require.NoError(t, err)
			require.NotNil(t, current.EndedAt)
			expected := before.Clone()
			expected.VideoOutcome = ""
			expected.VideoCollision = conflict
			expected.Status = models.JobStatusFailed
			expected.Error, expected.ErrorCode = applyErr.Error(), conflict.Code
			expected.StartedAt, expected.EndedAt = start, current.EndedAt
			expected.Revision++
			require.Equal(t, expected, current)
			require.NotSame(t, conflict, current.VideoCollision)
			require.Equal(t, provenance, store.GetProvenance(path))
			events := inputs.Broadcaster.(*stubBroadcaster).events
			require.Len(t, events, 1)
			require.Equal(t, StepFailed, events[0].Step)
		})
	}
}

func TestInterpretSuccessfulVideoClearsRetriedCollisionErrors(t *testing.T) {
	for _, code := range []string{"VIDEO_COLLISION_STALE", "VIDEO_COLLISION_UNRESOLVED", "VIDEO_COLLISION_REFRESHED"} {
		t.Run(code, func(t *testing.T) {
			const path = "/source/IPX-777.mp4"
			movie := &models.Movie{ID: "IPX-777", Title: "Reviewed title"}
			store := resultstore.New(1, []string{path})
			store.UpdateFileResult(path, &resultstore.MovieResult{
				FileMatchInfo: models.FileMatchInfo{Path: path, MovieID: movie.ID},
				Movie:         movie, Status: models.JobStatusFailed, Error: "prior collision", ErrorCode: code,
				VideoCollision: &videocollision.ConflictError{Code: code},
			})
			store.SetProvenance(path, &resultstore.ProvenanceData{FieldSources: map[string]string{"title": "user"}})
			before, err := store.GetMovieResult(path)
			require.NoError(t, err)
			provenance := store.GetProvenance(path)
			inputs := makeApplyInputs(nil)
			inputs.Updater = store
			afc := &ApplyFileContext{FilePath: path, Movie: movie, MovieResult: before, Match: before.FileMatchInfo, reviewBaseline: movie.Clone()}
			var organized int
			cfg := ApplyPhaseConfig{OnFileOrganized: func(gotPath string) {
				organized++
				require.Equal(t, path, gotPath)
			}}
			result := &workflow.ApplyResult{Movie: movie.Clone(), VideoOutcome: "replaced"}
			outcome := interpretApplyResult(path, movie, time.Now(), time.Minute, inputs, cfg, t.Context(), afc, result, nil)
			require.True(t, outcome.Success)
			require.False(t, outcome.Failed)
			require.Empty(t, outcome.ErrorMsg)
			require.Equal(t, 1, organized)
			current, err := store.GetMovieResult(path)
			require.NoError(t, err)
			expected := before.Clone()
			expected.VideoOutcome = "replaced"
			expected.VideoCollision = nil
			expected.Status = models.JobStatusCompleted
			expected.Error, expected.ErrorCode = "", ""
			expected.Revision++
			require.Equal(t, expected, current)
			require.Equal(t, provenance, store.GetProvenance(path))
			events := inputs.Broadcaster.(*stubBroadcaster).events
			require.Len(t, events, 1)
			require.Equal(t, StepComplete, events[0].Step)
		})
	}
}
