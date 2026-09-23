package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func collisionFixture(t *testing.T) (*DB, *CollisionService, models.MovieCredit, models.CreditCollision) {
	t.Helper()
	db := newCreditTestDB(t)
	actress := models.Actress{FirstName: "Truth", LastName: "Original", Verified: true, Origin: "user"}
	require.NoError(t, db.Create(&actress).Error)
	movie := models.Movie{ContentID: "collision-movie", ID: "collision-movie", Title: "Collision"}
	require.NoError(t, db.Create(&movie).Error)
	credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: actress.ID, CreditedName: "Reported Person", ReportedThumbURL: "https://example.com/new.jpg", Origin: "scrape"}
	require.NoError(t, db.Create(&credit).Error)
	collision := models.CreditCollision{CreditID: credit.ID, MovieContentID: movie.ContentID, Field: models.CreditFieldCreditedName, ReportedValue: "Reported Person", CanonicalValue: "Original Truth", Status: models.CollisionStatusOpen, Occurrences: 1, SourcesSeen: "dmm"}
	require.NoError(t, db.Create(&collision).Error)
	return db, NewCollisionService(db), credit, collision
}

func TestCollisionServiceReassignSynchronizesLegacyActressAssociation(t *testing.T) {
	t.Run("direct", func(t *testing.T) {
		db, service, credit, collision := collisionFixture(t)
		sourceActressID := credit.ActressID
		target := models.Actress{FirstName: "Person", LastName: "Reported", Verified: true, Origin: ActressOriginUser}
		require.NoError(t, db.Create(&target).Error)
		movie := models.Movie{ContentID: credit.MovieContentID}
		require.NoError(t, db.Model(&movie).Association("Actresses").Replace([]models.Actress{{ID: credit.ActressID}}))

		_, err := service.Resolve(context.Background(), collision.ID, models.CollisionResolutionReassign, target.ID)
		require.NoError(t, err)

		var ids []uint
		require.NoError(t, db.Table("movie_actresses").Where("movie_content_id = ?", credit.MovieContentID).Pluck("actress_id", &ids).Error)
		require.ElementsMatch(t, []uint{target.ID}, ids)
		require.NoError(t, db.First(&credit, credit.ID).Error)
		require.Equal(t, target.ID, credit.ActressID)

		rescrape := creditMovie(credit.MovieContentID, []models.MovieCredit{{
			CreditedName: collision.ReportedValue,
			Scraped:      models.Actress{FirstName: "Truth", LastName: "Original"},
		}})
		_, err = db.Repositories().MovieRepo.UpsertWithTranslations(context.Background(), rescrape, nil, nil)
		require.NoError(t, err)
		credits, err := service.Credits.ListByMovie(context.Background(), credit.MovieContentID)
		require.NoError(t, err)
		require.Len(t, credits, 1)
		require.Equal(t, target.ID, credits[0].ActressID)
		var reassignment models.MovieCreditReassignment
		require.NoError(t, db.First(&reassignment, "movie_content_id = ? AND source_actress_id = ?", credit.MovieContentID, sourceActressID).Error)
		require.Equal(t, target.ID, reassignment.TargetActressID)
	})

	t.Run("merge", func(t *testing.T) {
		db, service, source, collision := collisionFixture(t)
		target := models.Actress{FirstName: "Target", Verified: true, Origin: ActressOriginUser}
		require.NoError(t, db.Create(&target).Error)
		targetCredit := models.MovieCredit{MovieContentID: source.MovieContentID, ActressID: target.ID, CreditedName: "Target"}
		require.NoError(t, db.Create(&targetCredit).Error)
		movie := models.Movie{ContentID: source.MovieContentID}
		require.NoError(t, db.Model(&movie).Association("Actresses").Replace([]models.Actress{{ID: source.ActressID}, {ID: target.ID}}))

		_, err := service.Resolve(context.Background(), collision.ID, models.CollisionResolutionReassign, target.ID)
		require.NoError(t, err)

		var ids []uint
		require.NoError(t, db.Table("movie_actresses").Where("movie_content_id = ?", source.MovieContentID).Pluck("actress_id", &ids).Error)
		require.ElementsMatch(t, []uint{target.ID}, ids)
		var credits []models.MovieCredit
		require.NoError(t, db.Where("movie_content_id = ?", source.MovieContentID).Find(&credits).Error)
		require.Len(t, credits, 1)
		require.Equal(t, target.ID, credits[0].ActressID)
	})
}

