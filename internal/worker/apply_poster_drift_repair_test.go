package worker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingRepairWF is a WorkflowInterface stub whose Apply records every
// command it receives and returns a result whose Movie is the submitted
// movie verbatim (poster fields included — the pass "physically applied"
// exactly what it was given) with the configured pipeline title. onApply
// runs between recording and replying so a test can commit ANOTHER crop
// mid-repair, simulating an edit that lands inside a repair pass's own
// snapshot→write-back window.
type recordingRepairWF struct {
	mu        sync.Mutex
	calls     []workflow.ApplyCmd
	applyErr  error
	omitMovie bool
	pipeTitle string
	onApply   func(call int)
}

func (s *recordingRepairWF) Scrape(_ context.Context, _ scrape.ScrapeCmd) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
	return nil, nil, nil
}

func (s *recordingRepairWF) Apply(_ context.Context, cmd workflow.ApplyCmd) (*workflow.ApplyResult, error) {
	s.mu.Lock()
	s.calls = append(s.calls, cmd)
	call := len(s.calls)
	hook := s.onApply
	err := s.applyErr
	omit := s.omitMovie
	title := s.pipeTitle
	s.mu.Unlock()
	if hook != nil {
		hook(call)
	}
	if err != nil {
		return nil, err
	}
	res := &workflow.ApplyResult{}
	if !omit {
		m := cmd.Movie.Clone()
		m.Title = title
		res.Movie = m
	}
	return res, nil
}

func (s *recordingRepairWF) Preview(_ context.Context, _ workflow.PreviewCmd) (*workflow.PreviewResult, error) {
	return nil, nil
}

func (s *recordingRepairWF) Compare(_ context.Context, _ workflow.CompareCmd) (*workflow.CompareResult, error) {
	return nil, nil
}

func (s *recordingRepairWF) ScanAndMatch(_ context.Context, _ workflow.ScanAndMatchCmd) (*workflow.ScanAndMatchResult, error) {
	return nil, nil
}

func (s *recordingRepairWF) numCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

// driftRepairFixture holds a completed live result and the apply pieces the
// drift tests share: a pipeline ApplyResult claiming the old poster was
// organized, plus the applyCmd pass 1 ran with.
type driftRepairFixture struct {
	tracker  resultstore.Store
	wf       *recordingRepairWF
	inputs   applyPhaseInputs
	afc      *ApplyFileContext
	applyCmd workflow.ApplyCmd
	pipeline *workflow.ApplyResult
}

func newDriftRepairFixture(t *testing.T, movieID string) *driftRepairFixture {
	t.Helper()
	filePath := "/input/" + movieID + ".mp4"
	tracker := resultstore.New(1, []string{filePath})
	snapshot := &models.Movie{ID: movieID, Title: "Scraped", Poster: models.PosterState{
		PosterURL: "https://old.example/poster.jpg", ShouldCropPoster: true,
	}}
	tracker.UpdateFileResult(filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieID},
		Status:        models.JobStatusCompleted,
		Movie:         snapshot,
	})

	wf := &recordingRepairWF{pipeTitle: "Repaired"}
	inputs := applyPhaseInputs{
		JobID:       models.NewJobID(),
		Broadcaster: &stubBroadcaster{},
		Updater:     tracker,
		WF:          wf,
	}
	match := models.FileMatchInfo{Path: filePath, MovieID: movieID}
	afc := &ApplyFileContext{FilePath: filePath, Match: match, Destination: "/dest"}
	applyCmd := workflow.ApplyCmd{
		Movie:       snapshot,
		Match:       match,
		DestPath:    "/dest",
		GenerateNFO: true,
	}
	// Pass 1 output: organized to /dest/<movieID>/<movieID>.mp4 with a movie
	// the merge/display-title steps stamped.
	pipelineMovie := snapshot.Clone()
	pipelineMovie.Title = "Organized"
	pipeline := &workflow.ApplyResult{
		Movie:          pipelineMovie,
		OrganizeResult: &organizer.OrganizeResult{NewPath: "/dest/" + movieID + "/" + movieID + ".mp4", FolderPath: "/dest/" + movieID, Moved: true},
	}
	return &driftRepairFixture{tracker: tracker, wf: wf, inputs: inputs, afc: afc, applyCmd: applyCmd, pipeline: pipeline}
}

