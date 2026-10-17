package workflow

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const refreshContentID = "refresh00101"

// Only the leaf website is synthetic. Cache selection, aggregation, workflow,
// migrations, credit reconciliation and SQLite transactions are production code.
type durableRefreshSource struct {
	result *models.ScraperResult
	before func(context.Context) error
	calls  int
}

func (s *durableRefreshSource) Name() string    { return "refresh-local" }
func (s *durableRefreshSource) IsEnabled() bool { return true }
func (s *durableRefreshSource) Config() *models.ScraperSettings {
	return &models.ScraperSettings{Enabled: true}
}
func (s *durableRefreshSource) Close() error                                   { return nil }
func (s *durableRefreshSource) GetURL(context.Context, string) (string, error) { return "", nil }
func (s *durableRefreshSource) Search(ctx context.Context, _ string) (*models.ScraperResult, error) {
	s.calls++
	if s.before != nil {
		if err := s.before(ctx); err != nil {
			return nil, err
		}
	}
	return s.result, nil
}

func refreshSourceResult() *models.ScraperResult {
	return &models.ScraperResult{ID: "REFRESH-101", ContentID: refreshContentID, Title: "Original title", Source: "refresh-local", Actresses: []models.ActressInfo{
		{DMMID: 80101, FirstName: "First", LastName: "Reported"},
		{DMMID: 80102, FirstName: "Second", LastName: "Actor"},
		{DMMID: 80103, FirstName: "Third", LastName: "Actor"},
	}}
}

func durableRefreshWorkflow(db *database.DB, source *durableRefreshSource) *Workflow {
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(source)
	appcfg := config.DefaultConfig(nil, nil)
	agg := aggregator.New(&aggregator.Config{Metadata: aggregator.MetadataConfigFromApp(&appcfg.Metadata), ScrapersPriority: []string{source.Name()}}, nil, nil, nil)
	repo := database.NewMovieRepository(db)
	engine := scrape.New(registry, agg, database.NewActressRepository(db), repo, nil, &scrape.Config{ScrapersPriority: []string{source.Name()}, ScrapeActress: true, ActressDBEnabled: true}, nil, nil)
	return &Workflow{scrape: newScrapeOrchestrator(engine, repo, "", nil, nfo.NFONameConfig{}, nil)}
}

type durableRefreshSnapshot struct {
	movie         *models.Movie
	collisions    []models.CreditCollision
	reassignments []models.MovieCreditReassignment
}

type durableRefreshFixture struct {
	db        *database.DB
	cfg       *database.Config
	actors    []models.Actress
	creditIDs map[uint]uint
	before    durableRefreshSnapshot
}

