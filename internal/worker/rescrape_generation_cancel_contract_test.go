package worker

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type cancelAfterPosterGeneration struct {
	fs                afero.Fs
	tempDir           string
	cancel            context.CancelFunc
	calls             int
	contextErrAtEntry error
}

func (g *cancelAfterPosterGeneration) GeneratePoster(ctx context.Context, jobID string, movie *models.Movie) error {
	g.calls++
	g.contextErrAtEntry = ctx.Err()
	for _, suffix := range []string{".jpg", "-full.jpg"} {
		if err := afero.WriteFile(g.fs, filepath.Join(g.tempDir, "posters", jobID, movie.ID+suffix), []byte("uncommitted"+suffix), 0o644); err != nil {
			return err
		}
	}
	// Synchronous cancellation establishes the exact boundary without sleeps:
	// generation succeeded, but publication has not yet begun.
	g.cancel()
	return nil
}

func TestRescrapeCancellationAfterGenerationPreservesResultAndProvenance(t *testing.T) {
	const file, movieID = "QUERY-001.mp4", "QUERY-001"
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	fs := afero.NewMemMapFs()
	jobID := models.NewJobID()
	posterDir := filepath.Join("temp", "posters", jobID.String())
	require.NoError(t, fs.MkdirAll(posterDir, 0o755))
	for _, suffix := range []string{".jpg", "-full.jpg"} {
		require.NoError(t, afero.WriteFile(fs, filepath.Join(posterDir, movieID+suffix), []byte("committed"+suffix), 0o644))
	}
	store := resultstore.New(1, []string{file})
	store.UpdateFileResult(file, &resultstore.MovieResult{Movie: &models.Movie{ID: movieID, Title: "Committed title"}, Status: models.JobStatusCompleted, Persisted: true})
	store.SetProvenance(file, &resultstore.ProvenanceData{FieldSources: map[string]string{"title": "committed-source"}})
	before := store.SnapshotData()
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{Status: scrape.StatusCompleted, Movie: &models.Movie{ID: movieID, Title: "Uncommitted title"}, FieldSources: map[string]string{"title": "new-source"}}}
	generator := &cancelAfterPosterGeneration{fs: fs, tempDir: "temp", cancel: cancel}
	inputs := makeRescrapeInputs(wf)
	inputs.JobID, inputs.Fs, inputs.TempDir = jobID, fs, "temp"
	inputs.ResultMap, inputs.Finder, inputs.PosterGen = store, store, generator

	result, err := NewRescrapePhase().Rescrape(ctx, inputs, RescrapeCmd{FilePath: file, MovieID: movieID})

	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, result)
	require.Equal(t, 1, wf.scrapeCalled)
	require.Equal(t, 1, generator.calls)
	require.NoError(t, generator.contextErrAtEntry, "cancellation must happen inside successful generation, not before it")
	require.Equal(t, before, store.SnapshotData(), "neither result revision nor provenance may publish after cancellation")
	for _, suffix := range []string{".jpg", "-full.jpg"} {
		bytes, readErr := afero.ReadFile(fs, filepath.Join(posterDir, movieID+suffix))
		require.NoError(t, readErr)
		require.Equal(t, []byte("committed"+suffix), bytes, "cancelled generation must restore the committed poster pair")
	}
	entries, readErr := afero.ReadDir(fs, posterDir)
	require.NoError(t, readErr)
	require.Len(t, entries, 2, "settled cancellation must not leave backups or in-flight witnesses")
}
