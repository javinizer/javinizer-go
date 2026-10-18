package scrape

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraper/javbus"
	"github.com/javinizer/javinizer-go/internal/scraper/mgstage"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/require"
)

type emptyCastRegistrar struct {
	registration scraperutil.ScraperRegistration
}

func (r *emptyCastRegistrar) Register(reg scraperutil.ScraperRegistration) { r.registration = reg }

type emptyCastHTMLParser interface {
	ParseHTML(*goquery.Document, string) (*models.ScraperResult, error)
}

// These pages retain product identity/title while actor metadata is absent,
// filtered placeholder data, or plain text that the adapter does not recognize.
// The factory and ParseHTML below are the real production adapter paths.
func TestRealAdapterNoEvidencePreservesDurableCredit(t *testing.T) {
	cases := []struct {
		name      string
		register  func(scraperutil.ScraperRegistrar)
		html, url string
	}{
		{"javbus_missing_cast", javbus.Register, `<html><head><title>ABC-123 Product - JavBus</title></head><body><div id="info"><p><span class="header">品番:</span> ABC-123</p></div></body></html>`, "https://www.javbus.com/ABC-123"},
		{"javbus_unparsed_placeholder", javbus.Register, `<html><head><title>ABC-123 Product - JavBus</title></head><body><div id="star-div"><a href="/star/abc"><img title="出演者"></a><a href="/star/def"><img title="画像を拡大"></a></div></body></html>`, "https://www.javbus.com/ABC-123"},
		{"mgstage_missing_cast", mgstage.Register, `<html><head><title>Product</title></head><body><table><tr><th>品番：</th><td>ABC-123</td></tr></table></body></html>`, "https://www.mgstage.com/product/product_detail/ABC-123/"},
		{"mgstage_unparsed_plaintext_cast", mgstage.Register, `<html><head><title>Product</title></head><body><table><tr><th>品番：</th><td>ABC-123</td></tr><tr><th>出演：</th><td>Actor Name</td></tr></table></body></html>`, "https://www.mgstage.com/product/product_detail/ABC-123/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reg := &emptyCastRegistrar{}
			tc.register(reg)
			adapter, err := reg.registration.Constructor(scraperutil.ScraperDeps{Settings: models.ScraperSettings{Enabled: true}, GlobalProxy: &models.ProxyConfig{}})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, adapter.Close()) })
			parser, ok := adapter.(emptyCastHTMLParser)
			require.True(t, ok)
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tc.html))
			require.NoError(t, err)
			raw, err := parser.ParseHTML(doc, tc.url)
			require.NoError(t, err)
			require.Equal(t, "ABC-123", raw.ID)
			require.NotNil(t, raw.Actresses, "real adapter allocates an empty slice despite no usable actor evidence")
			require.Empty(t, raw.Actresses)

			f := newFixture(t)
			identity := models.Actress{DMMID: 329001, FirstName: "Canonical", Verified: true, Origin: database.ActressOriginUser}
			require.NoError(t, f.db.Create(&identity).Error)
			saved, err := f.movieRepo.Upsert(t.Context(), &models.Movie{ID: raw.ID, ContentID: raw.ContentID, Title: "Prior product", Credits: []models.MovieCredit{{Origin: string(models.CreditOriginScrape), Source: raw.Source, CreditedName: "Reported", Scraped: models.Actress{DMMID: identity.DMMID, FirstName: "Reported"}}}})
			require.NoError(t, err)
			require.Len(t, saved.Credits, 1)
			credit := saved.Credits[0]
			service := database.NewCollisionService(f.db)
			open, err := service.Collisions.ListOpenByMovie(t.Context(), raw.ContentID)
			require.NoError(t, err)
			require.Len(t, open, 1)
			_, err = service.Resolve(t.Context(), open[0].ID, models.CollisionResolutionKeepIdentity, 0)
			require.NoError(t, err)
			before, err := f.movieRepo.FindByContentID(t.Context(), raw.ContentID)
			require.NoError(t, err)
			require.True(t, before.Credits[0].DisplayForceCanonical)
			require.Equal(t, string(models.CreditOriginScrape), before.Credits[0].Origin)
			engine := f.withScraper(raw.Source, raw, nil).withPriority([]string{raw.Source}).build()
			fresh, err := engine.Scrape(t.Context(), ScrapeCmd{MovieID: raw.ID, ForceRefresh: true})
			require.NoError(t, err)
			require.NotNil(t, fresh.Movie.Credits)
			t.Logf("adapter=%s rawCastNil=%t rawCastLen=%d skipCreditReconcile=%t", raw.Source, raw.Actresses == nil, len(raw.Actresses), fresh.Movie.SkipCreditReconcile)
			persisted, err := f.movieRepo.Upsert(t.Context(), fresh.Movie)
			require.NoError(t, err)
			var collisions int64
			require.NoError(t, f.db.Model(&models.CreditCollision{}).Where("credit_id = ?", credit.ID).Count(&collisions).Error)
			t.Logf("after real-adapter empty refresh: credits=%d resolvedCollisions=%d", len(persisted.Credits), collisions)
			require.Len(t, persisted.Credits, 1, "absent/unparsed scrape data must preserve existing credit and its durable decisions")
			require.True(t, fresh.Movie.SkipCreditReconcile)
			require.Equal(t, credit.ID, persisted.Credits[0].ID)
			require.True(t, persisted.Credits[0].DisplayForceCanonical)
			require.EqualValues(t, 1, collisions)
			require.Len(t, persisted.Actresses, 1)
			require.Equal(t, identity.ID, persisted.Actresses[0].ID)

			// A later complete scrape of the same evidence must reuse the original
			// credit and settled collision rather than reopening the user's decision.
			returned := &models.ScraperResult{Source: raw.Source, ID: raw.ID, ContentID: raw.ContentID, Title: raw.Title, Actresses: []models.ActressInfo{{DMMID: identity.DMMID, FirstName: "Reported"}}}
			f.registry.RegisterInstance(&mockScraper{name: raw.Source, enabled: true, result: returned})
			fresh, err = engine.Scrape(t.Context(), ScrapeCmd{MovieID: raw.ID, ForceRefresh: true})
			require.NoError(t, err)
			persisted, err = f.movieRepo.Upsert(t.Context(), fresh.Movie)
			require.NoError(t, err)
			require.Len(t, persisted.Credits, 1)
			require.Equal(t, credit.ID, persisted.Credits[0].ID)
			require.True(t, persisted.Credits[0].DisplayForceCanonical)
			openAfter, err := service.Collisions.ListOpenByMovie(t.Context(), raw.ContentID)
			require.NoError(t, err)
			require.Empty(t, openAfter)
			var settled models.CreditCollision
			require.NoError(t, f.db.First(&settled, open[0].ID).Error)
			require.Equal(t, models.CollisionStatusResolved, settled.Status)
			require.Equal(t, models.CollisionResolutionKeepIdentity, settled.Resolution)
		})
	}
}

