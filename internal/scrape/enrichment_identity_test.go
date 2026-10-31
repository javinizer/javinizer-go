package scrape

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// Register only the sources explicitly supplied by each enrichment scenario.
// withScraper appends priorities; retaining defaults would query the same mock
// twice concurrently instead of exercising two distinct source instances.
func newEnrichmentFixture(t *testing.T) *testFixture {
	t.Helper()
	f := newFixture(t)
	f.cfg.Scrapers.Priority = nil
	return f
}

func seedEnrichmentIdentities(t *testing.T, f *testFixture, actors ...models.Actress) []models.Actress {
	t.Helper()
	for i := range actors {
		actors[i].Verified = true
		actors[i].Origin = database.ActressOriginUser
		require.NoError(t, f.db.Create(&actors[i]).Error)
	}
	return actors
}

func sharedRomanizationActors() []models.Actress {
	return []models.Actress{
		{DMMID: 1109243, FirstName: "Nagisa", LastName: "Hazuki", JapaneseName: "葉月なぎさ", ThumbURL: "https://example.invalid/nagisa.jpg"},
		{DMMID: 1034095, FirstName: "Nagisa", LastName: "Hazuki", JapaneseName: "葉月渚", ThumbURL: "https://example.invalid/other.jpg"},
	}
}

func requireIdentityCollisionCount(t *testing.T, f *testFixture, want int) {
	t.Helper()
	var collisions []models.CreditCollision
	require.NoError(t, f.db.Where("field = ? AND status = ?", models.CreditFieldIdentityLink, models.CollisionStatusOpen).Find(&collisions).Error)
	require.Len(t, collisions, want)
}

