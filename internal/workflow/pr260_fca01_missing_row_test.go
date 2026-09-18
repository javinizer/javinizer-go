package workflow

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestPR260FCA01NeverPersistedContentIDStillPublishes(t *testing.T) {
	db, _ := pr260ArtifactDB(t)
	movie := models.Movie{ContentID: "pr260-fca01-unpersisted", ID: "PR260-FCA01-LEGACY"}
	fs := afero.NewMemMapFs()
	path := "/incoming/PR260-FCA01-LEGACY.mp4"
	require.NoError(t, fs.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, afero.WriteFile(fs, path, []byte("legacy"), 0644))
	root := "/library/legacy"
	orch := pr260RealApply(fs, &movie, organizer.MediaFormatConfig{}, nil, true)
	cmd := ApplyCmd{Movie: &movie, PublicationFence: db.Repositories().MovieRepo.(database.ApplyPublicationFencer), Match: models.FileMatchInfo{Path: path, Name: filepath.Base(path), Extension: ".mp4", MovieID: movie.ID}, DestPath: root, Organize: OrganizeOptions{MoveFiles: true}}
	result, err := orch.Execute(context.Background(), cmd)
	require.NoError(t, err)
	exists, err := afero.Exists(fs, path)
	require.NoError(t, err)
	require.False(t, exists)
	require.NotNil(t, result.OrganizeResult)
	require.NotEmpty(t, result.OrganizeResult.NewPath)
	exists, err = afero.Exists(fs, result.OrganizeResult.NewPath)
	require.NoError(t, err)
	require.True(t, exists)
}

func TestPR260FCA01DeletedPersistedMovieRejectsStagedPublication(t *testing.T) {
	db, dsn := pr260ArtifactDB(t)
	mutation, err := database.New(&database.Config{Type: "sqlite", DSN: dsn, LogLevel: "error"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = mutation.Close() })
	movie := models.Movie{ContentID: "pr260-fca01-deleted", ID: "PR260-FCA01", Title: "Deleted", RenderGeneration: 3}
	require.NoError(t, db.Create(&movie).Error)
	persisted, err := db.Repositories().MovieRepo.FindByID(context.Background(), movie.ID)
	require.NoError(t, err)
	require.NotNil(t, persisted)
	fs := afero.NewMemMapFs()
	path := "/incoming/PR260-FCA01.mp4"
	require.NoError(t, fs.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, afero.WriteFile(fs, path, []byte("source"), 0644))
	root := "/library/FCA01"
	orch := pr260RealApply(fs, &movie, organizer.MediaFormatConfig{}, nil, true)
	entered, resume := make(chan struct{}), make(chan struct{})
	orch.artifactPrepared = func() { close(entered); <-resume }
	cmd := ApplyCmd{Movie: &movie, PersistedMovie: true, PublicationFence: db.Repositories().MovieRepo.(database.ApplyPublicationFencer), Match: models.FileMatchInfo{Path: path, Name: filepath.Base(path), Extension: ".mp4", MovieID: movie.ID}, DestPath: root, Organize: OrganizeOptions{MoveFiles: true}}
	done := make(chan error, 1)
	go func() { _, err := orch.Execute(context.Background(), cmd); done <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("staging did not reach publication")
	}
	require.NoError(t, mutation.Where("content_id = ?", movie.ContentID).Delete(&models.Movie{}).Error)
	close(resume)
	select {
	case err = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("apply hung after deletion")
	}
	require.ErrorIs(t, err, database.ErrNotFound)
	exists, err := afero.Exists(fs, path)
	require.NoError(t, err)
	require.True(t, exists)
	require.NoError(t, afero.Walk(fs, "/library", func(name string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.Mode().IsRegular() {
			t.Errorf("unauthorized output at %s", name)
		}
		return nil
	}))
	absent, err := db.Repositories().MovieRepo.FindByID(context.Background(), movie.ID)
	require.ErrorIs(t, err, database.ErrNotFound)
	require.Nil(t, absent)
}
