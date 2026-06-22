package database

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
)

func setupBaseRepoTestDB(t *testing.T) *DB {
	t.Helper()
	cfg := &Config{Type: "sqlite",
		DSN:      ":memory:",
		LogLevel: "silent"}
	db, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}
	if err := db.RunMigrationsOnStartup(context.Background()); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	return db
}

func newTestHistoryRepo(db *DB) *BaseRepository[models.History, uint] {
	return NewBaseRepository[models.History, uint](
		db, "history",
		func(h models.History) string { return fmt.Sprintf("%d", h.ID) },
		withDefaultOrder[models.History, uint]("created_at DESC"),
		WithNewEntity[models.History, uint](func() models.History { return models.History{} }),
	)
}

func newTestHistoryRepoNoOrder(db *DB) *BaseRepository[models.History, uint] {
	return NewBaseRepository[models.History, uint](
		db, "history",
		func(h models.History) string { return fmt.Sprintf("%d", h.ID) },
		WithNewEntity[models.History, uint](func() models.History { return models.History{} }),
	)
}

func newTestJobRepo(db *DB) *BaseRepository[models.Job, string] {
	return NewBaseRepository[models.Job, string](
		db, "job",
		func(j models.Job) string { return j.ID },
		withDefaultOrder[models.Job, string]("started_at DESC"),
		WithNewEntity[models.Job, string](func() models.Job { return models.Job{} }),
	)
}

func newTestGenreRepo(db *DB) *BaseRepository[models.Genre, uint] {
	return NewBaseRepository[models.Genre, uint](
		db, "genre",
		func(g models.Genre) string { return g.Name },
		WithNewEntity[models.Genre, uint](func() models.Genre { return models.Genre{} }),
	)
}

func TestBaseRepository_Create(t *testing.T) {
	db := setupBaseRepoTestDB(t)
	repo := newTestHistoryRepo(db)

	history := &models.History{MovieID: "TEST-001", Operation: models.HistoryOpOrganize, Status: models.HistoryStatusSuccess}
	err := repo.Create(context.TODO(), history)
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if history.ID == 0 {
		t.Error("expected ID to be set after Create")
	}
}

func TestBaseRepository_FindByID(t *testing.T) {
	db := setupBaseRepoTestDB(t)
	repo := newTestHistoryRepo(db)

	history := &models.History{MovieID: "TEST-001", Operation: models.HistoryOpOrganize, Status: models.HistoryStatusSuccess}
	if err := repo.Create(context.TODO(), history); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	found, err := repo.FindByID(context.TODO(), history.ID)
	if err != nil {
		t.Fatalf("FindByID returned error: %v", err)
	}
	if found.MovieID != "TEST-001" {
		t.Errorf("expected MovieID TEST-001, got %s", found.MovieID)
	}
}

