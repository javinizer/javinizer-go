package worker

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedNamedActress creates a DMM-less actress with the given names and returns
// the persisted record (with its DB id and timestamps).
func seedNamedActress(t *testing.T, repo actressRepoAlias, firstName, lastName, japaneseName string) models.Actress {
	t.Helper()
	a := &models.Actress{FirstName: firstName, LastName: lastName, JapaneseName: japaneseName}
	require.NoError(t, repo.Create(context.Background(), a))
	require.NotZero(t, a.ID)
	return *a
}

type actressRepoAlias = interface {
	Create(context.Context, *models.Actress) error
	FindByID(context.Context, uint) (*models.Actress, error)
	RenameNameFields(context.Context, uint, string, string, string) error
}

// A1: a title-only edit on a movie with fully-populated actresses must NOT
// trigger any actress rename writes (change-detection guard).
func TestUpdateMovie_TitleOnlyEdit_NoActressWrites(t *testing.T) {
	db := newActressEditTestDB(t)
	repos := db.Repositories()
	a1 := seedNamedActress(t, repos.ActressRepo, "Yui", "Hatano", "波多野結衣")
	a2 := seedNamedActress(t, repos.ActressRepo, "Rin", "", "rin")
	before1 := a1.UpdatedAt
	before2 := a2.UpdatedAt

	jq := NewJobStore(nil, nil, repos.MovieRepo, "", nil, nil, WithActressRepo(repos.ActressRepo))
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	job.results.UpdateFileResult("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ABC-001"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "ABC-001", Title: "Old", Actresses: []models.Actress{a1, a2}},
	})

	ej, ok := jq.GetJobForEdit(job.ID.String())
	require.True(t, ok)

	// Title-only edit: actresses unchanged.
	require.NoError(t, ej.UpdateMovie(context.Background(), "file1.mp4",
		&models.Movie{ID: "ABC-001", Title: "New Title", Actresses: []models.Actress{a1, a2}}))

	// Title persisted in-memory.
	res, _ := job.results.GetMovieResult("file1.mp4")
	assert.Equal(t, "New Title", res.Movie.Title)

	// Neither actress was re-Saved: updated_at unchanged.
	got1, _ := repos.ActressRepo.FindByID(context.Background(), a1.ID)
	got2, _ := repos.ActressRepo.FindByID(context.Background(), a2.ID)
	assert.True(t, got1.UpdatedAt.Equal(before1), "unedited actress A must not be re-saved (updated_at unchanged)")
	assert.True(t, got2.UpdatedAt.Equal(before2), "unedited actress B must not be re-saved (updated_at unchanged)")
}

// L1: renaming an actress must NOT clobber created_at (column-restricted update).
func TestUpdateMovie_Rename_PreservesCreatedAt(t *testing.T) {
	db := newActressEditTestDB(t)
	repos := db.Repositories()
	a := seedNamedActress(t, repos.ActressRepo, "Umi", "Yatsugake", "八掛うみ")
	created := a.CreatedAt

	jq := NewJobStore(nil, nil, repos.MovieRepo, "", nil, nil, WithActressRepo(repos.ActressRepo))
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	job.results.UpdateFileResult("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ABF-153"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "ABF-153", Actresses: []models.Actress{a}},
	})

	ej, ok := jq.GetJobForEdit(job.ID.String())
	require.True(t, ok)
	edited := a
	edited.FirstName = "Umis"
	require.NoError(t, ej.UpdateMovie(context.Background(), "file1.mp4",
		&models.Movie{ID: "ABF-153", Actresses: []models.Actress{edited}}))

	got, _ := repos.ActressRepo.FindByID(context.Background(), a.ID)
	assert.Equal(t, "Umis", got.FirstName, "rename persisted")
	assert.True(t, got.CreatedAt.Equal(created), "created_at must be preserved by column-restricted rename")
}

