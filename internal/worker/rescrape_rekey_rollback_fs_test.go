package worker

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/poster"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fsBackedPosterGen is a snapshot-capable PosterGenerator whose assets live in
// a REAL PosterManager over an in-memory filesystem. GeneratePoster simulates a
// successful generation by writing the destination files directly (no HTTP),
// so the pre-generation SnapshotPosterAssets / post-failure RestorePosterAssets
// legs exercise the same byte-level filesystem semantics as production wiring
// (workflow.factory.go: poster.NewPosterManager + NewScrapePosterGenerator).
type fsBackedPosterGen struct {
	*poster.ScrapePosterGenerator
	fs      afero.Fs
	tempDir string
}

func (g *fsBackedPosterGen) GeneratePoster(_ context.Context, jobID string, movie *models.Movie) error {
	dir := filepath.Join(g.tempDir, "posters", jobID)
	if err := g.fs.MkdirAll(dir, 0o777); err != nil {
		return err
	}
	if err := afero.WriteFile(g.fs, filepath.Join(dir, movie.ID+"-full.jpg"), []byte("generated-full-"+movie.ID), 0o644); err != nil {
		return err
	}
	return afero.WriteFile(g.fs, filepath.Join(dir, movie.ID+".jpg"), []byte("generated-preview-"+movie.ID), 0o644)
}

// TestRescrapePhase_Rescrape_PersistFailureRekeyKeepsRestoredOriginAssets pins
// the interplay flagged as P1-2: for an A→B rekey the success-path orphan
// cleanup DELETES origin A's poster files (withRescrapeStatus), and a failed
// in-section envelope persist then rolls memory back onto A — the movie the
// restore re-keys to must still resolve its poster assets on disk afterwards.
// The rollback restores A's pre-cleanup bytes from the origin snapshot, so the
// round-trip must leave A's files PRESENT (not merely referenced) and B's
// freshly generated files gone.
func TestRescrapePhase_Rescrape_PersistFailureRekeyKeepsRestoredOriginAssets(t *testing.T) {
	const (
		movieA   = "FSRB-ORIG"
		movieB   = "FSRB-ZDEST"
		filePath = "/source/fsrb.mp4"
	)
	memfs := afero.NewMemMapFs()
	tempDir := "/tmp/rescrape-fs"
	jobID := models.NewJobID()
	mgr := poster.NewPosterManager(memfs, tempDir, nil)
	gen := &fsBackedPosterGen{
		ScrapePosterGenerator: poster.NewScrapePosterGenerator(mgr, "", ""),
		fs:                    memfs,
		tempDir:               tempDir,
	}

	// Seed origin A's cached poster assets.
	posterDir := filepath.Join(tempDir, "posters", jobID.String())
	require.NoError(t, memfs.MkdirAll(posterDir, 0o777))
	aFull := filepath.Join(posterDir, movieA+"-full.jpg")
	aPrev := filepath.Join(posterDir, movieA+".jpg")
	require.NoError(t, afero.WriteFile(memfs, aFull, []byte("origin-full-A"), 0o644))
	require.NoError(t, afero.WriteFile(memfs, aPrev, []byte("origin-preview-A"), 0o644))

	tracker := resultstore.New(1, []string{filePath})
	tracker.UpdateFileResult(filePath, &resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: filePath, MovieID: movieA},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{
			ID:     movieA,
			Title:  "Old A",
			Poster: models.PosterState{PosterURL: "https://old.invalid/a.jpg", CroppedPosterURL: "/api/v1/temp/posters/" + jobID.String() + "/" + movieA + ".jpg"},
		},
	})
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie: &models.Movie{ID: movieB, Title: "Corrected", Poster: models.PosterState{PosterURL: "https://new.invalid/b.jpg"}},
	}}
	inputs := rescrapePhaseInputs{
		JobID:     jobID,
		WF:        wf,
		PosterGen: gen,
		ResultMap: tracker,
		Finder:    tracker,
		Lifecycle: &stubLifecycle{},
		Fs:        memfs,
		TempDir:   tempDir,
		PersistEnvelope: func() error {
			return errors.New("job repository unavailable")
		},
	}

	outcome, err := NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: movieA, FilePath: filePath})
	require.NoError(t, err)
	require.Equal(t, models.RescrapeStatusSuccess, outcome.Status)
	require.Error(t, outcome.PersistErr, "the in-section persist failure must surface on the outcome")
	assert.Equal(t, movieA, tracker.GetCurrentMovieID(filePath),
		"the rollback re-keys memory back to origin A")

	// The restored commit references A's poster URLs — the files MUST still
	// resolve on disk: the origin-snapshot restore leg must outlive the
	// success-path orphan cleanup that deleted them before the persist.
	fullBytes, fullErr := afero.ReadFile(memfs, aFull)
	require.NoError(t, fullErr, "origin A's full poster must survive the persist-failure rollback (orphan cleanup deleted it pre-persist)")
	assert.Equal(t, "origin-full-A", string(fullBytes))
	prevBytes, prevErr := afero.ReadFile(memfs, aPrev)
	require.NoError(t, prevErr, "origin A's preview must survive the persist-failure rollback")
	assert.Equal(t, "origin-preview-A", string(prevBytes))

	// Symmetric: the rolled-back destination B keeps no generated assets.
	_, bFullErr := memfs.Stat(filepath.Join(posterDir, movieB+"-full.jpg"))
	assert.True(t, errors.Is(bFullErr, afero.ErrFileNotFound) || bFullErr != nil,
		"the rolled-back rescrape leaves no fresh destination assets behind")
}
