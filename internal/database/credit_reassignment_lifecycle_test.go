package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestActressMergeRetargetsCreditReassignments(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)

	target := models.Actress{DMMID: 91001, FirstName: "Target", Verified: true, Origin: ActressOriginUser}
	source := models.Actress{DMMID: 91002, FirstName: "Source", Verified: true, Origin: ActressOriginUser}
	other := models.Actress{DMMID: 91003, FirstName: "Other", Verified: true, Origin: ActressOriginUser}
	external := models.Actress{DMMID: 91004, FirstName: "External", Verified: true, Origin: ActressOriginUser}
	for _, actress := range []*models.Actress{&target, &source, &other, &external} {
		require.NoError(t, db.Create(actress).Error)
	}

	movieIDs := []string{"reassignment-merge-1", "reassignment-merge-2", "reassignment-merge-3"}
	for _, movieID := range movieIDs {
		require.NoError(t, db.Create(&models.Movie{ContentID: movieID, ID: movieID, Title: movieID}).Error)
	}
	require.NoError(t, db.Create(&models.MovieCreditReassignment{
		MovieContentID:  movieIDs[0],
		SourceActressID: external.ID,
		TargetActressID: source.ID,
	}).Error)
	require.NoError(t, db.Create(&models.MovieCreditReassignment{
		MovieContentID:  movieIDs[1],
		SourceActressID: source.ID,
		TargetActressID: other.ID,
	}).Error)
	require.NoError(t, db.Create(&models.MovieCreditReassignment{
		MovieContentID:  movieIDs[2],
		SourceActressID: source.ID,
		TargetActressID: source.ID,
	}).Error)

	result, err := repo.Merge(context.Background(), target.ID, source.ID, map[string]string{"dmm_id": "target"})
	require.NoError(t, err)
	require.Equal(t, target.ID, result.MergedActress.ID)

	var rows []models.MovieCreditReassignment
	require.NoError(t, db.Order("movie_content_id ASC").Find(&rows).Error)
	require.Len(t, rows, 2)
	got := make(map[string][2]uint, len(rows))
	for _, row := range rows {
		got[row.MovieContentID] = [2]uint{row.SourceActressID, row.TargetActressID}
	}
	require.Equal(t, [2]uint{external.ID, target.ID}, got[movieIDs[0]])
	require.Equal(t, [2]uint{target.ID, other.ID}, got[movieIDs[1]])
}

func TestParentDeletesRemoveCreditReassignments(t *testing.T) {
	t.Run("movie", func(t *testing.T) {
		db := newCreditTestDB(t)
		source := models.Actress{DMMID: 91501}
		target := models.Actress{DMMID: 91502}
		require.NoError(t, db.Create(&source).Error)
		require.NoError(t, db.Create(&target).Error)
		movie := models.Movie{ContentID: "reassignment-delete-movie", ID: "reassignment-delete-movie", Title: "Delete movie"}
		require.NoError(t, db.Create(&movie).Error)
		require.NoError(t, db.Create(&models.MovieCreditReassignment{
			MovieContentID:  movie.ContentID,
			SourceActressID: source.ID,
			TargetActressID: target.ID,
		}).Error)

		require.NoError(t, NewMovieRepository(db).Delete(context.Background(), movie.ID))
		var count int64
		require.NoError(t, db.Model(&models.MovieCreditReassignment{}).Count(&count).Error)
		require.Zero(t, count)
	})

	t.Run("actress", func(t *testing.T) {
		db := newCreditTestDB(t)
		source := models.Actress{DMMID: 91601}
		target := models.Actress{DMMID: 91602}
		require.NoError(t, db.Create(&source).Error)
		require.NoError(t, db.Create(&target).Error)
		movie := models.Movie{ContentID: "reassignment-delete-actress", ID: "reassignment-delete-actress", Title: "Delete actress"}
		require.NoError(t, db.Create(&movie).Error)
		require.NoError(t, db.Create(&models.MovieCreditReassignment{
			MovieContentID:  movie.ContentID,
			SourceActressID: source.ID,
			TargetActressID: target.ID,
		}).Error)

		require.NoError(t, NewActressRepository(db).Delete(context.Background(), target.ID))
		var count int64
		require.NoError(t, db.Model(&models.MovieCreditReassignment{}).Count(&count).Error)
		require.Zero(t, count)
	})
}

