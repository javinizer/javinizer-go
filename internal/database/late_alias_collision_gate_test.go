package database

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func lateAliasMovieFixture(t *testing.T, db *DB, contentID string, candidateNames ...string) (*models.Movie, []models.Actress, models.Actress) {
	t.Helper()
	verified := models.Actress{JapaneseName: "Trusted Canonical", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&verified).Error)
	candidates := make([]models.Actress, len(candidateNames))
	credits := make([]models.MovieCredit, 0, len(candidateNames)+1)
	for i, name := range candidateNames {
		candidates[i] = models.Actress{DMMID: 95000 + i, JapaneseName: name, Origin: ActressOriginScrape}
		require.NoError(t, db.Create(&candidates[i]).Error)
		credits = append(credits, models.MovieCredit{
			CreditedName: name,
			Source:       "dmm",
			Scraped:      models.Actress{DMMID: candidates[i].DMMID, JapaneseName: name},
		})
	}
	credits = append(credits, models.MovieCredit{
		CreditedName: candidateNames[0],
		Source:       "trusted",
		Scraped:      models.Actress{JapaneseName: verified.JapaneseName},
	})
	movie := &models.Movie{
		ContentID:               contentID,
		ID:                      contentID,
		Credits:                 credits,
		CreditPolicy:            string(CollisionPolicyAutoAlias),
		TrustedCollisionSources: []string{"trusted"},
	}
	return movie, candidates, verified
}

func TestMovieUpsertFinalReconcileRecordsLateAliasIdentityCollision(t *testing.T) {
	db := newCreditTestDB(t)
	movie, candidates, _ := lateAliasMovieFixture(t, db, "late-alias", "Late Stage Name")

	saved, err := NewMovieRepository(db).Upsert(context.Background(), movie)
	require.NoError(t, err)
	require.Len(t, saved.Actresses, 1, "only the verified identity remains in compatibility cast")

	var candidate models.Actress
	require.NoError(t, db.First(&candidate, candidates[0].ID).Error)
	require.True(t, candidate.AmbiguityQuarantined)
	open, err := NewCreditCollisionRepository(db).ListOpenByMovie(context.Background(), saved.ContentID)
	require.NoError(t, err)
	require.Len(t, open, 1)
	require.Equal(t, models.CreditFieldIdentityLink, open[0].Field)
	require.Equal(t, "Late Stage Name", open[0].ReportedValue)
	require.Equal(t, candidate.FullName(), open[0].CanonicalValue)
	require.Equal(t, saved.Credits[0].ID, open[0].CreditID)
	require.ErrorIs(t, NewMovieRepository(db).WithApplyArtifactPublicationFence(context.Background(), saved.ContentID, saved.RenderGeneration, func(*models.Movie) error { return nil }), ErrApplyArtifactPublicationBlocked)

	_, err = NewMovieRepository(db).Upsert(context.Background(), movie)
	require.NoError(t, err)
	open, err = NewCreditCollisionRepository(db).ListOpenByMovie(context.Background(), saved.ContentID)
	require.NoError(t, err)
	require.Len(t, open, 1, "retry must reuse the open collision")
}

func TestArtifactPublicationFenceRejectsQuarantinedCreditedCandidateWithoutCollision(t *testing.T) {
	for _, suppressed := range []bool{false, true} {
		t.Run(map[bool]string{false: "active", true: "suppressed"}[suppressed], func(t *testing.T) {
			db := newCreditTestDB(t)
			movie := models.Movie{ContentID: "legacy-quarantine", ID: "legacy-quarantine", RenderGeneration: 4}
			candidate := models.Actress{DMMID: 96001, JapaneseName: "Legacy Candidate", Origin: ActressOriginScrape, AmbiguityQuarantined: true}
			require.NoError(t, db.Create(&movie).Error)
			require.NoError(t, db.Create(&candidate).Error)
			require.NoError(t, db.Create(&models.MovieCredit{MovieContentID: movie.ContentID, ActressID: candidate.ID, Suppressed: suppressed}).Error)
			called := false
			err := NewMovieRepository(db).WithApplyArtifactPublicationFence(context.Background(), movie.ContentID, movie.RenderGeneration, func(*models.Movie) error { called = true; return nil })
			if suppressed {
				require.NoError(t, err)
				require.True(t, called)
			} else {
				require.ErrorIs(t, err, ErrApplyArtifactPublicationBlocked)
				require.ErrorContains(t, err, "quarantined candidate credit")
				require.False(t, called)
			}
		})
	}
}