func newDurableRefreshFixture(t *testing.T) *durableRefreshFixture {
	t.Helper()
	ctx := context.Background()
	f := &durableRefreshFixture{cfg: &database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "refresh.db"), LogLevel: "silent"}}
	f.reopen(t)
	t.Cleanup(func() { require.NoError(t, f.db.Close()) })
	f.actors = []models.Actress{
		{DMMID: 80101, FirstName: "First", LastName: "Canonical", Verified: true, Origin: "user"},
		{DMMID: 80102, FirstName: "Second", LastName: "Actor", Verified: true, Origin: "user"},
		{DMMID: 80103, FirstName: "Third", LastName: "Actor", Verified: true, Origin: "user"},
		{DMMID: 80104, FirstName: "Correct", LastName: "Target", Verified: true, Origin: "user"},
	}
	for i := range f.actors {
		require.NoError(t, database.NewActressRepository(f.db).Create(ctx, &f.actors[i]))
	}
	res, meta, err := durableRefreshWorkflow(f.db, &durableRefreshSource{result: refreshSourceResult()}).Scrape(ctx, scrape.ScrapeCmd{MovieID: "REFRESH-101"})
	require.NoError(t, err)
	require.Equal(t, scrape.StatusCompleted, res.Status)
	require.True(t, meta.Persisted)
	credits := database.NewMovieCreditRepository(f.db)
	f.creditIDs = make(map[uint]uint)
	for _, credit := range res.Movie.Credits {
		f.creditIDs[credit.ActressID] = credit.ID
	}
	require.NoError(t, credits.UpdateOverride(ctx, f.creditIDs[f.actors[0].ID], "Movie Alias", true))
	require.NoError(t, credits.UpdateOrderPinned(ctx, f.creditIDs[f.actors[0].ID], 9, true))
	require.NoError(t, credits.SetDisplayForceCanonical(ctx, f.creditIDs[f.actors[0].ID], true))
	require.NoError(t, credits.UpdateSuppressed(ctx, f.creditIDs[f.actors[1].ID], true))
	third, err := credits.FindByCreditID(ctx, f.creditIDs[f.actors[2].ID])
	require.NoError(t, err)
	require.NoError(t, credits.ReassignCredit(ctx, third, f.actors[3].ID))
	f.creditIDs[f.actors[3].ID] = third.ID
	delete(f.creditIDs, f.actors[2].ID)
	svc := database.NewCollisionService(f.db)
	open, err := svc.Collisions.ListOpenByMovie(ctx, refreshContentID)
	require.NoError(t, err)
	require.Len(t, open, 1)
	_, err = svc.Resolve(ctx, open[0].ID, models.CollisionResolutionKeepIdentity, 0)
	require.NoError(t, err)
	f.reopen(t)
	f.before = f.snapshot(t)
	return f
}

func (f *durableRefreshFixture) reopen(t *testing.T) {
	t.Helper()
	if f.db != nil {
		require.NoError(t, f.db.Close())
	}
	var err error
	f.db, err = database.New(f.cfg)
	require.NoError(t, err)
	require.NoError(t, f.db.RunMigrationsOnStartup(context.Background()))
}

func (f *durableRefreshFixture) snapshot(t *testing.T) durableRefreshSnapshot {
	t.Helper()
	var snapshot durableRefreshSnapshot
	var err error
	snapshot.movie, err = database.NewMovieRepository(f.db).FindByContentID(context.Background(), refreshContentID)
	require.NoError(t, err)
	require.NoError(t, f.db.Where("movie_content_id = ?", refreshContentID).Order("id").Find(&snapshot.collisions).Error)
	require.NoError(t, f.db.Where("movie_content_id = ?", refreshContentID).Order("id").Find(&snapshot.reassignments).Error)
	return snapshot
}

func (f *durableRefreshFixture) assertChoices(t *testing.T, after durableRefreshSnapshot, alias string) {
	t.Helper()
	require.Len(t, after.movie.Credits, 3)
	byActor := make(map[uint]models.MovieCredit)
	for _, credit := range after.movie.Credits {
		byActor[credit.ActressID] = credit
		assert.Equal(t, f.creditIDs[credit.ActressID], credit.ID, "durable credit identity")
	}
	first := byActor[f.actors[0].ID]
	assert.Equal(t, alias, first.OverrideName)
	assert.True(t, first.UserOverride)
	assert.True(t, first.OrderPinned)
	assert.Equal(t, 9, first.OrderIndex)
	assert.True(t, first.DisplayForceCanonical)
	assert.False(t, first.Suppressed)
	assert.False(t, byActor[f.actors[3].ID].Suppressed)
	assert.True(t, byActor[f.actors[1].ID].Suppressed)
	assert.Contains(t, byActor, f.actors[3].ID)
	assert.NotContains(t, byActor, f.actors[2].ID)
	assert.Equal(t, f.before.reassignments, after.reassignments)
	require.NotEmpty(t, after.collisions)
	original := after.collisions[0]
	assert.Equal(t, f.before.collisions[0].ID, original.ID)
	assert.Equal(t, f.before.collisions[0].CreditID, original.CreditID)
	assert.Equal(t, "Reported First", original.ReportedValue)
	assert.Equal(t, "Canonical First", original.CanonicalValue)
	assert.Equal(t, models.CollisionStatusResolved, original.Status)
	assert.Equal(t, models.CollisionResolutionKeepIdentity, original.Resolution)
	// Canonical catalog identities remain independent of movie-only decisions.
	var actors []models.Actress
	require.NoError(t, f.db.Order("id").Find(&actors).Error)
	require.Len(t, actors, len(f.actors))
	for i := range actors {
		assert.Equal(t, f.actors[i].FullName(), actors[i].FullName())
		assert.Equal(t, f.actors[i].DMMID, actors[i].DMMID)
		assert.True(t, actors[i].Verified)
	}
	var foreignKeyErrors []map[string]any
	require.NoError(t, f.db.Raw("PRAGMA foreign_key_check").Scan(&foreignKeyErrors).Error)
	assert.Empty(t, foreignKeyErrors)
}

