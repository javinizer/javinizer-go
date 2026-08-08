package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestRenameNameFields(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)
	actress := &models.Actress{DMMID: 42, JapaneseName: "original"}
	require.NoError(t, repo.Create(context.Background(), actress))
	err := repo.RenameNameFields(context.Background(), actress.ID, "NewFirst", "NewLast", "NewJapanese")
	require.NoError(t, err)
	updated, err := repo.FindByID(context.Background(), actress.ID)
	require.NoError(t, err)
	if updated.FirstName != "NewFirst" || updated.LastName != "NewLast" || updated.JapaneseName != "NewJapanese" {
		t.Fatalf("rename did not apply: %+v", updated)
	}
}

func TestReplaceThumbnailForSyncTaskCoverage(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)
	actress := &models.Actress{DMMID: 42, JapaneseName: "test", ThumbURL: "https://example.test/old.jpg"}
	require.NoError(t, repo.Create(context.Background(), actress))
	replaced, err := repo.ReplaceThumbnailForSyncTask(context.Background(), actress.ID, 42, "https://example.test/old.jpg", "https://example.test/new.jpg", "", "")
	require.NoError(t, err)
	if !replaced {
		t.Fatal("expected replaced=true")
	}
}

func TestAssignDMMIDIfMissingForSyncTaskCoverage(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)
	actress := &models.Actress{JapaneseName: "test"}
	require.NoError(t, repo.Create(context.Background(), actress))
	assigned, err := repo.AssignDMMIDIfMissingForSyncTask(context.Background(), actress.ID, 999, "", "")
	require.NoError(t, err)
	if !assigned {
		t.Fatal("expected assigned=true")
	}
}