// TestInterpretApplyResult_RepairsMidApplyPosterDrift pins Codex P2-B: a
// manual crop that committed AFTER wf.Apply captured its snapshot but BEFORE
// the success write-back is merged into the envelope — pre-fix the file was
// then reported organized while its on-disk poster/NFO still described the
// OLD source/bounds, with no later organize that would fix them. The success
// path must DETECT the drift under the write-back lock and re-run the poster
// write from the live (merged) state BEFORE reporting success.
func TestInterpretApplyResult_RepairsMidApplyPosterDrift(t *testing.T) {
	fx := newDriftRepairFixture(t, "DRIFT-1")

	// The crop lands after pass 1 captured its snapshot: the envelope's live
	// poster state diverges from what pass 1 physically wrote.
	bounds := midOrganizeCrop(t, fx.tracker, "/input/DRIFT-1.mp4")

	cfg := ApplyPhaseConfig{Download: true, GenerateNFO: true}
	inputs := fx.inputs
	inputs.NFOEnabled = true
	outcome := interpretApplyResult("/input/DRIFT-1.mp4", fx.applyCmd.Movie, time.Now(), time.Minute, inputs, cfg,
		context.Background(), fx.afc, fx.applyCmd, fx.pipeline, nil)

	require.True(t, outcome.Success, "outcome: %+v", outcome)

	// The repair pass re-issued Apply exactly once, organized-mode OFF (the
	// file already moved), with the MERGED movie — live poster state over
	// pipeline metadata — and Match repointed at the organized location.
	require.Equal(t, 1, fx.wf.numCalls(), "exactly one repair pass must run when no further edit lands mid-repair")
	repairCmd := fx.wf.calls[0]
	assert.True(t, repairCmd.Organize.Skip, "the repair pass must not re-organize the already-moved file")
	require.NotNil(t, repairCmd.Movie)
	assertLivePosterPreserved(t, repairCmd.Movie, bounds)
	assert.Equal(t, "/dest/DRIFT-1/DRIFT-1.mp4", repairCmd.Match.Path, "the repair match must point at the moved file so NFO stream details resolve")
	assert.Equal(t, "/dest/DRIFT-1", repairCmd.DestPath, "the repair pass targets the organized folder")

	// The envelope carries the repair pass's movie (live poster + the
	// repair pipeline's metadata) and the outcome reports it.
	stored, err := fx.tracker.GetMovieResult("/input/DRIFT-1.mp4")
	require.NoError(t, err)
	assert.Equal(t, "Repaired", stored.Movie.Title)
	assertLivePosterPreserved(t, stored.Movie, bounds)
	require.NotNil(t, outcome.Movie)
	assert.Equal(t, "Repaired", outcome.Movie.Title)
}

// TestInterpretApplyResult_NoDriftSkipsRepair is the negative control: with
// no mid-apply edit the success write-back finds live == applied poster
// state and no repair pass runs.
func TestInterpretApplyResult_NoDriftSkipsRepair(t *testing.T) {
	fx := newDriftRepairFixture(t, "DRIFT-2")

	cfg := ApplyPhaseConfig{Download: true, GenerateNFO: true}
	inputs := fx.inputs
	inputs.NFOEnabled = true
	outcome := interpretApplyResult("/input/DRIFT-2.mp4", fx.applyCmd.Movie, time.Now(), time.Minute, inputs, cfg,
		context.Background(), fx.afc, fx.applyCmd, fx.pipeline, nil)

	require.True(t, outcome.Success, "outcome: %+v", outcome)
	assert.Equal(t, 0, fx.wf.numCalls(), "no drift — the poster write must NOT be re-run")
	stored, err := fx.tracker.GetMovieResult("/input/DRIFT-2.mp4")
	require.NoError(t, err)
	assert.Equal(t, "Organized", stored.Movie.Title)
}