func TestForceRefreshPreservesDurableChoices(t *testing.T) {
	for _, mode := range []string{"cache_control", "selected_fresh_control", "force_success", "force_content_key", "force_normalized_query", "force_changed_display_id", "force_missing_content_id", "force_new_evidence", "force_edit_during_scrape", "force_deferred"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			f := newDurableRefreshFixture(t)
			source := &durableRefreshSource{result: refreshSourceResult()}
			source.result.Title = "Fresh title"
			source.result.Description = "Fresh description"
			source.result.Actresses[0].ThumbURL = "https://fixture.invalid/fresh-thumb.jpg"
			cmd := scrape.ScrapeCmd{MovieID: "REFRESH-101", ForceRefresh: true}
			alias := "Movie Alias"
			switch mode {
			case "cache_control":
				cmd.ForceRefresh = false
			case "selected_fresh_control":
				cmd.ForceRefresh = false
				cmd.SelectedScrapers = []string{source.Name()}
			case "force_content_key":
				cmd.MovieID = refreshContentID
			case "force_normalized_query":
				cmd.MovieID = ""
				cmd.RawInput = "  refresh-101  "
			case "force_changed_display_id":
				source.result.ID = "CORRECTED-101"
			case "force_missing_content_id":
				source.result.ContentID = ""
			case "force_new_evidence":
				source.result.Actresses[0].LastName = "New Report"
			case "force_edit_during_scrape":
				alias = "Edited while scraping"
				source.before = func(ctx context.Context) error {
					return database.NewMovieCreditRepository(f.db).UpdateOverride(ctx, f.creditIDs[f.actors[0].ID], alias, true)
				}
			case "force_deferred":
				cmd.SkipPersist = true
			}
			res, meta, err := durableRefreshWorkflow(f.db, source).Scrape(ctx, cmd)
			require.NoError(t, err)
			require.Equal(t, scrape.StatusCompleted, res.Status)
			if cmd.SkipPersist {
				assert.False(t, meta.Persisted)
				assert.Equal(t, f.before, f.snapshot(t), "deferred query must not mutate the database")
				// The batch persistence path clones then uses this same transactional entry point.
				res.Movie, err = database.UpsertWithDisplayTitle(ctx, database.NewMovieRepository(f.db), res.Movie.Clone(), nil, nil, meta.RenderDisplayTitle)
				require.NoError(t, err)
			} else {
				assert.True(t, meta.Persisted)
			}
			f.reopen(t)
			after := f.snapshot(t)
			f.assertChoices(t, after, alias)
			assert.Equal(t, refreshContentID, res.Movie.ContentID)
			if mode == "cache_control" {
				assert.True(t, res.Cached)
				assert.Zero(t, source.calls)
				assert.Equal(t, "Original title", after.movie.Title)
				assert.Equal(t, f.before.collisions, after.collisions)
			} else {
				assert.False(t, res.Cached)
				assert.Equal(t, 1, source.calls, "force must still query the real scrape pipeline")
				assert.Equal(t, "Fresh title", after.movie.Title)
				assert.Equal(t, "Fresh description", after.movie.Description)
				assert.Greater(t, after.movie.RenderGeneration, f.before.movie.RenderGeneration)
				assert.True(t, after.movie.RenderDirty)
				if mode == "force_changed_display_id" {
					assert.Equal(t, "CORRECTED-101", after.movie.ID)
				}
				var first models.MovieCredit
				for _, credit := range after.movie.Credits {
					if credit.ActressID == f.actors[0].ID {
						first = credit
					}
				}
				assert.Equal(t, "https://fixture.invalid/fresh-thumb.jpg", first.ReportedThumbURL)
				if mode == "force_new_evidence" {
					assert.Equal(t, "New Report First", first.CreditedName)
					assert.Equal(t, f.before.collisions[0], after.collisions[0], "old decision remains historical evidence")
					var newEvidence []models.CreditCollision
					for _, collision := range after.collisions {
						if collision.ReportedValue == "New Report First" {
							newEvidence = append(newEvidence, collision)
						}
					}
					require.Len(t, newEvidence, 1)
					assert.Equal(t, models.CollisionStatusOpen, newEvidence[0].Status)
					assert.Empty(t, newEvidence[0].Resolution)
				} else {
					assert.Equal(t, 2, after.collisions[0].Occurrences)
				}
				// The reassigned target can legitimately have NEW conflicting evidence.
				var targetEvidence []models.CreditCollision
				for _, collision := range after.collisions {
					if collision.ReportedValue == "Actor Third" && collision.CanonicalValue == "Target Correct" {
						targetEvidence = append(targetEvidence, collision)
					}
				}
				require.Len(t, targetEvidence, 1)
				assert.Equal(t, models.CollisionStatusOpen, targetEvidence[0].Status)
			}
		})
	}
}