func TestMovieUpsertFinalReconcileHandlesMultipleCandidatesAndExplicitResolution(t *testing.T) {
	db := newCreditTestDB(t)
	candidateNames := []string{"Late Stage One", "Late Stage Two"}
	candidates := make([]models.Actress, 2)
	verified := make([]models.Actress, 2)
	credits := make([]models.MovieCredit, 0, 4)
	for i := range candidates {
		candidates[i] = models.Actress{DMMID: 97001 + i, JapaneseName: candidateNames[i], Origin: ActressOriginScrape}
		verified[i] = models.Actress{JapaneseName: "Trusted Canonical " + candidateNames[i], Verified: true, Origin: ActressOriginUser}
		require.NoError(t, db.Create(&candidates[i]).Error)
		require.NoError(t, db.Create(&verified[i]).Error)
		credits = append(credits, models.MovieCredit{CreditedName: candidateNames[i], Source: "dmm", Scraped: models.Actress{DMMID: candidates[i].DMMID, JapaneseName: candidateNames[i]}})
	}
	for i := range verified {
		credits = append(credits, models.MovieCredit{CreditedName: candidateNames[i], Source: "trusted", Scraped: models.Actress{JapaneseName: verified[i].JapaneseName}})
	}
	movie := &models.Movie{ContentID: "late-alias-multiple", ID: "late-alias-multiple", Credits: credits, CreditPolicy: string(CollisionPolicyAutoAlias), TrustedCollisionSources: []string{"trusted"}}

	saved, err := NewMovieRepository(db).Upsert(context.Background(), movie)
	require.NoError(t, err)
	open, err := NewCreditCollisionRepository(db).ListOpenByMovie(context.Background(), saved.ContentID)
	require.NoError(t, err)
	require.Len(t, open, 2)
	firstIDs := []uint{open[0].ID, open[1].ID}
	for i := range open {
		require.Equal(t, models.CreditFieldIdentityLink, open[i].Field)
		require.Equal(t, candidateNames[i], open[i].ReportedValue)
	}

	_, err = NewMovieRepository(db).Upsert(context.Background(), movie)
	require.NoError(t, err)
	open, err = NewCreditCollisionRepository(db).ListOpenByMovie(context.Background(), saved.ContentID)
	require.NoError(t, err)
	require.Equal(t, firstIDs, []uint{open[0].ID, open[1].ID}, "existing open evidence must be reused")

	remaining, err := NewCollisionService(db).Resolve(context.Background(), open[0].ID, models.CollisionResolutionAdoptCanonical, 0)
	require.NoError(t, err)
	require.Equal(t, 1, remaining)
	var resolved models.Actress
	require.NoError(t, db.First(&resolved, candidates[0].ID).Error)
	require.True(t, resolved.Verified)
	require.False(t, resolved.AmbiguityQuarantined)
}

func TestMovieUpsertLateAliasRetryAfterRestartDoesNotDuplicateCollision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "late-alias.db")
	openDB := func() *DB {
		db, err := New(&Config{Type: "sqlite", DSN: path, LogLevel: "silent"})
		require.NoError(t, err)
		require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
		return db
	}
	db := openDB()
	movie, _, _ := lateAliasMovieFixture(t, db, "late-alias-restart", "Restart Stage Name")
	saved, err := NewMovieRepository(db).Upsert(context.Background(), movie)
	require.NoError(t, err)
	before, err := NewCreditCollisionRepository(db).ListOpenByMovie(context.Background(), saved.ContentID)
	require.NoError(t, err)
	require.Len(t, before, 1)
	require.NoError(t, db.Close())

	db = openDB()
	t.Cleanup(func() { _ = db.Close() })
	_, err = NewMovieRepository(db).Upsert(context.Background(), movie)
	require.NoError(t, err)
	after, err := NewCreditCollisionRepository(db).ListOpenByMovie(context.Background(), saved.ContentID)
	require.NoError(t, err)
	require.Len(t, after, 1)
	require.Equal(t, before[0].ID, after[0].ID)
}