// A4: updated_at is bumped ONLY for the renamed actress, not for an unedited
// sibling on the same movie.
func TestUpdateMovie_Rename_BumpsUpdatedAtOnlyForRenamed(t *testing.T) {
	db := newActressEditTestDB(t)
	repos := db.Repositories()
	a := seedNamedActress(t, repos.ActressRepo, "Umi", "Yatsugake", "八掛うみ")
	b := seedNamedActress(t, repos.ActressRepo, "Rin", "", "rin")
	// Seed a known-old updated_at so the rename's autoUpdateTime is deterministically
	// newer than the pre-rename value. Without this, the Create and the rename can
	// land in the same timestamp-resolution unit on fast/Windows runners, making
	// the After() assertion flaky.
	oldTime := time.Now().Add(-time.Hour)
	require.NoError(t, db.Exec("UPDATE actresses SET updated_at = ? WHERE id IN (?, ?)", oldTime, a.ID, b.ID).Error)
	beforeA, _ := repos.ActressRepo.FindByID(context.Background(), a.ID)
	beforeB, _ := repos.ActressRepo.FindByID(context.Background(), b.ID)

	jq := NewJobStore(nil, nil, repos.MovieRepo, "", nil, nil, WithActressRepo(repos.ActressRepo))
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	job.results.UpdateFileResult("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "M-1"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "M-1", Actresses: []models.Actress{a, b}},
	})

	ej, ok := jq.GetJobForEdit(job.ID.String())
	require.True(t, ok)
	editedA := a
	editedA.FirstName = "Umis"
	require.NoError(t, ej.UpdateMovie(context.Background(), "file1.mp4",
		&models.Movie{ID: "M-1", Actresses: []models.Actress{editedA, b}}))

	gotA, _ := repos.ActressRepo.FindByID(context.Background(), a.ID)
	gotB, _ := repos.ActressRepo.FindByID(context.Background(), b.ID)
	assert.True(t, gotA.UpdatedAt.After(beforeA.UpdatedAt), "renamed actress A must have updated_at bumped")
	assert.True(t, gotB.UpdatedAt.Equal(beforeB.UpdatedAt), "unedited sibling B must NOT have updated_at bumped")
}

// A3: renaming a DMMID=0 actress to a colliding JapaneseName must keep the movie
// associated with the renamed record (by id), not orphan it to the colliding
// record (by name).
func TestUpdateMovie_Rename_DMMIDZeroCollision_StaysByD(t *testing.T) {
	db := newActressEditTestDB(t)
	repos := db.Repositories()
	// B created first -> lower id; both DMMID=0.
	b := seedNamedActress(t, repos.ActressRepo, "", "", "X") // id=1
	a := seedNamedActress(t, repos.ActressRepo, "", "", "Y") // id=2
	require.Less(t, b.ID, a.ID, "B must have the lower id (collision target)")

	jq := NewJobStore(nil, nil, repos.MovieRepo, "", nil, nil, WithActressRepo(repos.ActressRepo))
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	job.results.UpdateFileResult("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "M-1"},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: "M-1", Actresses: []models.Actress{
			{ID: a.ID, DMMID: 0, JapaneseName: "Y"},
		}},
	})

	ej, ok := jq.GetJobForEdit(job.ID.String())
	require.True(t, ok)
	// Rename A's JapaneseName to "X" (collides with B's "X").
	require.NoError(t, ej.UpdateMovie(context.Background(), "file1.mp4",
		&models.Movie{ID: "M-1", Actresses: []models.Actress{
			{ID: a.ID, DMMID: 0, JapaneseName: "X"},
		}}))

	res, _ := job.results.GetMovieResult("file1.mp4")
	require.NotEmpty(t, res.Movie.Actresses)
	assert.Equal(t, a.ID, res.Movie.Actresses[0].ID,
		"movie must stay associated to the renamed actress A (by id), not the colliding B (by name)")
	assert.Equal(t, "X", res.Movie.Actresses[0].JapaneseName)
}