func TestScrapeEnrichmentPreservesNativeIdentityEvidence(t *testing.T) {
	for _, scenario := range []struct{ bothNative, reverse bool }{{}, {bothNative: true}, {bothNative: true, reverse: true}} {
		bothNative := scenario.bothNative
		name := "old_dmm_stage_name_and_native_credit"
		if bothNative {
			name = "distinct_native_credits_with_shared_romanization"
			if scenario.reverse {
				name += "_reversed"
			}
		}
		t.Run(name, func(t *testing.T) {
			f := newEnrichmentFixture(t)
			f.cfg.Metadata.ActressDatabase.Enabled = true
			f.cfg.Metadata.Translation.Enabled = false
			actors := sharedRomanizationActors()
			actors = append(actors, models.Actress{DMMID: 1068778, JapaneseName: "真白美生"})
			actors = seedEnrichmentIdentities(t, f, actors...)
			native := []models.ActressInfo{{JapaneseName: actors[0].JapaneseName}}
			wantIDs := []uint{actors[2].ID, actors[0].ID}
			if bothNative {
				native = append(native, models.ActressInfo{JapaneseName: actors[1].JapaneseName})
				wantIDs = []uint{actors[0].ID, actors[1].ID}
				if scenario.reverse {
					native[0], native[1] = native[1], native[0]
				}
			} else {
				f.withScraper("dmm", &models.ScraperResult{Source: "dmm", Language: "ja", ID: "HMN-065", ContentID: "hmn00065", Title: "Fixture", Actresses: []models.ActressInfo{{DMMID: actors[2].DMMID, JapaneseName: actors[2].JapaneseName}}}, nil)
			}
			raw := &models.ScraperResult{Source: "javlibrary", Language: "ja", ID: "HMN-065", ContentID: "hmn00065", Title: "Fixture", Actresses: native}
			original := raw.Clone()
			f.withScraper("javlibrary", raw, nil)
			s := f.build()
			s.cfg.CollisionPolicy = "auto_keep"
			var firstCreditIDs []uint
			for refresh := 0; refresh < 2; refresh++ {
				result, err := s.Scrape(t.Context(), ScrapeCmd{MovieID: "HMN-065", ForceRefresh: true})
				require.NoError(t, err)
				require.NotNil(t, result.Movie)
				require.Len(t, result.Movie.Actresses, 2)
				require.Len(t, result.Movie.Credits, 2, "derived names must not coalesce distinct source identities")
				for _, credit := range result.Movie.Credits {
					if credit.Source != "javlibrary" {
						continue
					}
					require.Empty(t, credit.Scraped.FirstName)
					require.Empty(t, credit.Scraped.LastName)
					require.Zero(t, credit.Scraped.DMMID, "catalog lookup must not manufacture authoritative identity")
					require.Empty(t, credit.Scraped.ThumbURL)
					require.Equal(t, credit.Scraped.JapaneseName, credit.CreditedJapaneseName)
					require.Equal(t, credit.CreditedJapaneseName, credit.CreditedName)
					require.Empty(t, credit.ReportedThumbURL, "catalog image is not source-reported evidence")
					var display *models.Actress
					for i := range result.Movie.Actresses {
						if result.Movie.Actresses[i].JapaneseName == credit.CreditedJapaneseName {
							display = &result.Movie.Actresses[i]
						}
					}
					require.NotNil(t, display)
					require.Equal(t, "Nagisa", display.FirstName)
					require.Equal(t, "Hazuki", display.LastName)
					require.NotEmpty(t, display.ThumbURL, "display enrichment must remain available")
				}
				require.Equal(t, original, raw, "retained raw source evidence must not change")
				translations := make([]models.ActressTranslationData, len(result.Movie.Actresses))
				for i, a := range result.Movie.Actresses {
					translations[i] = models.ActressTranslationData{ActressIndex: i, Language: "en", DisplayName: "translated " + a.JapaneseName, SourceName: "translation:fixture"}
				}
				saved, err := database.NewMovieUpserter(database.NewMovieRepository(f.db)).UpsertWithTranslations(t.Context(), result.Movie, nil, translations)
				require.NoError(t, err)
				require.Len(t, saved.Credits, 2)
				ids := make([]uint, 0, 2)
				creditIDs := make([]uint, 0, 2)
				for _, c := range saved.Credits {
					ids = append(ids, c.ActressID)
					creditIDs = append(creditIDs, c.ID)
				}
				require.ElementsMatch(t, wantIDs, ids)
				var translated []models.ActressTranslation
				require.NoError(t, f.db.Where("language = ?", "en").Find(&translated).Error)
				require.Len(t, translated, 2)
				for _, translation := range translated {
					var owner models.Actress
					require.NoError(t, f.db.First(&owner, translation.ActressID).Error)
					require.Equal(t, "translated "+owner.JapaneseName, translation.DisplayName, "translation index must stay aligned with the native credit")
				}
				if refresh == 0 {
					firstCreditIDs = creditIDs
				} else {
					require.ElementsMatch(t, firstCreditIDs, creditIDs)
				}
				counts, err := database.NewCreditCollisionRepository(f.db).CountOpenByMovieBatch(t.Context(), []string{saved.ContentID})
				require.NoError(t, err)
				require.Zero(t, counts[saved.ContentID], "apply gate must remain clear")
				var candidates int64
				require.NoError(t, f.db.Model(&models.Actress{}).Where("verified = ?", false).Count(&candidates).Error)
				require.Zero(t, candidates)
			}
			cached, err := s.Scrape(t.Context(), ScrapeCmd{MovieID: "HMN-065"})
			require.NoError(t, err)
			require.True(t, cached.Cached)
			require.True(t, cached.Movie.SkipCreditReconcile)
			for _, name := range f.cfg.Scrapers.Priority {
				instance, ok := f.registry.GetInstance(name)
				require.True(t, ok)
				require.Equal(t, 2, instance.(*mockScraper).callCount, "each configured source is queried once per refresh, not on cache hit")
			}
			saved, err := database.NewMovieUpserter(database.NewMovieRepository(f.db)).Upsert(t.Context(), cached.Movie)
			require.NoError(t, err)
			require.Len(t, saved.Credits, 2)
			// Loaded/replayed credits no longer carry transient Scraped evidence.
			// These positive-DMM canonical owners must remain authoritative.
			replayJSON, err := json.Marshal(saved)
			require.NoError(t, err)
			var replay models.Movie
			require.NoError(t, json.Unmarshal(replayJSON, &replay))
			replay.SkipCreditReconcile = false
			replay.CreditPolicy = "auto_keep"
			replayed, err := database.NewMovieUpserter(database.NewMovieRepository(f.db)).Upsert(t.Context(), &replay)
			require.NoError(t, err)
			require.Len(t, replayed.Credits, 2)
			replayIDs := []uint{replayed.Credits[0].ActressID, replayed.Credits[1].ActressID}
			require.ElementsMatch(t, wantIDs, replayIDs)
			requireIdentityCollisionCount(t, f, 0)
			var stored []models.Actress
			require.NoError(t, f.db.Order("id").Find(&stored).Error)
			require.Equal(t, actors, stored, "neither verified identities nor catalog fields may be merged or rewritten")
		})
	}
}