func TestFinalCandidateCollisionReconcileEvidenceFallbacksAndReadFailures(t *testing.T) {
	for _, tc := range []struct {
		name, credited, japanese, want string
	}{
		{name: "credited name", credited: "Credited Fallback", want: "Credited Fallback"},
		{name: "credited Japanese name", japanese: "表示名", want: "表示名"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newCreditTestDB(t)
			movie := models.Movie{ContentID: "fallback-" + tc.name, ID: "fallback-" + tc.name}
			candidate := models.Actress{DMMID: 98001, Origin: ActressOriginScrape, AmbiguityQuarantined: true}
			require.NoError(t, db.Create(&movie).Error)
			require.NoError(t, db.Create(&candidate).Error)
			require.NoError(t, db.Create(&models.MovieCredit{MovieContentID: movie.ContentID, ActressID: candidate.ID, CreditedName: tc.credited, CreditedJapaneseName: tc.japanese}).Error)
			require.NoError(t, NewMovieRepository(db).upserter.reconcileFinalCandidateCollisionsTx(db.DB, movie.ContentID))
			open, err := NewCreditCollisionRepository(db).ListOpenByMovie(context.Background(), movie.ContentID)
			require.NoError(t, err)
			require.Len(t, open, 1)
			require.Equal(t, tc.want, open[0].ReportedValue)
		})
	}

	for _, table := range []string{"movie_credits", "actresses"} {
		t.Run(table+" read failure", func(t *testing.T) {
			db := newCreditTestDB(t)
			movie := models.Movie{ContentID: "read-failure-" + table, ID: "read-failure-" + table}
			candidate := models.Actress{DMMID: 98002, JapaneseName: "Read Failure", Origin: ActressOriginScrape, AmbiguityQuarantined: true}
			require.NoError(t, db.Create(&movie).Error)
			require.NoError(t, db.Create(&candidate).Error)
			require.NoError(t, db.Create(&models.MovieCredit{MovieContentID: movie.ContentID, ActressID: candidate.ID}).Error)
			injectDatabaseCallbackError(t, db, "query", table, 1)
			require.Error(t, NewMovieRepository(db).upserter.reconcileFinalCandidateCollisionsTx(db.DB, movie.ContentID))
		})
	}
}

func TestArtifactPublicationQuarantinedCreditProbeFailureIsClosed(t *testing.T) {
	db := newCreditTestDB(t)
	require.NoError(t, db.Migrator().DropTable(&models.Actress{}))
	err := rejectBlockedArtifactPublicationTx(context.Background(), db.DB, "missing-actress-table")
	require.ErrorContains(t, err, "quarantined candidate credits")
}

func TestArtifactPublicationQuarantinedCreditProbeHonorsCancellation(t *testing.T) {
	db := newCreditTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	const hook = "cancel_after_quarantined_credit_probe"
	require.NoError(t, db.Callback().Row().After("gorm:row").Register(hook, func(tx *gorm.DB) {
		if strings.Contains(tx.Statement.SQL.String(), "movie_credits") && tx.Error == nil {
			cancel()
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Row().Remove(hook) })
	require.ErrorIs(t, rejectBlockedArtifactPublicationTx(ctx, db.DB, "cancel-probe"), context.Canceled)
}

func TestMovieUpsertLateAliasCollisionFailureRollsBackMovieAliasAndCredits(t *testing.T) {
	db := newCreditTestDB(t)
	movie, candidates, _ := lateAliasMovieFixture(t, db, "late-alias-rollback", "Rollback Stage Name")
	require.NoError(t, db.Exec(`CREATE TRIGGER fail_late_identity_collision BEFORE INSERT ON credit_collisions WHEN NEW.field = 'identity_link' BEGIN SELECT RAISE(ABORT, 'injected identity collision failure'); END`).Error)

	_, err := NewMovieRepository(db).Upsert(context.Background(), movie)
	require.Error(t, err)
	for model, where := range map[any]string{
		&models.Movie{}:           "content_id = 'late-alias-rollback'",
		&models.MovieCredit{}:     "movie_content_id = 'late-alias-rollback'",
		&models.ActressAlias{}:    "alias_name = 'Rollback Stage Name'",
		&models.CreditCollision{}: "movie_content_id = 'late-alias-rollback'",
	} {
		var count int64
		require.NoError(t, db.Model(model).Where(where).Count(&count).Error)
		require.Zero(t, count)
	}
	var candidate models.Actress
	require.NoError(t, db.First(&candidate, candidates[0].ID).Error)
	require.False(t, candidate.AmbiguityQuarantined)
}
