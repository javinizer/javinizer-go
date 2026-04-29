package worker

import (
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newActressEnrichTestDB(t *testing.T) *database.ActressRepository {
	t.Helper()
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  filepath.Join(t.TempDir(), "actress-enrich.db"),
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}
	db, err := database.New(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.AutoMigrate())
	return database.NewActressRepository(db)
}

func TestEnrichActressesFromDB(t *testing.T) {
	actressRepo := newActressEnrichTestDB(t)

	dbActress1 := &models.Actress{
		DMMID:        10001,
		FirstName:    "Yui",
		LastName:     "Hatano",
		JapaneseName: "波多野結衣",
		ThumbURL:     "http://example.com/yui.jpg",
	}
	dbActress2 := &models.Actress{
		DMMID:        10002,
		FirstName:    "Ai",
		LastName:     "Uehara",
		JapaneseName: "上原亜衣",
		ThumbURL:     "http://example.com/ai.jpg",
	}
	require.NoError(t, actressRepo.Create(dbActress1))
	require.NoError(t, actressRepo.Create(dbActress2))

	enabledCfg := &config.Config{
		Metadata: config.MetadataConfig{
			ActressDatabase: config.ActressDatabaseConfig{
				Enabled: true,
			},
		},
	}

	disabledCfg := &config.Config{
		Metadata: config.MetadataConfig{
			ActressDatabase: config.ActressDatabaseConfig{
				Enabled: false,
			},
		},
	}

	t.Run("enriches by DMMID", func(t *testing.T) {
		movie := &models.Movie{
			ID: "TEST-001",
			Actresses: []models.Actress{
				{DMMID: 10001},
			},
		}
		enriched := EnrichActressesFromDB(movie, actressRepo, enabledCfg)
		assert.Equal(t, 1, enriched)
		assert.Equal(t, "波多野結衣", movie.Actresses[0].JapaneseName)
		assert.Equal(t, "http://example.com/yui.jpg", movie.Actresses[0].ThumbURL)
	})

	t.Run("enriches by JapaneseName", func(t *testing.T) {
		movie := &models.Movie{
			ID: "TEST-002",
			Actresses: []models.Actress{
				{JapaneseName: "上原亜衣"},
			},
		}
		enriched := EnrichActressesFromDB(movie, actressRepo, enabledCfg)
		assert.Equal(t, 1, enriched)
		assert.Equal(t, "http://example.com/ai.jpg", movie.Actresses[0].ThumbURL)
		assert.Equal(t, "Ai", movie.Actresses[0].FirstName)
	})

	t.Run("enriches by FirstName+LastName", func(t *testing.T) {
		movie := &models.Movie{
			ID: "TEST-003",
			Actresses: []models.Actress{
				{FirstName: "Yui", LastName: "Hatano"},
			},
		}
		enriched := EnrichActressesFromDB(movie, actressRepo, enabledCfg)
		assert.Equal(t, 1, enriched)
		assert.Equal(t, "http://example.com/yui.jpg", movie.Actresses[0].ThumbURL)
		assert.Equal(t, "波多野結衣", movie.Actresses[0].JapaneseName)
	})

	t.Run("does not overwrite existing fields", func(t *testing.T) {
		movie := &models.Movie{
			ID: "TEST-004",
			Actresses: []models.Actress{
				{
					DMMID:        10001,
					ThumbURL:     "http://existing.com/thumb.jpg",
					FirstName:    "Existing",
					LastName:     "Name",
					JapaneseName: "ExistingName",
				},
			},
		}
		enriched := EnrichActressesFromDB(movie, actressRepo, enabledCfg)
		assert.Equal(t, 0, enriched)
		assert.Equal(t, "http://existing.com/thumb.jpg", movie.Actresses[0].ThumbURL)
		assert.Equal(t, "Existing", movie.Actresses[0].FirstName)
	})

	t.Run("returns early when disabled", func(t *testing.T) {
		movie := &models.Movie{
			ID: "TEST-005",
			Actresses: []models.Actress{
				{DMMID: 10001},
			},
		}
		enriched := EnrichActressesFromDB(movie, actressRepo, disabledCfg)
		assert.Equal(t, 0, enriched)
		assert.Empty(t, movie.Actresses[0].ThumbURL)
	})

	t.Run("no match leaves actress unchanged", func(t *testing.T) {
		movie := &models.Movie{
			ID: "TEST-006",
			Actresses: []models.Actress{
				{DMMID: 99999, FirstName: "Unknown"},
			},
		}
		enriched := EnrichActressesFromDB(movie, actressRepo, enabledCfg)
		assert.Equal(t, 0, enriched)
		assert.Empty(t, movie.Actresses[0].ThumbURL)
		assert.Equal(t, "Unknown", movie.Actresses[0].FirstName)
	})

	t.Run("nil movie returns zero", func(t *testing.T) {
		enriched := EnrichActressesFromDB(nil, actressRepo, enabledCfg)
		assert.Equal(t, 0, enriched)
	})

	t.Run("nil actressRepo returns zero", func(t *testing.T) {
		movie := &models.Movie{ID: "TEST-007"}
		enriched := EnrichActressesFromDB(movie, nil, enabledCfg)
		assert.Equal(t, 0, enriched)
	})

	t.Run("nil config returns zero", func(t *testing.T) {
		movie := &models.Movie{ID: "TEST-008"}
		enriched := EnrichActressesFromDB(movie, actressRepo, nil)
		assert.Equal(t, 0, enriched)
	})

	t.Run("DMMID lookup takes priority over JapaneseName", func(t *testing.T) {
		movie := &models.Movie{
			ID: "TEST-009",
			Actresses: []models.Actress{
				{DMMID: 10001},
			},
		}
		enriched := EnrichActressesFromDB(movie, actressRepo, enabledCfg)
		assert.Equal(t, 1, enriched)
		assert.Equal(t, "波多野結衣", movie.Actresses[0].JapaneseName)
		assert.Equal(t, "Yui", movie.Actresses[0].FirstName)
		assert.Equal(t, "http://example.com/yui.jpg", movie.Actresses[0].ThumbURL)
	})

	t.Run("enriches multiple actresses", func(t *testing.T) {
		movie := &models.Movie{
			ID: "TEST-010",
			Actresses: []models.Actress{
				{DMMID: 10001},
				{DMMID: 10002},
			},
		}
		enriched := EnrichActressesFromDB(movie, actressRepo, enabledCfg)
		assert.Equal(t, 2, enriched)
		assert.Equal(t, "http://example.com/yui.jpg", movie.Actresses[0].ThumbURL)
		assert.Equal(t, "http://example.com/ai.jpg", movie.Actresses[1].ThumbURL)
	})

	t.Run("falls through to JapaneseName when DMMID lookup fails", func(t *testing.T) {
		movie := &models.Movie{
			ID: "TEST-011",
			Actresses: []models.Actress{
				{DMMID: 55555, JapaneseName: "波多野結衣"},
			},
		}
		enriched := EnrichActressesFromDB(movie, actressRepo, enabledCfg)
		assert.Equal(t, 1, enriched)
		assert.Equal(t, "http://example.com/yui.jpg", movie.Actresses[0].ThumbURL)
	})

	t.Run("second enrichment does not overwrite NFO-merged fields", func(t *testing.T) {
		movie := &models.Movie{
			ID: "TEST-012",
			Actresses: []models.Actress{
				{DMMID: 10001},
			},
		}
		enriched := EnrichActressesFromDB(movie, actressRepo, enabledCfg)
		assert.Equal(t, 1, enriched)
		assert.Equal(t, "http://example.com/yui.jpg", movie.Actresses[0].ThumbURL)
		movie.Actresses[0].ThumbURL = "http://nfo-merged.com/thumb.jpg"
		enriched = EnrichActressesFromDB(movie, actressRepo, enabledCfg)
		assert.Equal(t, 0, enriched)
		assert.Equal(t, "http://nfo-merged.com/thumb.jpg", movie.Actresses[0].ThumbURL)
	})
}