func TestForceRefreshFailureLeavesDurableStateUntouched(t *testing.T) {
	for _, mode := range []string{"source_failure", "source_cancel", "cancel_before_persist", "transaction_rollback", "deferred_cancel", "deferred_rollback"} {
		t.Run(mode, func(t *testing.T) {
			f := newDurableRefreshFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			source := &durableRefreshSource{result: refreshSourceResult()}
			source.result.Title = "Fresh title"
			source.result.Actresses[0].LastName = "New Report"
			cmd := scrape.ScrapeCmd{MovieID: "REFRESH-101", ForceRefresh: true}
			switch mode {
			case "source_failure":
				source.before = func(context.Context) error { return errors.New("synthetic source failure") }
			case "source_cancel":
				source.before = func(ctx context.Context) error { cancel(); return ctx.Err() }
			case "cancel_before_persist":
				source.before = func(context.Context) error { cancel(); return nil }
			case "deferred_cancel", "deferred_rollback":
				cmd.SkipPersist = true
			}
			if mode == "transaction_rollback" || mode == "deferred_rollback" {
				// Movie Save occurs after credit and collision changes in the same transaction.
				require.NoError(t, f.db.Exec(`CREATE TRIGGER reject_refresh BEFORE UPDATE OF title ON movies WHEN NEW.title = 'Fresh title' BEGIN SELECT RAISE(ABORT, 'injected refresh rollback'); END`).Error)
			}
			res, meta, err := durableRefreshWorkflow(f.db, source).Scrape(ctx, cmd)
			if mode == "source_failure" || mode == "source_cancel" {
				require.NoError(t, err)
				require.Equal(t, scrape.StatusFailed, res.Status)
				assert.False(t, meta.Persisted)
			} else if cmd.SkipPersist {
				require.NoError(t, err)
				assert.False(t, meta.Persisted)
				assert.Equal(t, f.before, f.snapshot(t))
				if mode == "deferred_cancel" {
					cancel()
				}
				_, err = database.UpsertWithDisplayTitle(ctx, database.NewMovieRepository(f.db), res.Movie.Clone(), nil, nil, meta.RenderDisplayTitle)
				require.Error(t, err)
			} else {
				require.Error(t, err)
				assert.False(t, meta.Persisted)
			}
			if mode == "transaction_rollback" || mode == "deferred_rollback" {
				assert.ErrorContains(t, err, "injected refresh rollback")
			}
			if mode == "cancel_before_persist" || mode == "deferred_cancel" {
				assert.ErrorIs(t, err, context.Canceled)
			}
			assert.Equal(t, 1, source.calls)
			f.reopen(t)
			after := f.snapshot(t)
			assert.Equal(t, f.before, after, "failed refresh must leave the full persisted movie and decision history untouched")
			f.assertChoices(t, after, "Movie Alias")
		})
	}
}