// TestInterpretApplyResult_DriftRepairGates pins the no-physical-artifact
// gates: drift detection alone must never trigger a repair when pass 1 wrote
// nothing the poster state could have drifted on (dry-run; neither download
// nor NFO generation enabled) or no workflow seam is wired (nil WF, as in
// direct interpret harnesses).
func TestInterpretApplyResult_DriftRepairGates(t *testing.T) {
	run := func(t *testing.T, mutate func(*driftRepairFixture, *applyPhaseInputs, *ApplyPhaseConfig, *workflow.ApplyCmd)) int {
		fx := newDriftRepairFixture(t, "DRIFT-G")
		inputs := fx.inputs
		inputs.NFOEnabled = true
		cfg := ApplyPhaseConfig{Download: true, GenerateNFO: true}
		applyCmd := fx.applyCmd
		if mutate != nil {
			mutate(fx, &inputs, &cfg, &applyCmd)
		}
		midOrganizeCrop(t, fx.tracker, "/input/DRIFT-G.mp4")
		outcome := interpretApplyResult("/input/DRIFT-G.mp4", applyCmd.Movie, time.Now(), time.Minute, inputs, cfg,
			context.Background(), fx.afc, applyCmd, fx.pipeline, nil)
		require.True(t, outcome.Success, "outcome: %+v", outcome)
		// The envelope merge still lands live poster state; only the
		// physical rewrite is gated away.
		stored, err := fx.tracker.GetMovieResult("/input/DRIFT-G.mp4")
		require.NoError(t, err)
		assert.Equal(t, "https://live.example/user-poster.jpg", stored.Movie.Poster.PosterURL)
		return fx.wf.numCalls()
	}

	t.Run("dry run", func(t *testing.T) {
		assert.Equal(t, 0, run(t, func(_ *driftRepairFixture, _ *applyPhaseInputs, _ *ApplyPhaseConfig, cmd *workflow.ApplyCmd) {
			cmd.DryRun = true
		}))
	})
	t.Run("no physical poster artifacts", func(t *testing.T) {
		assert.Equal(t, 0, run(t, func(_ *driftRepairFixture, _ *applyPhaseInputs, cfg *ApplyPhaseConfig, cmd *workflow.ApplyCmd) {
			cfg.Download = false
			cfg.GenerateNFO = false
			cmd.GenerateNFO = false
		}))
	})
	t.Run("nil workflow seam", func(t *testing.T) {
		assert.Equal(t, 0, run(t, func(_ *driftRepairFixture, inputs *applyPhaseInputs, _ *ApplyPhaseConfig, _ *workflow.ApplyCmd) {
			inputs.WF = nil
		}))
	})
}

// TestInterpretApplyResult_DriftRepairApplyFailure keeps the file a SUCCESS
// when the repair pass itself fails: the file IS organized; only its poster
// output lags the mid-apply edit (the envelope carries it). The outcome must
// fall back to pass 1's result rather than failing an organized file.
func TestInterpretApplyResult_DriftRepairApplyFailure(t *testing.T) {
	fx := newDriftRepairFixture(t, "DRIFT-3")
	fx.wf.applyErr = errors.New("simulated repair download failure")

	midOrganizeCrop(t, fx.tracker, "/input/DRIFT-3.mp4")

	cfg := ApplyPhaseConfig{Download: true, GenerateNFO: true}
	inputs := fx.inputs
	inputs.NFOEnabled = true
	outcome := interpretApplyResult("/input/DRIFT-3.mp4", fx.applyCmd.Movie, time.Now(), time.Minute, inputs, cfg,
		context.Background(), fx.afc, fx.applyCmd, fx.pipeline, nil)

	require.True(t, outcome.Success, "a failed repair pass must not fail an organized file: %+v", outcome)
	assert.Equal(t, 1, fx.wf.numCalls())
	assert.Equal(t, "Organized", outcome.Movie.Title, "the outcome keeps pass 1's (last physically completed) movie")
}

// TestInterpretApplyResult_DriftRepairMovielessResult covers a repair pass
// that returns no movie at all: the earlier result stands and the loop
// stops without touching the envelope again.
func TestInterpretApplyResult_DriftRepairMovielessResult(t *testing.T) {
	fx := newDriftRepairFixture(t, "DRIFT-4")
	fx.wf.omitMovie = true

	midOrganizeCrop(t, fx.tracker, "/input/DRIFT-4.mp4")

	cfg := ApplyPhaseConfig{Download: true, GenerateNFO: true}
	inputs := fx.inputs
	inputs.NFOEnabled = true
	outcome := interpretApplyResult("/input/DRIFT-4.mp4", fx.applyCmd.Movie, time.Now(), time.Minute, inputs, cfg,
		context.Background(), fx.afc, fx.applyCmd, fx.pipeline, nil)

	require.True(t, outcome.Success, "outcome: %+v", outcome)
	assert.Equal(t, 1, fx.wf.numCalls())
	assert.Equal(t, "Organized", outcome.Movie.Title)
}