// A no-evidence scrape guard must not pin obsolete members of a real cast.
func TestNonemptyScrapeStillReconcilesObsoleteMembership(t *testing.T) {
	f := newFixture(t)
	id := "PARTIAL-329"
	saved, err := f.movieRepo.Upsert(t.Context(), &models.Movie{ID: id, ContentID: id, Title: "Original", Credits: []models.MovieCredit{
		{Origin: string(models.CreditOriginScrape), Source: "fixture", CreditedName: "Retained", Scraped: models.Actress{FirstName: "Retained"}},
		{Origin: string(models.CreditOriginScrape), Source: "fixture", CreditedName: "Obsolete", Scraped: models.Actress{FirstName: "Obsolete"}},
	}})
	require.NoError(t, err)
	require.Len(t, saved.Credits, 2)
	raw := &models.ScraperResult{Source: "fixture", ID: id, ContentID: id, Title: "Updated", Actresses: []models.ActressInfo{{FirstName: "Retained"}}}
	engine := f.withScraper(raw.Source, raw, nil).withPriority([]string{raw.Source}).build()
	fresh, err := engine.Scrape(t.Context(), ScrapeCmd{MovieID: id, ForceRefresh: true})
	require.NoError(t, err)
	require.False(t, fresh.Movie.SkipCreditReconcile)
	persisted, err := f.movieRepo.Upsert(t.Context(), fresh.Movie)
	require.NoError(t, err)
	require.Len(t, persisted.Credits, 1)
	require.Equal(t, "Retained", persisted.Credits[0].CreditedName)
	require.Len(t, persisted.Actresses, 1)
}

// Explicit user edits remain a distinct operation from scrape omissions.
func TestExplicitUserEmptyCastStillSuppressesMembership(t *testing.T) {
	f := newFixture(t)
	saved, err := f.movieRepo.Upsert(t.Context(), &models.Movie{ID: "USER-329", ContentID: "USER-329", Title: "Original", Credits: []models.MovieCredit{{Origin: string(models.CreditOriginScrape), CreditedName: "Removed", Scraped: models.Actress{FirstName: "Removed"}}}})
	require.NoError(t, err)
	require.Len(t, saved.Credits, 1)
	edit := saved.Clone()
	edit.Credits = nil
	edit.Actresses = []models.Actress{}
	edit.SkipCreditReconcile = false
	persisted, err := f.movieRepo.Upsert(t.Context(), edit)
	require.NoError(t, err)
	require.Empty(t, persisted.Actresses)
	require.Len(t, persisted.Credits, 1)
	require.Equal(t, saved.Credits[0].ID, persisted.Credits[0].ID)
	require.True(t, persisted.Credits[0].Suppressed)
}