func TestCollisionServiceResolutions(t *testing.T) {
	cases := []struct{ name, resolution, field, reported, japanese string }{
		{"keep name", "keep_identity", "credited_name", "Reported Person", ""},
		{"keep link", "keep_identity", "identity_link", "Reported Person", ""},
		{"adopt western", "adopt_canonical", "credited_name", "Reported Person", ""},
		{"adopt single", "adopt_canonical", "credited_name", "Solo", ""},
		{"adopt japanese", "adopt_canonical", "credited_name", "日本名", ""},
		{"adopt thumb", "adopt_canonical", "reported_thumb_url", "https://example.com/thumb.jpg", ""},
		{"promote western", "adopt_canonical", "identity_link", "Reported Person", ""},
		{"promote japanese", "adopt_canonical", "identity_link", "Reported Person", "日本名"},
		{"alias", "adopt_alias", "credited_name", "Reported Person", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, service, credit, collision := collisionFixture(t)
			collision.Field, collision.ReportedValue = tc.field, tc.reported
			require.NoError(t, db.Save(&collision).Error)
			if tc.japanese != "" {
				require.NoError(t, db.Model(&credit).Update("credited_japanese_name", tc.japanese).Error)
			}
			sibling := models.Movie{ContentID: "sibling", ID: "sibling", Title: "Sibling"}
			require.NoError(t, db.Create(&sibling).Error)
			require.NoError(t, db.Create(&models.MovieCredit{MovieContentID: sibling.ContentID, ActressID: credit.ActressID}).Error)
			remaining, err := service.Resolve(context.Background(), collision.ID, tc.resolution, 0)
			require.NoError(t, err)
			require.Zero(t, remaining)
			require.NoError(t, db.First(&collision, collision.ID).Error)
			require.Equal(t, models.CollisionStatusResolved, collision.Status)
			require.Equal(t, tc.resolution, collision.Resolution)
			require.NoError(t, db.First(&credit, credit.ID).Error)
			var actress models.Actress
			require.NoError(t, db.First(&actress, credit.ActressID).Error)
			var movie models.Movie
			require.NoError(t, db.First(&movie, "content_id = ?", credit.MovieContentID).Error)
			require.True(t, movie.RenderDirty)
			require.EqualValues(t, 1, movie.RenderGeneration)
			require.NoError(t, db.First(&sibling, "content_id = ?", sibling.ContentID).Error)
			require.Equal(t, tc.resolution == "adopt_canonical", sibling.RenderDirty)
			switch tc.resolution {
			case "keep_identity":
				require.Equal(t, "Truth", actress.FirstName)
				require.Equal(t, tc.field == models.CreditFieldCreditedName, credit.DisplayForceCanonical)
			case "adopt_alias":
				require.True(t, credit.DisplayForceCanonical)
				var alias models.ActressAlias
				require.NoError(t, db.First(&alias, "alias_name = ?", tc.reported).Error)
				require.Equal(t, "Original Truth", alias.CanonicalName)
			case "adopt_canonical":
				switch tc.field {
				case "reported_thumb_url":
					require.Equal(t, tc.reported, actress.ThumbURL)
				case "identity_link":
					require.True(t, actress.Verified)
					require.Equal(t, "user", actress.Origin)
					require.Equal(t, credit.ReportedThumbURL, actress.ThumbURL)
					if tc.japanese != "" {
						require.Equal(t, tc.japanese, actress.JapaneseName)
					} else {
						require.Equal(t, "Person", actress.FirstName)
					}
				default:
					if tc.reported == "日本名" {
						require.Equal(t, tc.reported, actress.JapaneseName)
					} else if tc.reported == "Solo" {
						require.Equal(t, "Solo", actress.FirstName)
						require.Empty(t, actress.LastName)
					} else {
						require.Equal(t, "Person", actress.FirstName)
						require.Equal(t, "Reported", actress.LastName)
					}
				}
			}
			_, err = service.Resolve(context.Background(), collision.ID, tc.resolution, 0)
			require.ErrorIs(t, err, ErrCollisionNotOpen)
		})
	}
}

func TestCollisionServiceKeepIdentityRejectsUnverifiedCandidate(t *testing.T) {
	db := newCreditTestDB(t)
	repos := db.Repositories()
	for _, actress := range []models.Actress{
		{JapaneseName: "双子", FirstName: "A", Verified: true, Origin: ActressOriginUser},
		{JapaneseName: "双子", FirstName: "B", Verified: true, Origin: ActressOriginUser},
	} {
		require.NoError(t, repos.ActressRepo.Create(context.Background(), &actress))
	}
	movie := creditMovie("candidate-keep-identity", []models.MovieCredit{{
		CreditedName: "双子", Source: "dmm", Scraped: models.Actress{JapaneseName: "双子"},
	}})
	saved, err := repos.MovieRepo.UpsertWithTranslations(context.Background(), movie, nil, nil)
	require.NoError(t, err)
	require.Len(t, saved.Credits, 1)
	credit := saved.Credits[0]
	require.NotNil(t, credit.Actress)
	require.False(t, credit.Actress.Verified)

	collisions, err := repos.CreditCollisionRepo.ListOpenByMovie(context.Background(), movie.ContentID)
	require.NoError(t, err)
	require.Len(t, collisions, 1)
	require.Equal(t, models.CreditFieldIdentityLink, collisions[0].Field)

	_, err = NewCollisionService(db).Resolve(context.Background(), collisions[0].ID, models.CollisionResolutionKeepIdentity, 0)
	require.Error(t, err)
	var candidate models.Actress
	require.NoError(t, db.First(&candidate, credit.ActressID).Error)
	require.False(t, candidate.Verified)
	require.Equal(t, ActressOriginScrape, candidate.Origin)
	var savedCollision models.CreditCollision
	require.NoError(t, db.First(&savedCollision, collisions[0].ID).Error)
	require.Equal(t, models.CollisionStatusOpen, savedCollision.Status)
	require.Empty(t, savedCollision.Resolution)
	var actressIDs []uint
	require.NoError(t, db.Table("movie_actresses").Where("movie_content_id = ?", movie.ContentID).Pluck("actress_id", &actressIDs).Error)
	require.Empty(t, actressIDs)
}

