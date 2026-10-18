package scrape

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestZeroEffectiveScrapeEvidenceDoesNotBecomeUserCastEdit(t *testing.T) {
	cases := []struct {
		name string
		mode models.UnknownActressMode
		raw  []models.ActressInfo
	}{
		{"absent_with_synthesized_fallback", models.UnknownActressModeFallback, nil},
		{"empty_with_synthesized_fallback", models.UnknownActressModeFallback, []models.ActressInfo{}},
		{"nameless_unusable_entry", models.UnknownActressModeSkip, []models.ActressInfo{{}}},
		{"configured_unknown_filtered", models.UnknownActressModeSkip, []models.ActressInfo{{FirstName: "Unknown"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.cfg.Metadata.NFO.Format.UnknownActressMode = tc.mode
			f.cfg.Metadata.NFO.Format.UnknownActressText = "Unknown"
			const id = "EVIDENCE-329"
			saved, err := f.movieRepo.Upsert(t.Context(), &models.Movie{ID: id, ContentID: id, Title: "Original", Credits: []models.MovieCredit{{Origin: string(models.CreditOriginScrape), CreditedName: "Retained", Scraped: models.Actress{FirstName: "Retained"}}}})
			require.NoError(t, err)
			require.Len(t, saved.Credits, 1)
			raw := &models.ScraperResult{Source: "fixture", ID: id, ContentID: id, Title: "Refreshed", Actresses: tc.raw}
			engine := f.withScraper(raw.Source, raw, nil).withPriority([]string{raw.Source}).build()
			fresh, err := engine.Scrape(t.Context(), ScrapeCmd{MovieID: id, ForceRefresh: true})
			require.NoError(t, err)
			t.Logf("rawCastLen=%d aggregateCastLen=%d creditsNil=%t skip=%t", len(tc.raw), len(fresh.Movie.Actresses), fresh.Movie.Credits == nil, fresh.Movie.SkipCreditReconcile)
			persisted, err := f.movieRepo.Upsert(t.Context(), fresh.Movie)
			require.NoError(t, err)
			require.Len(t, persisted.Credits, 1)
			require.Equal(t, saved.Credits[0].ID, persisted.Credits[0].ID)
			require.False(t, persisted.Credits[0].Suppressed)
			require.Equal(t, string(models.CreditOriginScrape), persisted.Credits[0].Origin)
			require.Len(t, persisted.Actresses, 1)
			require.Equal(t, saved.Credits[0].ActressID, persisted.Actresses[0].ID)
		})
	}
}