func TestScrapeEnrichmentKeepsAmbiguousSourceEvidenceBlocked(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		info                       models.ActressInfo
		duplicateJP, aliasConflict bool
	}{
		{name: "shared_English_only", info: models.ActressInfo{FirstName: "Nagisa", LastName: "Hazuki"}},
		{name: "source_supplies_both_languages", info: models.ActressInfo{FirstName: "Nagisa", LastName: "Hazuki", JapaneseName: "葉月なぎさ"}},
		{name: "duplicate_native_name", info: models.ActressInfo{JapaneseName: "葉月なぎさ"}, duplicateJP: true},
		{name: "curated_alias_conflicts_with_native_owner", info: models.ActressInfo{JapaneseName: "葉月なぎさ"}, aliasConflict: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newEnrichmentFixture(t)
			f.cfg.Metadata.ActressDatabase.Enabled = true
			f.cfg.Metadata.Translation.Enabled = false
			actors := sharedRomanizationActors()
			if tc.duplicateJP {
				actors[1].JapaneseName = actors[0].JapaneseName
				actors[1].FirstName = "Different"
			}
			actors = seedEnrichmentIdentities(t, f, actors...)
			if tc.aliasConflict {
				require.NoError(t, f.db.Create(&models.ActressAlias{AliasName: actors[0].JapaneseName, CanonicalName: actors[1].JapaneseName}).Error)
			}
			// Disable alias display conversion, not the resolver's independent curated
			// alias evidence. The conflicting owner must still prevent a silent link.
			f.cfg.Metadata.ActressDatabase.ConvertAlias = false
			f.withScraper("javlibrary", &models.ScraperResult{Source: "javlibrary", Language: "ja", ID: "TEST-555", ContentID: "test555", Title: "Fixture", Actresses: []models.ActressInfo{tc.info}}, nil)
			s := f.build()
			s.cfg.CollisionPolicy = "auto_keep"
			result, err := s.Scrape(t.Context(), ScrapeCmd{MovieID: "TEST-555", ForceRefresh: true})
			require.NoError(t, err)
			require.Len(t, result.Movie.Credits, 1)
			require.Equal(t, tc.info.FirstName, result.Movie.Credits[0].Scraped.FirstName)
			require.Equal(t, tc.info.LastName, result.Movie.Credits[0].Scraped.LastName)
			require.Equal(t, tc.info.JapaneseName, result.Movie.Credits[0].Scraped.JapaneseName)
			saved, err := database.NewMovieUpserter(database.NewMovieRepository(f.db)).Upsert(t.Context(), result.Movie)
			require.NoError(t, err)
			require.Len(t, saved.Credits, 1)
			require.False(t, saved.Credits[0].Actress.Verified)
			requireIdentityCollisionCount(t, f, 1)
		})
	}
}