func TestCollisionServiceReassignReconcilesTransferredCollisions(t *testing.T) {
	for _, tc := range []struct {
		name           string
		existingTarget bool
	}{
		{name: "repoint"},
		{name: "merge existing target credit", existingTarget: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, service, source, selected := collisionFixture(t)
			target := models.Actress{FirstName: "Person", LastName: "Reported", ThumbURL: "target-thumb", Verified: true, Origin: ActressOriginUser}
			require.NoError(t, db.Create(&target).Error)
			movie := models.Movie{ContentID: source.MovieContentID}
			require.NoError(t, db.Model(&movie).Association("Actresses").Replace([]models.Actress{{ID: source.ActressID}}))

			var targetCredit models.MovieCredit
			if tc.existingTarget {
				targetCredit = models.MovieCredit{MovieContentID: source.MovieContentID, ActressID: target.ID, CreditedName: "Target Credit"}
				require.NoError(t, db.Create(&targetCredit).Error)
				require.NoError(t, db.Model(&movie).Association("Actresses").Replace([]models.Actress{{ID: source.ActressID}, {ID: target.ID}}))
				targetCollision := models.CreditCollision{
					CreditID: targetCredit.ID, MovieContentID: source.MovieContentID, Field: models.CreditFieldCreditedName,
					ReportedValue: selected.ReportedValue, CanonicalValue: "stale target", Status: models.CollisionStatusOpen,
					Occurrences: 2, SourcesSeen: "javdb",
				}
				require.NoError(t, db.Create(&targetCollision).Error)
			}

			transferred := []models.CreditCollision{
				{CreditID: source.ID, MovieContentID: source.MovieContentID, Field: models.CreditFieldReportedThumb, ReportedValue: "target-thumb", CanonicalValue: "stale", Status: models.CollisionStatusOpen},
				{CreditID: source.ID, MovieContentID: source.MovieContentID, Field: models.CreditFieldReportedThumb, ReportedValue: "other-thumb", CanonicalValue: "stale", Status: models.CollisionStatusOpen},
				{CreditID: source.ID, MovieContentID: source.MovieContentID, Field: models.CreditFieldCreditedName, ReportedValue: "Different Person", CanonicalValue: "stale", Status: models.CollisionStatusOpen},
			}
			for i := range transferred {
				require.NoError(t, db.Create(&transferred[i]).Error)
			}

			remaining, err := service.Resolve(context.Background(), selected.ID, models.CollisionResolutionReassign, target.ID)
			require.NoError(t, err)
			require.Equal(t, 2, remaining)
			var actressIDs []uint
			require.NoError(t, db.Table("movie_actresses").Where("movie_content_id = ?", source.MovieContentID).Pluck("actress_id", &actressIDs).Error)
			require.ElementsMatch(t, []uint{target.ID}, actressIDs)
			credits, err := service.Credits.ListByMovie(context.Background(), source.MovieContentID)
			require.NoError(t, err)
			require.Len(t, credits, 1)
			require.Equal(t, target.ID, credits[0].ActressID)

			var rows []models.CreditCollision
			require.NoError(t, db.Where("credit_id = ?", credits[0].ID).Find(&rows).Error)
			require.Len(t, rows, 4)
			for _, row := range rows {
				switch {
				case row.Field == models.CreditFieldCreditedName && row.ReportedValue == selected.ReportedValue:
					require.Equal(t, target.FullName(), row.CanonicalValue)
					require.Equal(t, models.CollisionStatusResolved, row.Status)
					if tc.existingTarget {
						require.Equal(t, models.CollisionResolutionAdoptCanonical, row.Resolution)
					} else {
						require.Equal(t, models.CollisionResolutionReassign, row.Resolution)
					}
				case row.Field == models.CreditFieldReportedThumb && row.ReportedValue == "target-thumb":
					require.Equal(t, target.ThumbURL, row.CanonicalValue)
					require.Equal(t, models.CollisionStatusResolved, row.Status)
					require.Equal(t, models.CollisionResolutionAdoptCanonical, row.Resolution)
				case row.Field == models.CreditFieldReportedThumb && row.ReportedValue == "other-thumb":
					require.Equal(t, target.ThumbURL, row.CanonicalValue)
					require.Equal(t, models.CollisionStatusOpen, row.Status)
				case row.Field == models.CreditFieldCreditedName && row.ReportedValue == "Different Person":
					require.Equal(t, target.FullName(), row.CanonicalValue)
					require.Equal(t, models.CollisionStatusOpen, row.Status)
				default:
					t.Fatalf("unexpected collision: %+v", row)
				}
			}
			var movieAfter models.Movie
			require.NoError(t, db.First(&movieAfter, "content_id = ?", source.MovieContentID).Error)
			require.True(t, movieAfter.RenderDirty)
			require.EqualValues(t, 1, movieAfter.RenderGeneration)
		})
	}
}

func TestMovieCreditRepositoryReassignReconcilesTransferredCollisions(t *testing.T) {
	db, _, source, selected := collisionFixture(t)
	target := models.Actress{FirstName: "Person", LastName: "Reported", ThumbURL: "target-thumb", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&target).Error)
	movie := models.Movie{ContentID: source.MovieContentID}
	require.NoError(t, db.Model(&movie).Association("Actresses").Replace([]models.Actress{{ID: source.ActressID}}))
	rows := []models.CreditCollision{
		{CreditID: source.ID, MovieContentID: source.MovieContentID, Field: models.CreditFieldReportedThumb, ReportedValue: "target-thumb", CanonicalValue: "stale", Status: models.CollisionStatusOpen},
		{CreditID: source.ID, MovieContentID: source.MovieContentID, Field: models.CreditFieldReportedThumb, ReportedValue: "other-thumb", CanonicalValue: "stale", Status: models.CollisionStatusOpen},
	}
	for i := range rows {
		require.NoError(t, db.Create(&rows[i]).Error)
	}
	require.NoError(t, NewMovieCreditRepository(db).ReassignCredit(context.Background(), &source, target.ID))
	credits, err := NewMovieCreditRepository(db).ListByMovie(context.Background(), source.MovieContentID)
	require.NoError(t, err)
	require.Len(t, credits, 1)
	require.Equal(t, target.ID, credits[0].ActressID)
	var saved []models.CreditCollision
	require.NoError(t, db.Where("credit_id = ?", credits[0].ID).Find(&saved).Error)
	require.Len(t, saved, 3)
	for _, row := range saved {
		switch row.ReportedValue {
		case selected.ReportedValue:
			require.Equal(t, target.FullName(), row.CanonicalValue)
			require.Equal(t, models.CollisionStatusResolved, row.Status)
			require.Equal(t, models.CollisionResolutionAdoptCanonical, row.Resolution)
		case "target-thumb":
			require.Equal(t, target.ThumbURL, row.CanonicalValue)
			require.Equal(t, models.CollisionStatusResolved, row.Status)
		case "other-thumb":
			require.Equal(t, target.ThumbURL, row.CanonicalValue)
			require.Equal(t, models.CollisionStatusOpen, row.Status)
		default:
			t.Fatalf("unexpected collision: %+v", row)
		}
	}
	var movieAfter models.Movie
	require.NoError(t, db.First(&movieAfter, "content_id = ?", source.MovieContentID).Error)
	require.True(t, movieAfter.RenderDirty)
	require.EqualValues(t, 1, movieAfter.RenderGeneration)
}