func TestForceRefreshRejectsConflictingContentID(t *testing.T) {
	for _, tc := range []struct {
		name               string
		deferred, existing bool
	}{
		{name: "inline"}, {name: "deferred", deferred: true}, {name: "existing_target", existing: true}, {name: "existing_target_deferred", deferred: true, existing: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			f := newDurableRefreshFixture(t)
			var otherBefore *models.Movie
			if tc.existing {
				var err error
				otherBefore, err = database.NewMovieRepository(f.db).Upsert(ctx, &models.Movie{ContentID: "different00101", ID: "OTHER-101", Title: "Other original title"})
				require.NoError(t, err)
			}
			source := &durableRefreshSource{result: refreshSourceResult()}
			source.result.ContentID = "different00101"
			source.result.Title = "Different content"
			w := durableRefreshWorkflow(f.db, source)
			res, meta, err := w.Scrape(ctx, scrape.ScrapeCmd{MovieID: "REFRESH-101", ForceRefresh: true, SkipPersist: tc.deferred})
			if tc.deferred {
				require.NoError(t, err)
				assert.True(t, res.Movie.RequireExactContentID)
				assert.Equal(t, f.before, f.snapshot(t))
				pending := res.Movie.Clone()
				_, err = database.UpsertWithDisplayTitle(ctx, database.NewMovieRepository(f.db), pending, nil, nil, meta.RenderDisplayTitle)
				assert.False(t, pending.RequireExactContentID, "constraint is consumed even on failure")
			} else {
				assert.False(t, res.Movie.RequireExactContentID)
			}
			require.ErrorIs(t, err, database.ErrMovieContentIDConflict)
			assert.ErrorContains(t, err, refreshContentID)
			assert.ErrorContains(t, err, "different00101")
			assert.False(t, meta.Persisted)
			f.reopen(t)
			assert.Equal(t, f.before, f.snapshot(t), "conflicting content must not mutate the old movie or its choices")
			var count int64
			require.NoError(t, f.db.Model(&models.Movie{}).Where("id = ?", "REFRESH-101").Count(&count).Error)
			assert.EqualValues(t, 1, count, "no ambiguous display-ID duplicate")
			otherAfter, err := database.NewMovieRepository(f.db).FindByContentID(ctx, "different00101")
			if tc.existing {
				require.NoError(t, err)
				assert.Equal(t, otherBefore, otherAfter)
			} else {
				assert.True(t, database.IsNotFound(err))
			}
			// A subsequent ordinary lookup and save remain bound to the original content.
			source.calls = 0
			cached, _, err := durableRefreshWorkflow(f.db, source).Scrape(ctx, scrape.ScrapeCmd{MovieID: "REFRESH-101"})
			require.NoError(t, err)
			assert.True(t, cached.Cached)
			assert.Zero(t, source.calls)
			assert.Equal(t, refreshContentID, cached.Movie.ContentID)
			assert.Equal(t, "Original title", cached.Movie.Title)
			assert.False(t, cached.Movie.RequireExactContentID)
			f.assertChoices(t, f.snapshot(t), "Movie Alias")
		})
	}
}