func TestScrapeEnrichmentNativeCreditJSONRoundTrip(t *testing.T) {
	f := newEnrichmentFixture(t)
	f.cfg.Metadata.ActressDatabase.Enabled = true
	f.cfg.Metadata.Translation.Enabled = false
	actors := seedEnrichmentIdentities(t, f, sharedRomanizationActors()...)
	f.withScraper("javlibrary", &models.ScraperResult{Source: "javlibrary", Language: "ja", ID: "TEST-555", ContentID: "test555", Title: "Fixture", Actresses: []models.ActressInfo{{JapaneseName: actors[0].JapaneseName}}}, nil)
	s := f.build()
	result, err := s.Scrape(t.Context(), ScrapeCmd{MovieID: "TEST-555", ForceRefresh: true})
	require.NoError(t, err)
	data, err := json.Marshal(result.Movie)
	require.NoError(t, err)
	var roundTrip models.Movie
	require.NoError(t, json.Unmarshal(data, &roundTrip))
	roundTrip.CreditPolicy = "auto_keep"
	require.Empty(t, roundTrip.Credits[0].Scraped.JapaneseName, "transient evidence is intentionally not serialized")
	saved, err := database.NewMovieUpserter(database.NewMovieRepository(f.db)).Upsert(t.Context(), &roundTrip)
	require.NoError(t, err)
	require.Len(t, saved.Credits, 1)
	require.Equal(t, actors[0].ID, saved.Credits[0].ActressID)
	requireIdentityCollisionCount(t, f, 0)
}

func TestScrapeEnrichmentPreservesPositiveDMMEvidence(t *testing.T) {
	for _, tc := range []struct {
		name    string
		raw     []models.ActressInfo
		wantDMM []int
	}{
		{"positive_ID_over_conflicting_native_name", []models.ActressInfo{{DMMID: 1109243, JapaneseName: "葉月渚"}}, []int{1109243}},
		{"distinct_positive_IDs_share_derived_romanization", []models.ActressInfo{{DMMID: 1109243, JapaneseName: "葉月なぎさ"}, {DMMID: 1034095, JapaneseName: "葉月渚"}}, []int{1109243, 1034095}},
		{"unknown_positive_ID_does_not_adopt_native_owner", []models.ActressInfo{{DMMID: 555999, JapaneseName: "葉月なぎさ"}}, []int{555999}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newEnrichmentFixture(t)
			f.cfg.Metadata.ActressDatabase.Enabled = true
			f.cfg.Metadata.Translation.Enabled = false
			seedEnrichmentIdentities(t, f, sharedRomanizationActors()...)
			f.withScraper("dmm", &models.ScraperResult{Source: "dmm", Language: "ja", ID: "TEST-555", ContentID: "test555", Title: "Fixture", Actresses: tc.raw}, nil)
			s := f.build()
			s.cfg.CollisionPolicy = "auto_keep"
			result, err := s.Scrape(t.Context(), ScrapeCmd{MovieID: "TEST-555", ForceRefresh: true})
			require.NoError(t, err)
			require.Len(t, result.Movie.Credits, len(tc.raw))
			require.Len(t, result.Movie.Actresses, len(tc.raw))
			type identityEvidence struct {
				DMMID        int
				JapaneseName string
			}
			wantEvidence := make([]identityEvidence, 0, len(tc.raw))
			for _, info := range tc.raw {
				wantEvidence = append(wantEvidence, identityEvidence{info.DMMID, info.JapaneseName})
			}
			gotEvidence := make([]identityEvidence, 0, len(result.Movie.Credits))
			for i, credit := range result.Movie.Credits {
				gotEvidence = append(gotEvidence, identityEvidence{credit.Scraped.DMMID, credit.Scraped.JapaneseName})
				require.Equal(t, "dmm", credit.Source)
				require.Equal(t, credit.Scraped.JapaneseName, credit.CreditedJapaneseName)
				require.Equal(t, i, credit.OrderIndex)
				require.Equal(t, result.Movie.Actresses[i].DMMID, credit.Scraped.DMMID)
				require.Equal(t, result.Movie.Actresses[i].JapaneseName, credit.Scraped.JapaneseName)
			}
			// Aggregation collects its identities from maps, so its output need
			// not retain raw scraper order. Preserve each ID/name pair and its
			// multiplicity, while checking credit/display order alignment above.
			require.ElementsMatch(t, wantEvidence, gotEvidence)
			saved, err := database.NewMovieUpserter(database.NewMovieRepository(f.db)).Upsert(t.Context(), result.Movie)
			require.NoError(t, err)
			got := make([]int, 0, len(saved.Credits))
			persistedEvidence := make([]identityEvidence, 0, len(saved.Credits))
			for _, c := range saved.Credits {
				require.NotNil(t, c.Actress)
				got = append(got, c.Actress.DMMID)
				persistedEvidence = append(persistedEvidence, identityEvidence{c.Actress.DMMID, c.CreditedJapaneseName})
			}
			require.ElementsMatch(t, tc.wantDMM, got)
			require.ElementsMatch(t, wantEvidence, persistedEvidence)
			requireIdentityCollisionCount(t, f, 0)
		})
	}
}