func TestCollisionServiceReassignRollsBackCollisionReconciliation(t *testing.T) {
	db, service, source, selected := collisionFixture(t)
	target := models.Actress{FirstName: "Target", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&target).Error)
	movie := models.Movie{ContentID: source.MovieContentID}
	require.NoError(t, db.Model(&movie).Association("Actresses").Replace([]models.Actress{{ID: source.ActressID}}))
	extra := models.CreditCollision{
		CreditID: source.ID, MovieContentID: source.MovieContentID, Field: models.CreditFieldCreditedName,
		ReportedValue: "Other Person", CanonicalValue: "stale", Status: models.CollisionStatusOpen,
	}
	require.NoError(t, db.Create(&extra).Error)
	require.NoError(t, db.Exec("CREATE TRIGGER fail_reassign_collision_reconcile BEFORE UPDATE OF canonical_value ON credit_collisions BEGIN SELECT RAISE(ABORT, 'injected'); END").Error)
	t.Cleanup(func() { _ = db.Exec("DROP TRIGGER fail_reassign_collision_reconcile").Error })

	_, err := service.Resolve(context.Background(), selected.ID, models.CollisionResolutionReassign, target.ID)
	require.Error(t, err)
	var savedSelected models.CreditCollision
	require.NoError(t, db.First(&savedSelected, selected.ID).Error)
	require.Equal(t, models.CollisionStatusOpen, savedSelected.Status)
	require.Empty(t, savedSelected.Resolution)
	var savedExtra models.CreditCollision
	require.NoError(t, db.First(&savedExtra, extra.ID).Error)
	require.Equal(t, source.ID, savedExtra.CreditID)
	var savedCredit models.MovieCredit
	require.NoError(t, db.First(&savedCredit, source.ID).Error)
	require.Equal(t, source.ActressID, savedCredit.ActressID)
	var targetCredits int64
	require.NoError(t, db.Model(&models.MovieCredit{}).Where("movie_content_id = ? AND actress_id = ?", source.MovieContentID, target.ID).Count(&targetCredits).Error)
	require.Zero(t, targetCredits)
	var actressIDs []uint
	require.NoError(t, db.Table("movie_actresses").Where("movie_content_id = ?", source.MovieContentID).Pluck("actress_id", &actressIDs).Error)
	require.ElementsMatch(t, []uint{source.ActressID}, actressIDs)
}

func TestCollisionServiceSuppressionRestoreReconcilesRescrapedEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		pinned bool
	}{
		{name: "unpinned"},
		{name: "pinned", pinned: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newCreditTestDB(t)
			repos := db.Repositories()
			actress := models.Actress{DMMID: 7001, FirstName: "Canonical", LastName: "Person", ThumbURL: "canonical-thumb", Verified: true, Origin: ActressOriginUser}
			require.NoError(t, repos.ActressRepo.Create(context.Background(), &actress))
			first := creditMovie("suppression-restore-"+tc.name, []models.MovieCredit{{
				CreditedName: "Old Person", ReportedThumbURL: "old-thumb", Source: "dmm",
				Scraped: models.Actress{DMMID: actress.DMMID, FirstName: "Old", LastName: "Person", ThumbURL: "old-thumb"},
			}})
			saved, err := repos.MovieRepo.UpsertWithTranslations(context.Background(), first, nil, nil)
			require.NoError(t, err)
			require.Len(t, saved.Credits, 1)
			creditID := saved.Credits[0].ID
			collisions, err := repos.CreditCollisionRepo.ListOpenByMovie(context.Background(), first.ContentID)
			require.NoError(t, err)
			require.Len(t, collisions, 2)
			for i := range collisions {
				if collisions[i].Field == models.CreditFieldCreditedName {
					require.NoError(t, db.Model(&models.CreditCollision{}).Where("id = ?", collisions[i].ID).Update("user_pinned", tc.pinned).Error)
				}
			}

			service := NewCollisionService(db)
			require.NoError(t, service.SetCreditSuppressed(context.Background(), creditID, true))
			second := creditMovie(first.ContentID, []models.MovieCredit{{
				CreditedName: "New Person", ReportedThumbURL: "new-thumb", Source: "javdb",
				Scraped: models.Actress{DMMID: actress.DMMID, FirstName: "New", LastName: "Person", ThumbURL: "new-thumb"},
			}})
			_, err = repos.MovieRepo.UpsertWithTranslations(context.Background(), second, nil, nil)
			require.NoError(t, err)
			var storedCredit models.MovieCredit
			require.NoError(t, db.First(&storedCredit, creditID).Error)
			require.True(t, storedCredit.Suppressed)
			require.Equal(t, "New Person", storedCredit.CreditedName)
			require.Equal(t, "new-thumb", storedCredit.ReportedThumbURL)

			require.NoError(t, service.SetCreditSuppressed(context.Background(), creditID, false))
			require.NoError(t, db.First(&storedCredit, creditID).Error)
			require.False(t, storedCredit.Suppressed)
			var stored []models.CreditCollision
			require.NoError(t, db.Where("credit_id = ?", creditID).Find(&stored).Error)
			require.Len(t, stored, 4)
			for _, collision := range stored {
				switch collision.ReportedValue {
				case "Old Person":
					require.Equal(t, models.CollisionStatusResolved, collision.Status)
					require.Equal(t, models.CollisionResolutionByRemoval, collision.Resolution)
					require.Equal(t, tc.pinned, collision.UserPinned)
				case "old-thumb":
					require.Equal(t, models.CollisionStatusResolved, collision.Status)
					require.Equal(t, models.CollisionResolutionByRemoval, collision.Resolution)
					require.False(t, collision.UserPinned)
				case "New Person":
					require.Equal(t, models.CollisionStatusOpen, collision.Status)
					require.Empty(t, collision.Resolution)
					require.False(t, collision.UserPinned)
					require.Equal(t, actress.FullName(), collision.CanonicalValue)
				case "new-thumb":
					require.Equal(t, models.CollisionStatusOpen, collision.Status)
					require.Empty(t, collision.Resolution)
					require.False(t, collision.UserPinned)
					require.Equal(t, actress.ThumbURL, collision.CanonicalValue)
				default:
					t.Fatalf("unexpected collision: %+v", collision)
				}
			}
			var actressIDs []uint
			require.NoError(t, db.Table("movie_actresses").Where("movie_content_id = ?", first.ContentID).Pluck("actress_id", &actressIDs).Error)
			require.ElementsMatch(t, []uint{actress.ID}, actressIDs)
		})
	}
}