func TestForceRefreshNewMovieDoesNotInheritOtherContentChoices(t *testing.T) {
	ctx := context.Background()
	f := newDurableRefreshFixture(t)
	source := &durableRefreshSource{result: refreshSourceResult()}
	source.result.ID = "DISTINCT-101"
	source.result.ContentID = "distinct00101"
	res, _, err := durableRefreshWorkflow(f.db, source).Scrape(ctx, scrape.ScrapeCmd{MovieID: "DISTINCT-101", ForceRefresh: true})
	require.NoError(t, err)
	assert.Equal(t, "distinct00101", res.Movie.ContentID)
	assert.False(t, res.Movie.RequireExactContentID)
	f.reopen(t)
	assert.Equal(t, f.before, f.snapshot(t))
	fresh, err := database.NewMovieRepository(f.db).FindByID(ctx, "DISTINCT-101")
	require.NoError(t, err)
	require.Len(t, fresh.Credits, 3)
	seen := make(map[uint]bool)
	for _, credit := range fresh.Credits {
		seen[credit.ActressID] = true
		assert.Empty(t, credit.OverrideName)
		assert.False(t, credit.UserOverride)
		assert.False(t, credit.Suppressed)
		assert.False(t, credit.OrderPinned)
		assert.False(t, credit.DisplayForceCanonical)
		assert.NotEqual(t, f.creditIDs[credit.ActressID], credit.ID)
	}
	assert.True(t, seen[f.actors[2].ID], "other content's reassignment must not cross the identity boundary")
	assert.False(t, seen[f.actors[3].ID])
	var collisions []models.CreditCollision
	require.NoError(t, f.db.Where("movie_content_id = ?", fresh.ContentID).Find(&collisions).Error)
	require.Len(t, collisions, 1)
	assert.NotEqual(t, f.before.collisions[0].ID, collisions[0].ID)
	assert.Equal(t, models.CollisionStatusOpen, collisions[0].Status)
	assert.Empty(t, collisions[0].Resolution)
	var mappings int64
	require.NoError(t, f.db.Model(&models.MovieCreditReassignment{}).Where("movie_content_id = ?", fresh.ContentID).Count(&mappings).Error)
	assert.Zero(t, mappings)
}

func TestFreshRefreshWithoutReportedCastPreservesChoices(t *testing.T) {
	for _, mode := range []string{"force", "selected_fresh", "force_deferred"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			f := newDurableRefreshFixture(t)
			source := &durableRefreshSource{result: refreshSourceResult()}
			source.result.Actresses = nil
			source.result.Title = "Fresh title without cast"
			cmd := scrape.ScrapeCmd{MovieID: "REFRESH-101", ForceRefresh: mode != "selected_fresh", SkipPersist: mode == "force_deferred"}
			if mode == "selected_fresh" {
				cmd.SelectedScrapers = []string{source.Name()}
			}
			res, meta, err := durableRefreshWorkflow(f.db, source).Scrape(ctx, cmd)
			require.NoError(t, err)
			if cmd.SkipPersist {
				assert.Equal(t, f.before, f.snapshot(t))
				res.Movie, err = database.UpsertWithDisplayTitle(ctx, database.NewMovieRepository(f.db), res.Movie.Clone(), nil, nil, meta.RenderDisplayTitle)
				require.NoError(t, err)
			}
			assert.Equal(t, 1, source.calls)
			assert.False(t, res.Cached)
			assert.Equal(t, "Fresh title without cast", res.Movie.Title)
			f.reopen(t)
			after := f.snapshot(t)
			f.assertChoices(t, after, "Movie Alias")
			assert.Equal(t, f.before.movie.Credits, after.movie.Credits)
			assert.Equal(t, f.before.movie.Actresses, after.movie.Actresses)
			assert.Equal(t, f.before.collisions, after.collisions)
		})
	}
}

func TestExplicitUserEmptyCastStillRemovesVisibleCredits(t *testing.T) {
	f := newDurableRefreshFixture(t)
	saved, err := database.NewMovieRepository(f.db).Upsert(context.Background(), &models.Movie{ID: "REFRESH-101", ContentID: refreshContentID, Title: "Explicit user edit", Actresses: []models.Actress{}})
	require.NoError(t, err)
	require.Len(t, saved.Credits, 3)
	assert.Empty(t, saved.Actresses)
	for _, credit := range saved.Credits {
		assert.True(t, credit.Suppressed)
	}
	f.reopen(t)
	after := f.snapshot(t)
	assert.Equal(t, "Explicit user edit", after.movie.Title)
	assert.Equal(t, f.before.reassignments, after.reassignments)
	for _, credit := range after.movie.Credits {
		assert.True(t, credit.Suppressed)
	}
}