func TestPostProcessEnrichmentKeepsConflictingDMMComponentsSeparate(t *testing.T) {
	f := newEnrichmentFixture(t)
	actors := seedEnrichmentIdentities(t, f, sharedRomanizationActors()...)
	movie := &models.Movie{ID: "TEST-555", ContentID: "test555", Title: "Fixture", Actresses: []models.Actress{{DMMID: actors[0].DMMID, JapaneseName: "同名"}, {DMMID: actors[1].DMMID, JapaneseName: "同名"}}}
	sources := []*models.ScraperResult{{Source: "dmm", Actresses: []models.ActressInfo{{DMMID: actors[0].DMMID, JapaneseName: "同名"}, {DMMID: actors[1].DMMID, JapaneseName: "同名"}}}}
	result, err := postProcessScraped(t.Context(), movie, sources, nil, &Config{ActressDBEnabled: true, CollisionPolicy: "auto_keep"}, nil, database.NewActressRepository(f.db), ScrapeCmd{MovieID: "TEST-555"}, time.Now())
	require.NoError(t, err)
	require.Len(t, result.Movie.Credits, 2)
	for i, c := range result.Movie.Credits {
		require.Equal(t, actors[i].DMMID, c.Scraped.DMMID)
		require.Equal(t, "同名", c.Scraped.JapaneseName)
		require.Empty(t, c.Scraped.FirstName)
	}
	saved, err := database.NewMovieUpserter(database.NewMovieRepository(f.db)).Upsert(t.Context(), result.Movie)
	require.NoError(t, err)
	require.Len(t, saved.Credits, 2)
	require.ElementsMatch(t, []uint{actors[0].ID, actors[1].ID}, []uint{saved.Credits[0].ActressID, saved.Credits[1].ActressID})
	requireIdentityCollisionCount(t, f, 0)
}