func TestBaseRepository_FindByID_NotFound(t *testing.T) {
	db := setupBaseRepoTestDB(t)
	repo := newTestHistoryRepo(db)

	_, err := repo.FindByID(context.TODO(), 99999)
	if err == nil {
		t.Fatal("expected error for non-existent ID, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestBaseRepository_Delete(t *testing.T) {
	db := setupBaseRepoTestDB(t)
	repo := newTestHistoryRepo(db)

	history := &models.History{MovieID: "TEST-001", Operation: models.HistoryOpOrganize, Status: models.HistoryStatusSuccess}
	if err := repo.Create(context.TODO(), history); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	if err := repo.Delete(context.TODO(), history.ID); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}

	_, err := repo.FindByID(context.TODO(), history.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestBaseRepository_List(t *testing.T) {
	db := setupBaseRepoTestDB(t)
	repo := newTestHistoryRepo(db)

	for i := 0; i < 5; i++ {
		if err := repo.Create(context.TODO(), &models.History{MovieID: fmt.Sprintf("TEST-%03d", i), Operation: models.HistoryOpOrganize, Status: models.HistoryStatusSuccess}); err != nil {
			t.Fatalf("Create %d returned error: %v", i, err)
		}
	}

	results, err := repo.List(context.TODO(), 3, 0)
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}
}

func TestBaseRepository_ListAll(t *testing.T) {
	db := setupBaseRepoTestDB(t)
	repo := newTestHistoryRepo(db)

	for i := 0; i < 5; i++ {
		if err := repo.Create(context.TODO(), &models.History{MovieID: fmt.Sprintf("TEST-%03d", i), Operation: models.HistoryOpOrganize, Status: models.HistoryStatusSuccess}); err != nil {
			t.Fatalf("Create %d returned error: %v", i, err)
		}
	}

	results, err := repo.ListAll(context.TODO())
	if err != nil {
		t.Fatalf("ListAll returned error: %v", err)
	}
	if len(results) != 5 {
		t.Errorf("expected 5 results from ListAll, got %d", len(results))
	}
}

func TestBaseRepository_Count(t *testing.T) {
	db := setupBaseRepoTestDB(t)
	repo := newTestHistoryRepo(db)

	for i := 0; i < 3; i++ {
		if err := repo.Create(context.TODO(), &models.History{MovieID: fmt.Sprintf("TEST-%03d", i), Operation: models.HistoryOpOrganize, Status: models.HistoryStatusSuccess}); err != nil {
			t.Fatalf("Create %d returned error: %v", i, err)
		}
	}

	count, err := repo.Count(context.TODO())
	if err != nil {
		t.Fatalf("Count returned error: %v", err)
	}
	if count != 3 {
		t.Errorf("expected count 3, got %d", count)
	}
}

func TestBaseRepository_Create_ErrorWrapping(t *testing.T) {
	db := setupBaseRepoTestDB(t)
	repo := newTestHistoryRepo(db)

	history := &models.History{MovieID: "TEST-001", Operation: models.HistoryOpOrganize, Status: models.HistoryStatusSuccess}
	if err := repo.Create(context.TODO(), history); err != nil {
		t.Fatalf("first Create returned error: %v", err)
	}

	duplicate := &models.History{ID: history.ID, MovieID: "TEST-002", Operation: models.HistoryOpOrganize, Status: models.HistoryStatusSuccess}
	err := repo.Create(context.TODO(), duplicate)
	if err == nil {
		t.Fatal("expected error for duplicate create, got nil")
	}
}

func TestBaseRepository_StringID(t *testing.T) {
	db := setupBaseRepoTestDB(t)
	repo := newTestJobRepo(db)

	job := &models.Job{ID: "job-001", Status: "running"}
	if err := repo.Create(context.TODO(), job); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	found, err := repo.FindByID(context.TODO(), "job-001")
	if err != nil {
		t.Fatalf("FindByID returned error: %v", err)
	}
	if found.ID != "job-001" {
		t.Errorf("expected ID job-001, got %s", found.ID)
	}

	_, err = repo.FindByID(context.TODO(), "nonexistent")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestBaseRepository_GetDB(t *testing.T) {
	db := setupBaseRepoTestDB(t)
	repo := newTestHistoryRepo(db)

	got := repo.GetDB()
	if got == nil {
		t.Error("GetDB returned nil")
	}
}

func TestBaseRepository_List_DefaultOrder(t *testing.T) {
	db := setupBaseRepoTestDB(t)
	repo := newTestHistoryRepo(db)

	if err := repo.Create(context.TODO(), &models.History{MovieID: "FIRST", Operation: models.HistoryOpOrganize, Status: models.HistoryStatusSuccess}); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if err := repo.Create(context.TODO(), &models.History{MovieID: "SECOND", Operation: models.HistoryOpOrganize, Status: models.HistoryStatusSuccess}); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	results, err := repo.ListAll(context.TODO())
	if err != nil {
		t.Fatalf("ListAll returned error: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}
	if results[0].MovieID != "SECOND" {
		t.Errorf("expected newest first (SECOND), got %s", results[0].MovieID)
	}
}

func TestBaseRepository_FindByID_ErrorWrapping(t *testing.T) {
	db := setupBaseRepoTestDB(t)
	repo := newTestHistoryRepoNoOrder(db)

	_, err := repo.FindByID(context.TODO(), 99999)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	errMsg := err.Error()
	if !strings.Contains(errMsg, "history") {
		t.Errorf("error message should contain 'history', got: %s", errMsg)
	}
}

func TestBaseRepository_Delete_NonExistent(t *testing.T) {
	db := setupBaseRepoTestDB(t)
	repo := newTestHistoryRepoNoOrder(db)

	err := repo.Delete(context.TODO(), 99999)
	if err != nil {
		t.Errorf("deleting non-existent record should not error, got: %v", err)
	}
}

func TestBaseRepository_NoDefaultOrder(t *testing.T) {
	db := setupBaseRepoTestDB(t)
	repo := newTestGenreRepo(db)

	if err := repo.Create(context.TODO(), &models.Genre{Name: "Action"}); err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	results, err := repo.ListAll(context.TODO())
	if err != nil {
		t.Fatalf("ListAll returned error: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

func TestBaseRepository_FindByID_ErrNotFound_WithGormErrRecordNotFound(t *testing.T) {
	db := setupBaseRepoTestDB(t)
	repo := newTestHistoryRepoNoOrder(db)

	_, err := repo.FindByID(context.TODO(), 99999)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}
