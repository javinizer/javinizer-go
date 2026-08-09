package worker

import (
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// audit R17 candidate F-R17-1: a losing rescrape whose generation FAILED
// (PosterError ⇒ no genSHA fingerprints ⇒ verify degenerates to the legacy
// "allow" verdict) must NOT rewind a concurrent WINNER rescrape's committed
// canonical bytes. Choreography (all sections honored, K-serialized in
// production — the interleave itself is what this test models):
//
//	t1  R1 (loser): parks OLD pair aside; generation FAILS (PosterError),
//	    writes nothing. genSHA stays nil.
//	t2  R2 (winner): parks nothing (R1 emptied canonical); generates healthy
//	    W bytes at both canonical legs.
//	t3  R2 wins the CAS commit (fresh row references W bytes).
//	t4  R1's conflict closeout runs: legacy verdict "allow" ⇒ Remove(W) +
//	    rename(OLD) ⇒ the committed row's canonical bytes are STOLEN.
func TestWithRescrapeStatusLoserFailedGenMustNotRewindWinner(t *testing.T) {
	fs := afero.NewMemMapFs()
	jobID := models.NewJobID()
	dir := filepath.Join("/tmp", "posters", jobID.String())
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-a", "LG-1", "")
	lc := rescrapeLifecycleShim(jobID, fs, store)

	fullCanon := filepath.Join(dir, "LG-1-full.jpg")
	cropCanon := filepath.Join(dir, "LG-1.jpg")

	// committed pre-op bytes both R1 and R2 see:
	require.NoError(t, afero.WriteFile(fs, fullCanon, []byte("OLD-full"), 0o644))
	require.NoError(t, afero.WriteFile(fs, cropCanon, []byte("OLD-crop"), 0o644))

	// t1 — R1 parks + its generation FAILS with no bytes written (PosterError).
	parkedR1 := parkCanonicalPosterPair(fs, dir, "LG-1", 0)
	require.True(t, parkedR1.hadFull)
	require.True(t, parkedR1.hadCrop)

	// t2 — R2 parks (canonical empty thanks to R1) and generates healthy bytes.
	parkedR2 := parkCanonicalPosterPair(fs, dir, "LG-1", 0)
	require.False(t, parkedR2.hadFull)
	require.False(t, parkedR2.hadCrop)
	require.NoError(t, afero.WriteFile(fs, fullCanon, []byte("WINNER-full"), 0o644))
	require.NoError(t, afero.WriteFile(fs, cropCanon, []byte("WINNER-crop"), 0o644))

	// t3 — R2 closeout as the healthy SUCCESS winner: parked pair (empty) discarded.
	_, err := withRescrapeStatus(lc, func(scope *rescrapeGenScope) (*RescrapeResult, *resultstore.MovieResult, error) {
		scope.parked = parkedR2
		scope.preExistedPair = false
		return &RescrapeResult{Status: models.RescrapeStatusSuccess},
			&resultstore.MovieResult{Movie: &models.Movie{ID: "LG-1"}, OrchestrationState: models.OrchestrationState{PosterGenerated: true}}, nil
	})
	require.NoError(t, err)

	// t4 — R1 loses the CAS: conflict closeout with PosterError (genSHA nil ⇒
	// legacy verify verdict ⇒ unconditional rewind).
	genErr := "download wedged"
	_, err = withRescrapeStatus(lc, func(scope *rescrapeGenScope) (*RescrapeResult, *resultstore.MovieResult, error) {
		scope.parked = parkedR1
		scope.preExistedPair = true
		return &RescrapeResult{Status: models.RescrapeStatusConflict},
			&resultstore.MovieResult{Movie: &models.Movie{ID: "LG-1"}, OrchestrationState: models.OrchestrationState{PosterGenerated: true, PosterError: &genErr}}, nil
	})
	require.NoError(t, err)

	gotFull, ferr := afero.ReadFile(fs, fullCanon)
	gotCrop, cerr := afero.ReadFile(fs, cropCanon)
	require.NoError(t, ferr)
	require.NoError(t, cerr)
	assert.Equal(t, "WINNER-full", string(gotFull), "loser's legacy-verify closeout must NEVER rewind the winner's committed -full leg")
	assert.Equal(t, "WINNER-crop", string(gotCrop), "loser's legacy-verify closeout must NEVER rewind the winner's committed crop leg")
}

// audit R17 candidate F-R17-1b: same hole through the F-R16-1 success arm —
// Success+PosterError+preExisted now routes to the keyed content-verify
// restore; with genSHA nil the "content verify" is the unconditional legacy
// verdict, rewinding bytes a winner committed AFTER this op's commit but
// BEFORE this op's (delayed) closeout.
func TestWithRescrapeStatusSuccessPosterErrorMustNotRewindWinner(t *testing.T) {
	fs := afero.NewMemMapFs()
	jobID := models.NewJobID()
	dir := filepath.Join("/tmp", "posters", jobID.String())
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-a", "LS-1", "")
	lc := rescrapeLifecycleShim(jobID, fs, store)

	fullCanon := filepath.Join(dir, "LS-1-full.jpg")
	cropCanon := filepath.Join(dir, "LS-1.jpg")
	require.NoError(t, afero.WriteFile(fs, fullCanon, []byte("OLD-full"), 0o644))
	require.NoError(t, afero.WriteFile(fs, cropCanon, []byte("OLD-crop"), 0o644))

	// R1 parks + failed generation (no bytes), commits successfully (its
	// metadata refresh won the CAS), closeout delayed past R2's landing.
	parkedR1 := parkCanonicalPosterPair(fs, dir, "LS-1", 0)
	require.True(t, parkedR1.hadFull)

	// R2 refresh: commits a newer row AND its bytes landed at canonical
	// (winner pathway models a resubmitted rescrape that captured r1 and
	// committed r2 before R1's deferred closeout fired).
	require.NoError(t, afero.WriteFile(fs, fullCanon, []byte("WINNER-full"), 0o644))
	require.NoError(t, afero.WriteFile(fs, cropCanon, []byte("WINNER-crop"), 0o644))

	// R1's success-arm closeout (F-R16-1): lostGeneration && preExisted ⇒
	// keyed content-verify restore — but with no fingerprints this rewinds.
	genErr := "download wedged"
	_, err := withRescrapeStatus(lc, func(scope *rescrapeGenScope) (*RescrapeResult, *resultstore.MovieResult, error) {
		scope.parked = parkedR1
		scope.preExistedPair = true
		return &RescrapeResult{Status: models.RescrapeStatusSuccess},
			&resultstore.MovieResult{Movie: &models.Movie{ID: "LS-1"}, OrchestrationState: models.OrchestrationState{PosterGenerated: true, PosterError: &genErr}}, nil
	})
	require.NoError(t, err)

	gotFull, ferr := afero.ReadFile(fs, fullCanon)
	gotCrop, cerr := afero.ReadFile(fs, cropCanon)
	require.NoError(t, ferr)
	require.NoError(t, cerr)
	assert.Equal(t, "WINNER-full", string(gotFull), "F-R16-1 success arm must not rewind a later winner's -full leg")
	assert.Equal(t, "WINNER-crop", string(gotCrop), "F-R16-1 success arm must not rewind a later winner's crop leg")
}