func TestMoveCreditReassignmentsTxRemovesSelfMappings(t *testing.T) {
	db := newCreditTestDB(t)
	source := models.Actress{DMMID: 92001, FirstName: "Source"}
	target := models.Actress{DMMID: 92002, FirstName: "Target"}
	require.NoError(t, db.Create(&source).Error)
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&models.Movie{ContentID: "reassignment-self", ID: "reassignment-self", Title: "Self"}).Error)
	require.NoError(t, db.Create(&models.MovieCreditReassignment{
		MovieContentID:  "reassignment-self",
		SourceActressID: source.ID,
		TargetActressID: source.ID,
	}).Error)

	require.NoError(t, moveCreditReassignmentsTx(db.DB, source.ID, target.ID))
	var count int64
	require.NoError(t, db.Model(&models.MovieCreditReassignment{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestCreditReassignmentCleanupErrors(t *testing.T) {
	db := newCreditTestDB(t)
	require.NoError(t, db.Migrator().DropTable(&models.MovieCreditReassignment{}))
	require.Error(t, deleteCreditReassignmentsTx(db.DB, "movie_content_id = ?", "test", "missing"))
	require.Error(t, moveCreditReassignmentsTx(db.DB, 1, 2))
}

func TestCreditReassignmentCallerErrors(t *testing.T) {
	t.Run("merge", func(t *testing.T) {
		db := newCreditTestDB(t)
		repo := NewActressRepository(db)
		target := models.Actress{DMMID: 95001}
		source := models.Actress{DMMID: 95002}
		require.NoError(t, db.Create(&target).Error)
		require.NoError(t, db.Create(&source).Error)
		plan, err := repo.merger.PlanMerge(context.Background(), target.ID, source.ID, map[string]string{"dmm_id": "target"})
		require.NoError(t, err)
		require.NoError(t, db.Migrator().DropTable(&models.MovieCreditReassignment{}))

		_, err = repo.merger.ExecuteMerge(context.Background(), plan, db)
		require.Error(t, err)
	})

	t.Run("actress delete", func(t *testing.T) {
		db := newCreditTestDB(t)
		actress := models.Actress{DMMID: 95003}
		require.NoError(t, db.Create(&actress).Error)
		require.NoError(t, db.Migrator().DropTable(&models.MovieCreditReassignment{}))

		require.Error(t, NewActressRepository(db).Delete(context.Background(), actress.ID))
	})

	t.Run("movie delete", func(t *testing.T) {
		db := newCreditTestDB(t)
		movie := models.Movie{ContentID: "reassignment-caller-error", ID: "reassignment-caller-error", Title: "Caller error"}
		require.NoError(t, db.Create(&movie).Error)
		require.NoError(t, db.Migrator().DropTable(&models.MovieCreditReassignment{}))

		require.Error(t, NewMovieRepository(db).Delete(context.Background(), movie.ID))
	})
}

func TestMoveCreditReassignmentsTxDeleteError(t *testing.T) {
	db := newCreditTestDB(t)
	source := models.Actress{DMMID: 93001}
	target := models.Actress{DMMID: 93002}
	require.NoError(t, db.Create(&source).Error)
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&models.Movie{ContentID: "reassignment-delete-error", ID: "reassignment-delete-error", Title: "Delete error"}).Error)
	require.NoError(t, db.Create(&models.MovieCreditReassignment{
		MovieContentID:  "reassignment-delete-error",
		SourceActressID: source.ID,
		TargetActressID: target.ID,
	}).Error)
	require.NoError(t, db.Exec("CREATE TRIGGER fail_credit_reassignment_move_delete BEFORE DELETE ON movie_credit_reassignments BEGIN SELECT RAISE(ABORT, 'injected database error'); END").Error)
	t.Cleanup(func() { _ = db.Exec("DROP TRIGGER fail_credit_reassignment_move_delete").Error })

	require.Error(t, moveCreditReassignmentsTx(db.DB, source.ID, target.ID))
}

func TestMoveCreditReassignmentsTxCreateError(t *testing.T) {
	db := newCreditTestDB(t)
	source := models.Actress{DMMID: 94001}
	target := models.Actress{DMMID: 94002}
	other := models.Actress{DMMID: 94003}
	require.NoError(t, db.Create(&source).Error)
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&other).Error)
	require.NoError(t, db.Create(&models.Movie{ContentID: "reassignment-create-error", ID: "reassignment-create-error", Title: "Create error"}).Error)
	require.NoError(t, db.Create(&models.MovieCreditReassignment{
		MovieContentID:  "reassignment-create-error",
		SourceActressID: source.ID,
		TargetActressID: other.ID,
	}).Error)
	require.NoError(t, db.Exec("CREATE TRIGGER fail_credit_reassignment_move_create BEFORE INSERT ON movie_credit_reassignments BEGIN SELECT RAISE(ABORT, 'injected database error'); END").Error)
	t.Cleanup(func() { _ = db.Exec("DROP TRIGGER fail_credit_reassignment_move_create").Error })

	require.Error(t, moveCreditReassignmentsTx(db.DB, source.ID, target.ID))
}