// TestInterpretApplyResult_DriftRepairStopsOnMidRepairRekey: the live result
// is re-keyed (A→B) while the repair pass runs. The repair write-back
// skips the store write (P2-5 keeps B wholesale) and the loop must stop —
// B's own writers own its disk representation.
func TestInterpretApplyResult_DriftRepairStopsOnMidRepairRekey(t *testing.T) {
	fx := newDriftRepairFixture(t, "DRIFT-5")
	fx.wf.onApply = func(call int) {
		if call == 1 {
			rekeyLiveResult(t, fx.tracker, "/input/DRIFT-5.mp4", "DRIFT-5B")
		}
	}

	midOrganizeCrop(t, fx.tracker, "/input/DRIFT-5.mp4")

	cfg := ApplyPhaseConfig{Download: true, GenerateNFO: true}
	inputs := fx.inputs
	inputs.NFOEnabled = true
	outcome := interpretApplyResult("/input/DRIFT-5.mp4", fx.applyCmd.Movie, time.Now(), time.Minute, inputs, cfg,
		context.Background(), fx.afc, fx.applyCmd, fx.pipeline, nil)

	require.True(t, outcome.Success, "outcome: %+v", outcome)
	assert.Equal(t, 1, fx.wf.numCalls(), "a mid-repair rekey must stop the loop — no second repair pass")
	stored, err := fx.tracker.GetMovieResult("/input/DRIFT-5.mp4")
	require.NoError(t, err)
	assert.Equal(t, "DRIFT-5B", stored.Movie.ID, "the re-keyed live movie survives untouched")
}

// TestInterpretApplyResult_DriftRepairConvergesOnSecondPass: an edit lands
// mid-repair (inside the repair pass's own snapshot→write-back window). The
// pass-1 write-back catches the drift, pass 1 of repair runs against the
// first live state, its OWN write-back detects the second edit and runs one
// more pass; once the live state stays put, the loop converges.
func TestInterpretApplyResult_DriftRepairConvergesOnSecondPass(t *testing.T) {
	fx := newDriftRepairFixture(t, "DRIFT-6")
	fx.wf.onApply = func(call int) {
		if call == 1 {
			// A second crop lands while repair pass 1 is "downloading".
			require.NoError(t, fx.tracker.AtomicUpdateFileResult("/input/DRIFT-6.mp4", func(current *resultstore.MovieResult) (*resultstore.MovieResult, error) {
				m := current.Movie.Clone()
				m.Poster.CropBounds = &models.CropBounds{X: 5, Y: 6, Width: 200, Height: 300, ImageWidth: 1000, ImageHeight: 1500, MaxPosterHeight: 800}
				current.Movie = m
				return current, nil
			}))
		}
	}

	midOrganizeCrop(t, fx.tracker, "/input/DRIFT-6.mp4")

	cfg := ApplyPhaseConfig{Download: true, GenerateNFO: true}
	inputs := fx.inputs
	inputs.NFOEnabled = true
	outcome := interpretApplyResult("/input/DRIFT-6.mp4", fx.applyCmd.Movie, time.Now(), time.Minute, inputs, cfg,
		context.Background(), fx.afc, fx.applyCmd, fx.pipeline, nil)

	require.True(t, outcome.Success, "outcome: %+v", outcome)
	assert.Equal(t, 2, fx.wf.numCalls(), "the mid-repair edit must trigger exactly one extra pass")
	stored, err := fx.tracker.GetMovieResult("/input/DRIFT-6.mp4")
	require.NoError(t, err)
	require.NotNil(t, stored.Movie)
	require.NotNil(t, stored.Movie.Poster.CropBounds)
	assert.Equal(t, models.CropBounds{X: 5, Y: 6, Width: 200, Height: 300, ImageWidth: 1000, ImageHeight: 1500, MaxPosterHeight: 800},
		*stored.Movie.Poster.CropBounds, "the envelope converges on the LATEST live crop")
}

// TestInterpretApplyResult_DriftRepairBoundsTheLoop: a continuous edit storm
// (every repair pass races a fresh crop) must not spin forever — the loop
// stops after maxPosterDriftRepairPasses, still reporting the file organized
// (the envelope is authoritative; a re-organize converges the disk).
func TestInterpretApplyResult_DriftRepairBoundsTheLoop(t *testing.T) {
	fx := newDriftRepairFixture(t, "DRIFT-7")
	fx.wf.onApply = func(call int) {
		// Every repair pass races a NEW crop with call-distinct bounds, so
		// each write-back detects fresh drift.
		require.NoError(t, fx.tracker.AtomicUpdateFileResult("/input/DRIFT-7.mp4", func(current *resultstore.MovieResult) (*resultstore.MovieResult, error) {
			m := current.Movie.Clone()
			m.Poster.CropBounds = &models.CropBounds{X: call, Y: call, Width: 100, Height: 150, ImageWidth: 1000, ImageHeight: 1500, MaxPosterHeight: 800}
			current.Movie = m
			return current, nil
		}))
	}

	midOrganizeCrop(t, fx.tracker, "/input/DRIFT-7.mp4")

	cfg := ApplyPhaseConfig{Download: true, GenerateNFO: true}
	inputs := fx.inputs
	inputs.NFOEnabled = true
	outcome := interpretApplyResult("/input/DRIFT-7.mp4", fx.applyCmd.Movie, time.Now(), time.Minute, inputs, cfg,
		context.Background(), fx.afc, fx.applyCmd, fx.pipeline, nil)

	require.True(t, outcome.Success, "outcome: %+v", outcome)
	assert.Equal(t, maxPosterDriftRepairPasses, fx.wf.numCalls(), "the repair loop is bounded")
}