func TestCollisionServiceCandidateSuppressionRestoreKeepsProjectionQuarantined(t *testing.T) {
	db := newCreditTestDB(t)
	candidate := models.Actress{FirstName: "Candidate", LastName: "Restore", Origin: ActressOriginScrape}
	require.NoError(t, db.Create(&candidate).Error)
	movie := models.Movie{ContentID: "candidate-suppression-restore", ID: "candidate-suppression-restore"}
	require.NoError(t, db.Create(&movie).Error)
	credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: candidate.ID, CreditedName: candidate.FullName(), Origin: string(models.CreditOriginScrape)}
	require.NoError(t, db.Create(&credit).Error)
	service := NewCollisionService(db)
	require.NoError(t, service.SetCreditSuppressed(context.Background(), credit.ID, true))
	require.NoError(t, service.SetCreditSuppressed(context.Background(), credit.ID, false))
	var actressIDs []uint
	require.NoError(t, db.Table("movie_actresses").Where("movie_content_id = ?", movie.ContentID).Pluck("actress_id", &actressIDs).Error)
	require.Empty(t, actressIDs)
	var stored models.MovieCredit
	require.NoError(t, db.First(&stored, credit.ID).Error)
	require.False(t, stored.Suppressed)
	var collisionCount int64
	require.NoError(t, db.Model(&models.CreditCollision{}).Where("credit_id = ?", credit.ID).Count(&collisionCount).Error)
	require.Zero(t, collisionCount)
}

func TestCollisionServiceLegacySuppressionRestoreDoesNotCreateCollisions(t *testing.T) {
	db := newCreditTestDB(t)
	actress := models.Actress{FirstName: "Legacy", LastName: "Identity", ThumbURL: "canonical-thumb", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&actress).Error)
	movie := models.Movie{ContentID: "legacy-suppression-restore", ID: "legacy-suppression-restore"}
	require.NoError(t, db.Create(&movie).Error)
	credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: actress.ID, CreditedName: "Old Legacy", ReportedThumbURL: "old-thumb", LegacyInferred: true, Origin: string(models.CreditOriginUser)}
	require.NoError(t, db.Create(&credit).Error)
	service := NewCollisionService(db)
	require.NoError(t, service.SetCreditSuppressed(context.Background(), credit.ID, true))
	require.NoError(t, service.SetCreditSuppressed(context.Background(), credit.ID, false))
	var collisionCount int64
	require.NoError(t, db.Model(&models.CreditCollision{}).Where("credit_id = ?", credit.ID).Count(&collisionCount).Error)
	require.Zero(t, collisionCount)
	var actressIDs []uint
	require.NoError(t, db.Table("movie_actresses").Where("movie_content_id = ?", movie.ContentID).Pluck("actress_id", &actressIDs).Error)
	require.ElementsMatch(t, []uint{actress.ID}, actressIDs)
}

func TestCollisionServiceAdoptedThumbnailSurvivesImport(t *testing.T) {
	db, service, credit, collision := collisionFixture(t)
	require.NoError(t, db.Model(&models.Actress{}).Where("id = ?", credit.ActressID).Update("origin", ActressOriginScrape).Error)
	collision.Field = models.CreditFieldReportedThumb
	collision.ReportedValue = "adopted-thumb"
	collision.CanonicalValue = "old-thumb"
	require.NoError(t, db.Save(&collision).Error)
	_, err := service.Resolve(context.Background(), collision.ID, models.CollisionResolutionAdoptCanonical, 0)
	require.NoError(t, err)
	incoming := models.Actress{ID: credit.ActressID, FirstName: "Imported", LastName: "Name", JapaneseName: "輸入", ThumbURL: "import-thumb"}
	require.NoError(t, service.Actresses.ImportUpsert(context.Background(), &incoming))
	var stored models.Actress
	require.NoError(t, db.First(&stored, credit.ActressID).Error)
	require.Equal(t, "adopted-thumb", stored.ThumbURL)
	require.Equal(t, ActressOriginUser, stored.Origin)
	require.Equal(t, "Truth", stored.FirstName)
	require.Equal(t, "Original", stored.LastName)
}