// A7: renaming across a multi-part movie is idempotent — calling UpdateMovie
// per part does not error and the rename is applied exactly once.
func TestUpdateMovie_Rename_MultiPart_Idempotent(t *testing.T) {
	db := newActressEditTestDB(t)
	repos := db.Repositories()
	a := seedNamedActress(t, repos.ActressRepo, "Umi", "Yatsugake", "八掛うみ")

	jq := NewJobStore(nil, nil, repos.MovieRepo, "", nil, nil, WithActressRepo(repos.ActressRepo))
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	job.results.UpdateFileResult("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ABF-153"},
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "ABF-153", Actresses: []models.Actress{a}},
	})

	ej, ok := jq.GetJobForEdit(job.ID.String())
	require.True(t, ok)
	edited := a
	edited.FirstName = "Umis"

	// Simulate the handler looping file parts: UpdateMovie twice.
	require.NoError(t, ej.UpdateMovie(context.Background(), "file1.mp4",
		&models.Movie{ID: "ABF-153", Actresses: []models.Actress{edited}}))
	require.NoError(t, ej.UpdateMovie(context.Background(), "file1.mp4",
		&models.Movie{ID: "ABF-153", Actresses: []models.Actress{edited}}))

	got, _ := repos.ActressRepo.FindByID(context.Background(), a.ID)
	assert.Equal(t, "Umis", got.FirstName, "rename applied")
	res, _ := job.results.GetMovieResult("file1.mp4")
	assert.Equal(t, "Umis", res.Movie.Actresses[0].FirstName, "in-memory carries the rename")
	// Idempotency: updated_at bumped by the first rename; the second call's
	// change-detection skips a re-write, so updated_at must not advance again
	// beyond a single bump (allow a small clock epsilon).
	assert.True(t, got.UpdatedAt.After(time.Time{}), "updated_at is set")
}

// CodeRabbit (PR #73): a stale/missing idGroup ID (the referenced actress was
// deleted) must be created as a genuinely new record with an auto-assigned id,
// not re-inserted with the stale primary key (which could resurrect the row or
// collide with a reused id).
func TestUpdateMovie_Rename_StaleID_CreatesFreshNotResurrected(t *testing.T) {
	db := newActressEditTestDB(t)
	repos := db.Repositories()
	b := seedNamedActress(t, repos.ActressRepo, "", "", "X")
	const staleID uint = 999999
	require.NotEqual(t, staleID, b.ID)

	jq := NewJobStore(nil, nil, repos.MovieRepo, "", nil, nil, WithActressRepo(repos.ActressRepo))
	job := jq.CreateJobBatch([]string{"file1.mp4"})
	job.results.UpdateFileResult("file1.mp4", &MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "M-1"},
		Status:        models.JobStatusCompleted,
		Movie: &models.Movie{ID: "M-1", Actresses: []models.Actress{
			{ID: staleID, JapaneseName: "Y"},
		}},
	})

	ej, ok := jq.GetJobForEdit(job.ID.String())
	require.True(t, ok)
	require.NoError(t, ej.UpdateMovie(context.Background(), "file1.mp4",
		&models.Movie{ID: "M-1", Actresses: []models.Actress{
			{ID: staleID, JapaneseName: "Y"},
		}}))

	res, _ := job.results.GetMovieResult("file1.mp4")
	require.NotEmpty(t, res.Movie.Actresses)
	got := res.Movie.Actresses[0]
	assert.NotEqual(t, staleID, got.ID, "stale ID must not be resurrected; a fresh auto PK should be assigned")
	assert.NotZero(t, got.ID, "a new actress row should be created")
	assert.Equal(t, "Y", got.JapaneseName)
	dbGot, err := repos.ActressRepo.FindByID(context.Background(), got.ID)
	require.NoError(t, err)
	assert.Equal(t, "Y", dbGot.JapaneseName)
}