func TestScrapeEnrichmentExistingCollisionRequiresExplicitReassignment(t *testing.T) {
	f := newEnrichmentFixture(t)
	f.cfg.Metadata.ActressDatabase.Enabled = true
	f.cfg.Metadata.Translation.Enabled = false
	actors := seedEnrichmentIdentities(t, f, sharedRomanizationActors()...)
	f.withScraper("javlibrary", &models.ScraperResult{Source: "javlibrary", Language: "ja", ID: "TEST-555", ContentID: "test555", Title: "Fixture", Actresses: []models.ActressInfo{{JapaneseName: actors[0].JapaneseName}}}, nil)
	s := f.build()
	s.cfg.CollisionPolicy = "auto_keep"
	old, err := s.Scrape(t.Context(), ScrapeCmd{MovieID: "TEST-555", ForceRefresh: true})
	require.NoError(t, err)
	// Reconstruct the old bug's persisted input, not an arbitrary SQL collision:
	// the display romanization used to be copied into credit identity evidence.
	old.Movie.Credits[0].Scraped.FirstName = actors[0].FirstName
	old.Movie.Credits[0].Scraped.LastName = actors[0].LastName
	upserter := database.NewMovieUpserter(database.NewMovieRepository(f.db))
	previous, err := upserter.Upsert(t.Context(), old.Movie)
	require.NoError(t, err)
	requireIdentityCollisionCount(t, f, 1)
	candidateID := previous.Credits[0].ActressID
	require.NotEqual(t, actors[0].ID, candidateID)
	require.False(t, previous.Credits[0].Actress.Verified)
	var oldCollision models.CreditCollision
	require.NoError(t, f.db.Where("field = ? AND status = ?", models.CreditFieldIdentityLink, models.CollisionStatusOpen).First(&oldCollision).Error)
	// Existing open collisions deliberately pin their old credit. A fresh
	// correct scrape must not silently waive that outstanding decision.
	refreshed, err := s.Scrape(t.Context(), ScrapeCmd{MovieID: "TEST-555", ForceRefresh: true})
	require.NoError(t, err)
	blocked, err := upserter.Upsert(t.Context(), refreshed.Movie)
	require.NoError(t, err)
	require.Len(t, blocked.Credits, 2)
	require.ElementsMatch(t, []uint{candidateID, actors[0].ID}, []uint{blocked.Credits[0].ActressID, blocked.Credits[1].ActressID})
	var collision models.CreditCollision
	require.NoError(t, f.db.Where("field = ? AND status = ?", models.CreditFieldIdentityLink, models.CollisionStatusOpen).First(&collision).Error)
	require.Equal(t, oldCollision.ID, collision.ID, "refresh must preserve the outstanding identity decision")
	counts, err := database.NewCreditCollisionRepository(f.db).CountOpenByMovieBatch(t.Context(), []string{blocked.ContentID})
	require.NoError(t, err)
	require.EqualValues(t, 1, counts[blocked.ContentID])
	// Use the existing public resolution service, selecting the verified
	// native-name/DMM owner explicitly; this is not an automatic identity merge.
	_, err = database.NewCollisionService(f.db).Resolve(t.Context(), collision.ID, models.CollisionResolutionReassign, actors[0].ID)
	require.NoError(t, err)
	require.NoError(t, f.db.First(&collision, collision.ID).Error)
	require.Equal(t, models.CollisionStatusResolved, collision.Status)
	require.Equal(t, models.CollisionResolutionReassign, collision.Resolution)
	for refresh := 0; refresh < 2; refresh++ {
		result, err := s.Scrape(t.Context(), ScrapeCmd{MovieID: "TEST-555", ForceRefresh: true})
		require.NoError(t, err)
		saved, err := upserter.Upsert(t.Context(), result.Movie)
		require.NoError(t, err)
		require.Len(t, saved.Credits, 1)
		require.Equal(t, actors[0].ID, saved.Credits[0].ActressID)
		counts, err := database.NewCreditCollisionRepository(f.db).CountOpenByMovieBatch(t.Context(), []string{saved.ContentID})
		require.NoError(t, err)
		require.Zero(t, counts[saved.ContentID])
		var candidate models.Actress
		require.NoError(t, f.db.First(&candidate, candidateID).Error, "repairing the credit must not silently delete or merge an identity")
		require.False(t, candidate.Verified)
		var candidates int64
		require.NoError(t, f.db.Model(&models.Actress{}).Where("verified = ?", false).Count(&candidates).Error)
		require.EqualValues(t, 1, candidates, "refresh must not create another candidate")
	}
	var verified []models.Actress
	require.NoError(t, f.db.Where("verified = ?", true).Order("id").Find(&verified).Error)
	require.Equal(t, actors, verified, "explicit credit reassignment must not merge or rewrite catalog identities")

}
