package worker

import (
	"context"
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/poster"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRescrapePhase_Rescrape_PremutationPosterFailureDegradesUnderArmedSnapshot
// pins the typed-discrimination degrade leg: a generation failure positively
// marked poster.ErrPosterCacheUntouched (SSRF blocked, download/HTTP failure,
// oversized/undecodable image — the Remove-before-Rename mutation boundary was
// never crossed) must NOT trip the fail-closed branch even with a snapshot
// armed, because NOTHING was mutated: the old cached image still stands and the
// rescraped state commits with the poster failure recorded as metadata. This is
// the long-standing scrape/rescrape contract the e2e suite pins via the
// unreachable-but-legal "e2e.invalid" poster source.
func TestRescrapePhase_Rescrape_PremutationPosterFailureDegradesUnderArmedSnapshot(t *testing.T) {
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{
		Movie: &models.Movie{ID: "RBK-001", Title: "New", Poster: models.PosterState{PosterURL: "https://new.invalid/poster.jpg"}},
	}}
	premutationErr := fmt.Errorf("SSRF validation failed: %w", poster.ErrPosterCacheUntouched)
	gen := &snapshotStubPosterGen{generateErr: premutationErr}
	inputs, tracker, filePath := rescrapePhaseTestInputs(t, wf, gen)

	outcome, err := NewRescrapePhase().Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: "RBK-001", FilePath: filePath})
	require.NoError(t, err)
	require.NotNil(t, outcome)
	assert.Equal(t, models.RescrapeStatusSuccess, outcome.Status,
		"a failure proven pre-mutation (cache untouched) degrades to success-with-metadata, not a hard fail")
	assert.Equal(t, 1, gen.snapshots, "the snapshot is still armed pre-generation")
	assert.Equal(t, 1, gen.generated)
	assert.Equal(t, 0, gen.restores, "nothing mutated — the armed rollback must NOT replay")
	current, cerr := tracker.GetMovieResult(filePath)
	require.NoError(t, cerr)
	assert.Equal(t, "New", current.Movie.Title, "the degrade still commits the rescraped movie")
	require.NotNil(t, current.PosterError, "the degrade records the poster failure on the committed result")
	assert.Contains(t, *current.PosterError, "SSRF validation failed")
}