func TestCollisionServiceAdoptCanonicalReconcilesSiblingCollisions(t *testing.T) {
	db, service, credit, collision := collisionFixture(t)
	require.NoError(t, db.Model(&models.Actress{}).Where("id = ?", credit.ActressID).Update("thumb_url", "https://example.com/old.jpg").Error)
	siblingMovie := models.Movie{ContentID: "sibling-collision", ID: "sibling-collision", Title: "Sibling"}
	require.NoError(t, db.Create(&siblingMovie).Error)
	siblingCredit := models.MovieCredit{MovieContentID: siblingMovie.ContentID, ActressID: credit.ActressID}
	require.NoError(t, db.Create(&siblingCredit).Error)
	rows := []models.CreditCollision{
		{CreditID: siblingCredit.ID, MovieContentID: siblingMovie.ContentID, Field: models.CreditFieldCreditedName, ReportedValue: "Reported Person", CanonicalValue: "Old", Status: models.CollisionStatusOpen},
		{CreditID: siblingCredit.ID, MovieContentID: siblingMovie.ContentID, Field: models.CreditFieldCreditedName, ReportedValue: "Other Person", CanonicalValue: "Old", Status: models.CollisionStatusOpen},
		{CreditID: siblingCredit.ID, MovieContentID: siblingMovie.ContentID, Field: models.CreditFieldCreditedName, ReportedValue: "Reported  Person", CanonicalValue: "Old", Status: models.CollisionStatusOpen, UserPinned: true},
		{CreditID: siblingCredit.ID, MovieContentID: siblingMovie.ContentID, Field: models.CreditFieldIdentityLink, ReportedValue: "Reported Person", CanonicalValue: "Old", Status: models.CollisionStatusOpen},
		{CreditID: siblingCredit.ID, MovieContentID: siblingMovie.ContentID, Field: models.CreditFieldIdentityLink, ReportedValue: "Other Person", CanonicalValue: "Old", Status: models.CollisionStatusOpen},
		{CreditID: siblingCredit.ID, MovieContentID: siblingMovie.ContentID, Field: models.CreditFieldReportedThumb, ReportedValue: "https://example.com/old.jpg", CanonicalValue: "stale", Status: models.CollisionStatusOpen},
		{CreditID: siblingCredit.ID, MovieContentID: siblingMovie.ContentID, Field: models.CreditFieldReportedThumb, ReportedValue: "https://example.com/other.jpg", CanonicalValue: "stale", Status: models.CollisionStatusOpen},
	}
	for i := range rows {
		require.NoError(t, db.Create(&rows[i]).Error)
	}

	remaining, err := service.Resolve(context.Background(), collision.ID, models.CollisionResolutionAdoptCanonical, 0)
	require.NoError(t, err)
	require.Zero(t, remaining)
	var saved []models.CreditCollision
	require.NoError(t, db.Where("credit_id = ?", siblingCredit.ID).Order("id ASC").Find(&saved).Error)
	require.Len(t, saved, len(rows))
	for _, row := range saved {
		if row.Field == models.CreditFieldCreditedName || row.Field == models.CreditFieldIdentityLink {
			require.Equal(t, "Reported Person", row.CanonicalValue)
			if models.NormalizeActressNameKey(row.ReportedValue) == "reported person" && !row.UserPinned {
				require.Equal(t, models.CollisionStatusResolved, row.Status)
				require.Equal(t, models.CollisionResolutionAdoptCanonical, row.Resolution)
			} else {
				require.Equal(t, models.CollisionStatusOpen, row.Status)
			}
		} else {
			require.Equal(t, "https://example.com/old.jpg", row.CanonicalValue)
			if row.ReportedValue == row.CanonicalValue {
				require.Equal(t, models.CollisionStatusResolved, row.Status)
			} else {
				require.Equal(t, models.CollisionStatusOpen, row.Status)
			}
		}
	}
}

func TestCollisionServiceAdoptCanonicalRetargetsAliases(t *testing.T) {
	db, service, credit, collision := collisionFixture(t)
	alias := models.ActressAlias{AliasName: "Legacy Name", CanonicalName: "Original Truth"}
	require.NoError(t, db.Create(&alias).Error)

	_, err := service.Resolve(context.Background(), collision.ID, models.CollisionResolutionAdoptCanonical, 0)
	require.NoError(t, err)
	require.NoError(t, db.First(&alias, alias.ID).Error)
	require.Equal(t, "Reported Person", alias.CanonicalName)
	found, err := service.Actresses.FindVerifiedByAlias(context.Background(), "Legacy Name")
	require.NoError(t, err)
	require.Equal(t, credit.ActressID, found.ID)
}

func TestCollisionServiceAliasRetargetErrorRollsBack(t *testing.T) {
	db, service, _, collision := collisionFixture(t)
	require.NoError(t, db.DB.Exec("DROP TABLE actress_aliases").Error)
	_, err := service.Resolve(context.Background(), collision.ID, models.CollisionResolutionAdoptCanonical, 0)
	require.Error(t, err)
}