// Source-to-target reassignment survives membership omission without pinning an
// absent scrape-owned credit forever. When the source returns, its choice reapplies.
func TestFreshRefreshReappliesDurableReassignmentAfterOmission(t *testing.T) {
	for _, mode := range []string{"force_partial", "selected_partial", "force_explicit_empty", "selected_explicit_empty"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			f := newDurableRefreshFixture(t)
			unowned := models.Actress{DMMID: 80105, FirstName: "Obsolete", LastName: "Actor", Verified: true, Origin: "user"}
			require.NoError(t, database.NewActressRepository(f.db).Create(ctx, &unowned))
			extra := models.MovieCredit{MovieContentID: refreshContentID, ActressID: unowned.ID, CreditedName: unowned.FullName(), Origin: "scrape", Source: "refresh-local"}
			require.NoError(t, f.db.Transaction(func(tx *gorm.DB) error { return database.NewMovieCreditRepository(f.db).UpsertTx(tx, &extra) }))
			source := &durableRefreshSource{result: refreshSourceResult()}
			source.result.Title = "Fresh reduced cast"
			source.result.Actresses = source.result.Actresses[:2]
			if mode == "force_explicit_empty" || mode == "selected_explicit_empty" {
				source.result.Actresses = []models.ActressInfo{}
			}
			cmd := scrape.ScrapeCmd{MovieID: "REFRESH-101", ForceRefresh: mode == "force_partial" || mode == "force_explicit_empty"}
			if !cmd.ForceRefresh {
				cmd.SelectedScrapers = []string{source.Name()}
			}
			res, _, err := durableRefreshWorkflow(f.db, source).Scrape(ctx, cmd)
			require.NoError(t, err)
			assert.Equal(t, "Fresh reduced cast", res.Movie.Title)
			f.reopen(t)
			after := f.snapshot(t)
			assert.Equal(t, f.before.reassignments, after.reassignments)
			require.Len(t, after.movie.Credits, 2)
			for _, credit := range after.movie.Credits {
				assert.NotEqual(t, unowned.ID, credit.ActressID, "obsolete unowned credit should be removed")
				assert.NotEqual(t, f.actors[3].ID, credit.ActressID, "reassignment is not a cast-membership pin")
			}
			source.result = refreshSourceResult()
			source.result.Title = "Cast returned"
			res, _, err = durableRefreshWorkflow(f.db, source).Scrape(ctx, cmd)
			require.NoError(t, err)
			f.reopen(t)
			restored := f.snapshot(t)
			assert.Equal(t, f.before.reassignments, restored.reassignments)
			require.Len(t, restored.movie.Credits, 3)
			seen := make(map[uint]models.MovieCredit)
			for _, credit := range restored.movie.Credits {
				seen[credit.ActressID] = credit
			}
			assert.Contains(t, seen, f.actors[3].ID, "returning source must reapply the durable target")
			assert.NotContains(t, seen, f.actors[2].ID, "original source must not replace the user's target")
			assert.Equal(t, "Movie Alias", seen[f.actors[0].ID].OverrideName)
			assert.Equal(t, f.creditIDs[f.actors[0].ID], seen[f.actors[0].ID].ID)
			assert.True(t, seen[f.actors[1].ID].Suppressed)
			assert.Equal(t, f.before.collisions[0].ID, restored.collisions[0].ID)
			assert.Equal(t, models.CollisionStatusResolved, restored.collisions[0].Status)
			assert.Equal(t, models.CollisionResolutionKeepIdentity, restored.collisions[0].Resolution)
		})
	}
}
