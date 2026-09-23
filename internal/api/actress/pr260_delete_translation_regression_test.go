package actress

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestRegisteredDeleteActressRemovesTranslationsWithForeignKeysOnOrOff(t *testing.T) {
	for _, foreignKeys := range []bool{false, true} {
		t.Run(map[bool]string{false: "foreign keys off", true: "foreign keys on"}[foreignKeys], func(t *testing.T) {
			dsn := filepath.Join(t.TempDir(), "api-delete.db") + map[bool]string{false: "?_foreign_keys=0", true: "?_foreign_keys=1"}[foreignKeys]
			db, err := database.New(&database.Config{Type: "sqlite", DSN: dsn, LogLevel: "silent"})
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
			repos := db.Repositories()

			actress := models.Actress{JapaneseName: "API Delete", Verified: true, Origin: database.ActressOriginUser}
			require.NoError(t, db.Create(&actress).Error)
			require.NoError(t, db.Create(&models.ActressTranslation{ActressID: actress.ID, Language: "en", DisplayName: "API Delete"}).Error)
			movie := models.Movie{ContentID: "api-legacy-delete", ID: "API-LEGACY-DELETE", RenderGeneration: 5}
			require.NoError(t, db.Create(&movie).Error)
			require.NoError(t, db.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?)", movie.ContentID, actress.ID).Error)

			gin.SetMode(gin.TestMode)
			router := gin.New()
			RegisterRoutes(router.Group("/api/v1"), NewActressDeps(repos.ContentRepos, repos.TranslationRepos))
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodDelete, "/api/v1/actresses/"+itoa(actress.ID), nil)
			router.ServeHTTP(response, request)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())

			for table, predicate := range map[string]string{"actresses": "id = ?", "actress_translations": "actress_id = ?", "movie_actresses": "actress_id = ?"} {
				var count int64
				require.NoError(t, db.Table(table).Where(predicate, actress.ID).Count(&count).Error)
				require.Zero(t, count, table)
			}
			var stored models.Movie
			require.NoError(t, db.First(&stored, "content_id = ?", movie.ContentID).Error)
			require.True(t, stored.RenderDirty)
			require.EqualValues(t, 6, stored.RenderGeneration)
			called := false
			require.ErrorIs(t, database.NewMovieRepository(db).WithApplyArtifactPublicationFence(context.Background(), movie.ContentID, 5, func(*models.Movie) error { called = true; return nil }), database.ErrApplyPublicationStale)
			require.False(t, called)
			require.NoError(t, database.NewMovieRepository(db).WithApplyArtifactPublicationFence(context.Background(), movie.ContentID, 6, func(*models.Movie) error { called = true; return nil }))
			require.True(t, called)
		})
	}
}

func TestRegisteredMergeInvalidatesLegacyOnlyProjectionWithForeignKeysOnOrOff(t *testing.T) {
	for _, foreignKeys := range []bool{false, true} {
		t.Run(map[bool]string{false: "foreign keys off", true: "foreign keys on"}[foreignKeys], func(t *testing.T) {
			dsn := filepath.Join(t.TempDir(), "api-merge.db") + map[bool]string{false: "?_foreign_keys=0", true: "?_foreign_keys=1"}[foreignKeys]
			db, err := database.New(&database.Config{Type: "sqlite", DSN: dsn, LogLevel: "silent"})
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
			target := models.Actress{FirstName: "Target", Verified: true, Origin: database.ActressOriginUser}
			source := models.Actress{JapaneseName: "API Legacy Source", Verified: true, Origin: database.ActressOriginUser}
			require.NoError(t, db.Create(&target).Error)
			require.NoError(t, db.Create(&source).Error)
			movie := models.Movie{ContentID: "api-legacy-merge", ID: "API-LEGACY-MERGE", RenderGeneration: 9}
			require.NoError(t, db.Create(&movie).Error)
			require.NoError(t, db.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?)", movie.ContentID, source.ID).Error)

			response := postRegisteredMerge(t, database.NewActressRepository(db), target.ID, source.ID, nil)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			var stored models.Movie
			require.NoError(t, db.First(&stored, "content_id = ?", movie.ContentID).Error)
			require.True(t, stored.RenderDirty)
			require.EqualValues(t, 10, stored.RenderGeneration)
			var ids []uint
			require.NoError(t, db.Table("movie_actresses").Where("movie_content_id = ?", movie.ContentID).Pluck("actress_id", &ids).Error)
			require.Equal(t, []uint{target.ID}, ids)
		})
	}
}