func TestCollisionServiceAdoptCanonicalReconciliationErrorRollsBack(t *testing.T) {
	db, service, credit, collision := collisionFixture(t)
	siblingMovie := models.Movie{ContentID: "sibling-reconcile-error", ID: "sibling-reconcile-error"}
	require.NoError(t, db.Create(&siblingMovie).Error)
	siblingCredit := models.MovieCredit{MovieContentID: siblingMovie.ContentID, ActressID: credit.ActressID}
	require.NoError(t, db.Create(&siblingCredit).Error)
	siblingCollision := models.CreditCollision{
		CreditID:       siblingCredit.ID,
		MovieContentID: siblingMovie.ContentID,
		Field:          models.CreditFieldCreditedName,
		ReportedValue:  "Other Person",
		CanonicalValue: "Old",
		Status:         models.CollisionStatusOpen,
	}
	require.NoError(t, db.Create(&siblingCollision).Error)
	injectDatabaseCallbackError(t, db, "update", "credit_collisions", 2)

	_, err := service.Resolve(context.Background(), collision.ID, models.CollisionResolutionAdoptCanonical, 0)
	require.Error(t, err)
	require.NoError(t, db.First(&collision, collision.ID).Error)
	require.Equal(t, models.CollisionStatusOpen, collision.Status)
	require.NoError(t, db.First(&credit, credit.ID).Error)
	var actress models.Actress
	require.NoError(t, db.First(&actress, credit.ActressID).Error)
	require.Equal(t, "Truth", actress.FirstName)
}

func TestCollisionServiceInvalidResolutionRollsBack(t *testing.T) {
	for _, tc := range []struct {
		name, resolution string
		same             bool
	}{
		{"unknown", "invalid", false}, {"missing target", "reassign", false}, {"same target", "reassign", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, service, credit, collision := collisionFixture(t)
			var target uint
			if tc.same {
				target = credit.ActressID
			}
			_, err := service.Resolve(context.Background(), collision.ID, tc.resolution, target)
			require.Error(t, err)
			require.NoError(t, db.First(&collision, collision.ID).Error)
			require.Equal(t, models.CollisionStatusOpen, collision.Status)
			require.Empty(t, collision.Resolution)
		})
	}
}

func TestCollisionServiceOverrideAndSuppression(t *testing.T) {
	db, service, credit, collision := collisionFixture(t)
	ctx := context.Background()
	legacyMovie := models.Movie{ContentID: credit.MovieContentID}
	require.NoError(t, db.Model(&legacyMovie).Association("Actresses").Replace([]models.Actress{{ID: credit.ActressID}}))
	historical := models.CreditCollision{
		CreditID:       credit.ID,
		MovieContentID: credit.MovieContentID,
		Field:          models.CreditFieldCreditedName,
		ReportedValue:  "Older Reported Person",
		CanonicalValue: "Original Truth",
		Status:         models.CollisionStatusResolved,
		Resolution:     models.CollisionResolutionByRemoval,
	}
	require.NoError(t, db.Create(&historical).Error)
	require.ErrorIs(t, service.UpdateCreditOverride(ctx, 9999, "Missing", true), ErrNotFound)
	require.ErrorIs(t, service.SetCreditSuppressed(ctx, 9999, true), ErrNotFound)
	require.NoError(t, service.UpdateCreditOverride(ctx, credit.ID, "User Name", true))
	require.NoError(t, db.First(&credit, credit.ID).Error)
	require.Equal(t, "User Name", credit.OverrideName)
	require.True(t, credit.UserOverride)
	require.Equal(t, "user", credit.Origin)
	require.NoError(t, service.SetCreditSuppressed(ctx, credit.ID, true))
	var ids []uint
	require.NoError(t, db.Table("movie_actresses").Where("movie_content_id = ?", credit.MovieContentID).Pluck("actress_id", &ids).Error)
	require.Empty(t, ids)
	require.NoError(t, db.First(&collision, collision.ID).Error)
	require.Equal(t, models.CollisionResolutionBySuppression, collision.Resolution)
	require.Equal(t, models.CollisionStatusResolved, collision.Status)
	require.NoError(t, db.First(&historical, historical.ID).Error)
	require.Equal(t, models.CollisionResolutionByRemoval, historical.Resolution)
	require.Equal(t, models.CollisionStatusResolved, historical.Status)
	require.NoError(t, service.SetCreditSuppressed(ctx, credit.ID, false))
	ids = nil
	require.NoError(t, db.Table("movie_actresses").Where("movie_content_id = ?", credit.MovieContentID).Pluck("actress_id", &ids).Error)
	require.ElementsMatch(t, []uint{credit.ActressID}, ids)
	require.NoError(t, db.First(&collision, collision.ID).Error)
	require.Equal(t, models.CollisionStatusOpen, collision.Status)
	require.Empty(t, collision.Resolution)
	require.False(t, collision.UserPinned)
	require.NoError(t, db.First(&historical, historical.ID).Error)
	require.Equal(t, models.CollisionResolutionByRemoval, historical.Resolution)
	require.Equal(t, models.CollisionStatusResolved, historical.Status)
	require.NoError(t, service.UpdateCreditOverride(ctx, credit.ID, "", false))
	require.NoError(t, db.First(&credit, credit.ID).Error)
	require.False(t, credit.Suppressed)
	require.False(t, credit.UserOverride)
	var movie models.Movie
	require.NoError(t, db.First(&movie, "content_id = ?", credit.MovieContentID).Error)
	require.EqualValues(t, 4, movie.RenderGeneration)
}