// TestPosterStateDrifted pins the drift predicate's degeneration arms: nil
// or identity-mismatched inputs never count as drift (a re-keyed live movie
// belongs to another family — its own writers own the disk representation),
// and the physically-applied field set alone decides drift.
func TestPosterStateDrifted(t *testing.T) {
	base := func() *models.Movie {
		return &models.Movie{ID: "PD-1", Poster: models.PosterState{
			PosterURL: "https://a.example/p.jpg", CoverURL: "https://a.example/c.jpg",
			ShouldCropPoster: true,
			CropBounds:       &models.CropBounds{X: 1, Y: 2, Width: 3, Height: 4},
		}}
	}
	assert.False(t, posterStateDrifted(nil, base()), "nil applied pass movie is never drift")
	assert.False(t, posterStateDrifted(base(), nil), "a result that lost its movie is never drift")
	other := base()
	other.ID = "PD-2"
	assert.False(t, posterStateDrifted(base(), other), "identity mismatch is a rekey, not drift")
	assert.False(t, posterStateDrifted(base(), base()), "identical state is no drift")

	boundsOnly := base()
	boundsOnly.Poster.CropBounds = &models.CropBounds{X: 9, Y: 9, Width: 3, Height: 4}
	assert.True(t, posterStateDrifted(base(), boundsOnly), "changed bounds drift")

	clearedBounds := base()
	clearedBounds.Poster.CropBounds = nil
	assert.True(t, posterStateDrifted(base(), clearedBounds), "cleared bounds drift")
	assert.True(t, posterStateDrifted(clearedBounds, base()), "gained bounds drift")

	intentFlip := base()
	intentFlip.Poster.ShouldCropPoster = false
	assert.True(t, posterStateDrifted(base(), intentFlip), "crop-intent flip drifts")

	envelopeOnly := base()
	envelopeOnly.Poster.CroppedPosterURL = "/api/v1/temp/posters/job/PD-1.jpg?v=9"
	envelopeOnly.Poster.OriginalPosterURL = "https://orig/p.jpg"
	assert.False(t, posterStateDrifted(base(), envelopeOnly),
		"envelope-only poster pointers (preview URL, reset baseline) never reach the disk inside apply")
}

// TestInterpretApplyResult_DriftRepairForcesPosterReplacement pins Codex
// P2-A at the worker seam: drift = the mid-apply edit changed the effective
// poster source while leaving CropBounds NIL (poster-from-URL / source
// edit). The downloader's exists-skip would keep the poster the first
// pass installed, so the repair pass must carry ForcePosterReplace to make
// the rewrite REPLACE, not keep, the stale destination.
func TestInterpretApplyResult_DriftRepairForcesPosterReplacement(t *testing.T) {
	fx := newDriftRepairFixture(t, "FORCE-1")

	// A poster-from-URL edit lands mid-apply: new source, no crop bounds.
	require.NoError(t, fx.tracker.AtomicUpdateFileResult("/input/FORCE-1.mp4", func(current *resultstore.MovieResult) (*resultstore.MovieResult, error) {
		m := current.Movie.Clone()
		m.Poster.PosterURL = "https://live.example/user-poster.jpg"
		m.Poster.CropBounds = nil
		current.Movie = m
		return current, nil
	}))

	cfg := ApplyPhaseConfig{Download: true, GenerateNFO: true}
	inputs := fx.inputs
	inputs.NFOEnabled = true
	outcome := interpretApplyResult("/input/FORCE-1.mp4", fx.applyCmd.Movie, time.Now(), time.Minute, inputs, cfg,
		context.Background(), fx.afc, fx.applyCmd, fx.pipeline, nil)

	require.True(t, outcome.Success, "outcome: %+v", outcome)
	require.Equal(t, 1, fx.wf.numCalls())
	repairCmd := fx.wf.calls[0]
	assert.True(t, repairCmd.ForcePosterReplace,
		"the repair pass must force-replace the stale poster — the exists-skip would keep the pass-1 image")
	require.NotNil(t, repairCmd.Movie)
	assert.Equal(t, "https://live.example/user-poster.jpg", repairCmd.Movie.Poster.PosterURL)
	assert.Nil(t, repairCmd.Movie.Poster.CropBounds, "the nil-bounds drift case is the one P2-A covers")
}
