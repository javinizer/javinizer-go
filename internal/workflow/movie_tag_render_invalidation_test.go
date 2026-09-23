package workflow

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestMovieTagMutationRejectsStagedNFOAndRetryUsesNewTags(t *testing.T) {
	db, _ := pr260ArtifactDB(t)
	movie := models.Movie{ContentID: "tag-race-content", ID: "TAG-RACE-DISPLAY", Title: "Tag race", RenderDirty: true}
	require.NoError(t, db.Create(&movie).Error)
	tagRepo := database.NewMovieTagRepository(db)
	require.NoError(t, tagRepo.AddTag(t.Context(), movie.ContentID, "old-tag"))
	require.NoError(t, db.First(&movie, "content_id = ?", movie.ContentID).Error)

	fs := afero.NewMemMapFs()
	source := "/incoming/TAG-RACE-DISPLAY.mp4"
	dest := "/library/tag-race"
	require.NoError(t, fs.MkdirAll(filepath.Dir(source), 0o755))
	require.NoError(t, fs.MkdirAll(dest, 0o755))
	require.NoError(t, afero.WriteFile(fs, source, []byte("video"), 0o644))
	orch := pr260RealApply(fs, &movie, organizer.MediaFormatConfig{}, nil, true)
	orch.tagRepo = tagRepo
	orch.artifactPrepared = func() {
		require.NoError(t, tagRepo.AddTag(context.Background(), movie.ContentID, "new-tag"))
	}
	cmd := ApplyCmd{
		Movie: &movie, PersistedMovie: true,
		PublicationFence: db.Repositories().MovieRepo.(database.ApplyPublicationFencer),
		Match:            models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mp4", MovieID: movie.ID},
		DestPath:         dest, Organize: OrganizeOptions{Skip: true}, GenerateNFO: true,
		OperationMode: operationmode.OperationModeMetadataArtwork,
	}

	result, err := orch.Execute(context.Background(), cmd)
	require.ErrorIs(t, err, database.ErrApplyPublicationStale)
	require.NotNil(t, result)
	require.Equal(t, "artifact_publication", result.FailedStep)
	require.Empty(t, result.NFOPath)
	persisted, err := db.Repositories().MovieRepo.FindByContentID(t.Context(), movie.ContentID)
	require.NoError(t, err)
	require.True(t, persisted.RenderDirty)

	orch.artifactPrepared = nil
	cmd.Movie = persisted
	result, err = orch.Execute(context.Background(), cmd)
	require.NoError(t, err)
	require.NotEmpty(t, result.NFOPath)
	data, err := afero.ReadFile(fs, result.NFOPath)
	require.NoError(t, err)
	require.Contains(t, string(data), "<tag>old-tag</tag>")
	require.Contains(t, string(data), "<tag>new-tag</tag>")
	require.False(t, strings.Contains(string(data), "TAG-RACE-DISPLAY</tag>"))
	persisted, err = db.Repositories().MovieRepo.FindByContentID(t.Context(), movie.ContentID)
	require.NoError(t, err)
	require.False(t, persisted.RenderDirty)
}

func TestStepNFOReadsCanonicalAndLegacyTagsOnce(t *testing.T) {
	db, _ := pr260ArtifactDB(t)
	movie := models.Movie{ContentID: "nfo-tag-content", ID: "NFO-TAG-DISPLAY", Title: "NFO tags", RenderDirty: true}
	require.NoError(t, db.Create(&movie).Error)
	require.NoError(t, db.Create(&models.MovieTag{MovieID: movie.ContentID, Tag: "duplicate"}).Error)
	require.NoError(t, db.Create(&models.MovieTag{MovieID: movie.ID, Tag: "duplicate"}).Error)
	require.NoError(t, db.Create(&models.MovieTag{MovieID: movie.ID, Tag: "legacy-only"}).Error)

	fs := afero.NewMemMapFs()
	source := "/incoming/NFO-TAG-DISPLAY.mp4"
	dest := "/library/nfo-tags"
	require.NoError(t, fs.MkdirAll(filepath.Dir(source), 0o755))
	require.NoError(t, fs.MkdirAll(dest, 0o755))
	require.NoError(t, afero.WriteFile(fs, source, []byte("video"), 0o644))
	orch := pr260RealApply(fs, &movie, organizer.MediaFormatConfig{}, nil, true)
	orch.tagRepo = database.NewMovieTagRepository(db)
	result, err := orch.Execute(context.Background(), ApplyCmd{
		Movie: &movie, PersistedMovie: true,
		PublicationFence: db.Repositories().MovieRepo.(database.ApplyPublicationFencer),
		Match:            models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mp4", MovieID: movie.ID},
		DestPath:         dest, Organize: OrganizeOptions{Skip: true}, GenerateNFO: true,
		OperationMode: operationmode.OperationModeMetadataArtwork,
	})
	require.NoError(t, err)
	data, err := afero.ReadFile(fs, result.NFOPath)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(string(data), "<tag>duplicate</tag>"))
	require.Equal(t, 1, strings.Count(string(data), "<tag>legacy-only</tag>"))
}