func TestCollisionServiceReassignMergesUserFieldsAndCollisions(t *testing.T) {
	db, service, source, collision := collisionFixture(t)
	target := models.Actress{FirstName: "Target", Verified: true}
	require.NoError(t, db.Create(&target).Error)
	source.UserOverride, source.Suppressed, source.DisplayForceCanonical, source.OrderPinned = true, true, true, true
	source.OverrideName, source.Origin, source.OrderIndex = "User Name", "user", 7
	require.NoError(t, db.Save(&source).Error)
	destination := models.MovieCredit{MovieContentID: source.MovieContentID, ActressID: target.ID, Origin: "scrape"}
	require.NoError(t, db.Create(&destination).Error)
	other := models.CreditCollision{CreditID: destination.ID, MovieContentID: source.MovieContentID, Field: collision.Field, ReportedValue: collision.ReportedValue, Status: models.CollisionStatusOpen, Occurrences: 2, SourcesSeen: "javdb,dmm", UserPinned: true}
	require.NoError(t, db.Create(&other).Error)
	unique := models.CreditCollision{CreditID: source.ID, MovieContentID: source.MovieContentID, Field: models.CreditFieldReportedThumb, ReportedValue: "other", Status: models.CollisionStatusOpen}
	require.NoError(t, db.Create(&unique).Error)
	remaining, err := service.Resolve(context.Background(), collision.ID, models.CollisionResolutionReassign, target.ID)
	require.NoError(t, err)
	require.Equal(t, 2, remaining)
	require.NoError(t, db.First(&destination, destination.ID).Error)
	require.True(t, destination.UserOverride)
	require.True(t, destination.Suppressed)
	require.True(t, destination.DisplayForceCanonical)
	require.True(t, destination.OrderPinned)
	require.Equal(t, 7, destination.OrderIndex)
	require.Equal(t, "User Name", destination.OverrideName)
	require.Equal(t, "user", destination.Origin)
	require.NoError(t, db.First(&other, other.ID).Error)
	require.Equal(t, 3, other.Occurrences)
	require.Equal(t, "javdb,dmm", other.SourcesSeen)
	require.True(t, other.UserPinned)
	require.NoError(t, db.First(&unique, unique.ID).Error)
	require.Equal(t, destination.ID, unique.CreditID)
	var count int64
	require.NoError(t, db.Model(&models.MovieCredit{}).Where("id = ?", source.ID).Count(&count).Error)
	require.Zero(t, count)
}

func TestCollisionServiceReassignRejectsInvalidTarget(t *testing.T) {
	for _, target := range []models.Actress{{}, {FirstName: "Candidate", Origin: "scrape"}} {
		db, service, _, collision := collisionFixture(t)
		targetID := uint(999)
		if target.FirstName != "" {
			require.NoError(t, db.Create(&target).Error)
			targetID = target.ID
		}
		_, err := service.Resolve(t.Context(), collision.ID, models.CollisionResolutionReassign, targetID)
		require.ErrorIs(t, err, ErrNotFound)
		require.NoError(t, db.First(&collision, collision.ID).Error)
		require.Equal(t, models.CollisionStatusOpen, collision.Status)
	}
}

func TestCollisionServiceReassignRejectsCurrentIdentity(t *testing.T) {
	db, service, credit, collision := collisionFixture(t)
	_, err := service.Resolve(t.Context(), collision.ID, models.CollisionResolutionReassign, credit.ActressID)
	require.ErrorContains(t, err, "already linked")
	require.NoError(t, db.First(&collision, collision.ID).Error)
	require.Equal(t, models.CollisionStatusOpen, collision.Status)
}

func TestReassignCreditReturnsLookupError(t *testing.T) {
	db := newCreditTestDB(t)
	require.NoError(t, db.Close())
	err := reassignCreditTx(db.DB, &models.MovieCredit{}, 1)
	require.Error(t, err)
}

func TestCollisionServiceAliasRejectsExistingOwner(t *testing.T) {
	db, service, credit, collision := collisionFixture(t)
	collision.CanonicalValue = ""
	require.NoError(t, db.Save(&collision).Error)
	alias := models.ActressAlias{AliasName: collision.ReportedValue, CanonicalName: "Old"}
	require.NoError(t, db.Create(&alias).Error)
	_, err := service.Resolve(context.Background(), collision.ID, models.CollisionResolutionAdoptAlias, 0)
	require.ErrorIs(t, err, ErrActressAliasOwnershipConflict)
	_, err = service.Actresses.FindByID(context.Background(), credit.ActressID)
	require.NoError(t, err)
	require.NoError(t, db.First(&alias, alias.ID).Error)
	require.Equal(t, "Old", alias.CanonicalName)
	require.NoError(t, db.First(&collision, collision.ID).Error)
	require.Equal(t, models.CollisionStatusOpen, collision.Status)
}

func TestCollisionResolutionRejectsAliasForNonNameField(t *testing.T) {
	db, service, _, collision := collisionFixture(t)
	collision.Field = models.CreditFieldReportedThumb
	collision.ReportedValue = "https://example.com/reported.jpg"
	require.NoError(t, db.Save(&collision).Error)
	_, err := service.Resolve(t.Context(), collision.ID, models.CollisionResolutionAdoptAlias, 0)
	require.ErrorContains(t, err, "adopt_alias requires a credited_name collision")
	require.NoError(t, db.First(&collision, collision.ID).Error)
	require.Equal(t, models.CollisionStatusOpen, collision.Status)
	var aliases int64
	require.NoError(t, db.Model(&models.ActressAlias{}).Count(&aliases).Error)
	require.Zero(t, aliases)
}

func TestCollisionServiceCancelledContext(t *testing.T) {
	_, service, credit, collision := collisionFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := service.Resolve(ctx, collision.ID, models.CollisionResolutionKeepIdentity, 0)
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, service.UpdateCreditOverride(ctx, credit.ID, "x", true), context.Canceled)
	require.ErrorIs(t, service.SetCreditSuppressed(ctx, credit.ID, true), context.Canceled)
}
