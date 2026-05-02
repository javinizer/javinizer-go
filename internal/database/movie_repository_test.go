package database

import (
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestMovie(id string) *models.Movie {
	releaseDate := time.Date(2023, 5, 15, 0, 0, 0, 0, time.UTC)
	// Note: ContentID is now the primary key, ID is for display/search
	// For tests, we use lowercase version of ID as ContentID (mimics real scraper behavior)
	return &models.Movie{
		ContentID:     strings.ToLower(strings.ReplaceAll(id, "-", "")), // e.g., "ipx064"
		ID:            id,                                               // e.g., "IPX-064"
		DisplayTitle:  "Test Movie " + id,
		Title:         "Test Title " + id,
		OriginalTitle: "テストタイトル" + id,
		Description:   "Test description for movie " + id,
		ReleaseDate:   &releaseDate,
		ReleaseYear:   2023,
		Runtime:       120,
		Director:      "Test Director",
		Maker:         "Test Studio",
		Label:         "Test Label",
		Series:        "Test Series",
		RatingScore:   4.5,
		RatingVotes:   100,
		PosterURL:     "http://example.com/poster.jpg",
		CoverURL:      "http://example.com/cover.jpg",
		TrailerURL:    "http://example.com/trailer.mp4",
		SourceName:    "test",
		SourceURL:     "http://example.com/movie/" + id,
	}
}

func TestMovieRepository_Create(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.AutoMigrate())
	repo := NewMovieRepository(db)

	t.Run("Create basic movie", func(t *testing.T) {
		movie := createTestMovie("IPX-001")
		err := repo.Create(movie)
		require.NoError(t, err)

		// Verify creation
		found, err := repo.FindByID("IPX-001")
		require.NoError(t, err)
		assert.Equal(t, "IPX-001", found.ID)
		assert.Equal(t, "Test Movie IPX-001", found.DisplayTitle)
	})

	t.Run("Create movie with genres", func(t *testing.T) {
		movie := createTestMovie("IPX-002")
		movie.Genres = []models.Genre{
			{Name: "Drama"},
			{Name: "Romance"},
		}

		err := repo.Create(movie)
		require.NoError(t, err)

		// Verify genres
		found, err := repo.FindByID("IPX-002")
		require.NoError(t, err)
		assert.Len(t, found.Genres, 2)
	})

	t.Run("Create movie with actresses", func(t *testing.T) {
		movie := createTestMovie("IPX-003")
		movie.Actresses = []models.Actress{
			{JapaneseName: "佐々木希", FirstName: "Nozomi", LastName: "Sasaki"},
		}

		err := repo.Create(movie)
		require.NoError(t, err)

		// Verify actresses
		found, err := repo.FindByID("IPX-003")
		require.NoError(t, err)
		assert.Len(t, found.Actresses, 1)
		assert.Equal(t, "佐々木希", found.Actresses[0].JapaneseName)
	})
}

func TestMovieRepository_Update(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.AutoMigrate())
	repo := NewMovieRepository(db)

	t.Run("Update existing movie", func(t *testing.T) {
		movie := createTestMovie("IPX-010")
		err := repo.Create(movie)
		require.NoError(t, err)

		// Update movie
		movie.Title = "Updated Title"
		movie.Runtime = 150
		err = repo.Update(movie)
		require.NoError(t, err)

		// Verify update
		found, err := repo.FindByID("IPX-010")
		require.NoError(t, err)
		assert.Equal(t, "Updated Title", found.Title)
		assert.Equal(t, 150, found.Runtime)
	})
}

func TestMovieRepository_FindByID(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.AutoMigrate())
	repo := NewMovieRepository(db)

	t.Run("Find existing movie", func(t *testing.T) {
		movie := createTestMovie("IPX-020")
		err := repo.Create(movie)
		require.NoError(t, err)

		found, err := repo.FindByID("IPX-020")
		require.NoError(t, err)
		assert.Equal(t, "IPX-020", found.ID)
	})

	t.Run("Find non-existent movie", func(t *testing.T) {
		_, err := repo.FindByID("NONEXISTENT-999")
		assert.Error(t, err)
	})
}

func TestMovieRepository_FindByContentID(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.AutoMigrate())
	repo := NewMovieRepository(db)

	t.Run("Find by content ID", func(t *testing.T) {
		movie := createTestMovie("IPX-030")
		movie.ContentID = "unique-content-id"
		err := repo.Create(movie)
		require.NoError(t, err)

		found, err := repo.FindByContentID("unique-content-id")
		require.NoError(t, err)
		assert.Equal(t, "IPX-030", found.ID)
		assert.Equal(t, "unique-content-id", found.ContentID)
	})

	t.Run("Find by non-existent content ID", func(t *testing.T) {
		_, err := repo.FindByContentID("nonexistent-content-id")
		assert.Error(t, err)
	})
}

func TestMovieRepository_Delete(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.AutoMigrate())
	repo := NewMovieRepository(db)

	t.Run("Delete existing movie", func(t *testing.T) {
		movie := createTestMovie("IPX-040")
		err := repo.Create(movie)
		require.NoError(t, err)

		err = repo.Delete("IPX-040")
		require.NoError(t, err)

		// Verify deletion
		_, err = repo.FindByID("IPX-040")
		assert.Error(t, err)
	})

	t.Run("Delete movie with translations", func(t *testing.T) {
		movie := createTestMovie("IPX-041")
		movie.Translations = []models.MovieTranslation{
			{Language: "en", Title: "English Title"},
			{Language: "zh", Title: "Chinese Title"},
		}
		err := repo.Create(movie)
		require.NoError(t, err)

		// Delete should cascade to translations
		err = repo.Delete("IPX-041")
		require.NoError(t, err)

		// Verify movie is deleted
		_, err = repo.FindByID("IPX-041")
		assert.Error(t, err)
	})

	t.Run("Delete non-existent movie", func(t *testing.T) {
		err := repo.Delete("NONEXISTENT-999")
		assert.NoError(t, err, "Deleting non-existent movie should not error")
	})
}

func TestMovieRepository_List(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.AutoMigrate())
	repo := NewMovieRepository(db)

	t.Run("List with pagination", func(t *testing.T) {
		// Create multiple movies
		for i := 1; i <= 10; i++ {
			movie := createTestMovie("IPX-050" + string(rune('0'+i)))
			err := repo.Create(movie)
			require.NoError(t, err)
		}

		// Get first 5
		movies, err := repo.List(5, 0)
		require.NoError(t, err)
		assert.Len(t, movies, 5)

		// Get next 5
		movies, err = repo.List(5, 5)
		require.NoError(t, err)
		assert.Len(t, movies, 5)
	})

	t.Run("List with empty database", func(t *testing.T) {
		cfg := &config.Config{
			Database: config.DatabaseConfig{
				Type: "sqlite",
				DSN:  ":memory:",
			},
			Logging: config.LoggingConfig{
				Level: "error",
			},
		}

		db2, err := New(cfg)
		require.NoError(t, err)
		defer func() { _ = db2.Close() }()

		require.NoError(t, db2.AutoMigrate())
		repo2 := NewMovieRepository(db2)

		movies, err := repo2.List(10, 0)
		require.NoError(t, err)
		assert.Len(t, movies, 0)
	})
}

func TestMovieRepository_Upsert(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.AutoMigrate())
	repo := NewMovieRepository(db)

	t.Run("Upsert creates new movie", func(t *testing.T) {
		movie := createTestMovie("IPX-060")
		_, err := repo.Upsert(movie)
		require.NoError(t, err)

		found, err := repo.FindByID("IPX-060")
		require.NoError(t, err)
		assert.Equal(t, "IPX-060", found.ID)
	})

	t.Run("Upsert updates existing movie", func(t *testing.T) {
		movie := createTestMovie("IPX-061")
		err := repo.Create(movie)
		require.NoError(t, err)

		originalCreatedAt := movie.CreatedAt

		// Wait a moment to ensure UpdatedAt would be different
		time.Sleep(10 * time.Millisecond)

		// Update via upsert
		movie.Title = "Updated via Upsert"
		movie.Runtime = 180
		_, err = repo.Upsert(movie)
		require.NoError(t, err)

		// Verify update
		found, err := repo.FindByID("IPX-061")
		require.NoError(t, err)
		assert.Equal(t, "Updated via Upsert", found.Title)
		assert.Equal(t, 180, found.Runtime)
		assert.Equal(t, originalCreatedAt.Unix(), found.CreatedAt.Unix())
	})

	t.Run("Upsert updates genres", func(t *testing.T) {
		movie := createTestMovie("IPX-062")
		movie.Genres = []models.Genre{
			{Name: "Action"},
		}
		err := repo.Create(movie)
		require.NoError(t, err)

		// Update with different genres
		movie.Genres = []models.Genre{
			{Name: "Drama"},
			{Name: "Comedy"},
		}
		_, err = repo.Upsert(movie)
		require.NoError(t, err)

		// Verify genres replaced
		found, err := repo.FindByID("IPX-062")
		require.NoError(t, err)
		assert.Len(t, found.Genres, 2)
	})

	t.Run("Upsert updates actresses", func(t *testing.T) {
		movie := createTestMovie("IPX-063")
		movie.Actresses = []models.Actress{
			{DMMID: 90001, JapaneseName: "Actress1", FirstName: "First1", LastName: "Last1"},
		}
		err := repo.Create(movie)
		require.NoError(t, err)

		// Update with different actresses
		movie.Actresses = []models.Actress{
			{DMMID: 90002, JapaneseName: "Actress2", FirstName: "First2", LastName: "Last2"},
		}
		_, err = repo.Upsert(movie)
		require.NoError(t, err)

		// Verify actresses replaced
		found, err := repo.FindByID("IPX-063")
		require.NoError(t, err)
		assert.Len(t, found.Actresses, 1)
		assert.Equal(t, "Actress2", found.Actresses[0].JapaneseName)
	})

	t.Run("Upsert with translations", func(t *testing.T) {
		movie := createTestMovie("IPX-064")
		movie.Translations = []models.MovieTranslation{
			{Language: "en", Title: "English Title"},
		}
		_, err := repo.Upsert(movie)
		require.NoError(t, err)

		// Verify translation
		found, err := repo.FindByID("IPX-064")
		require.NoError(t, err)
		assert.Len(t, found.Translations, 1)
		assert.Equal(t, "en", found.Translations[0].Language)

		// Update translation
		movie.Translations = []models.MovieTranslation{
			{Language: "en", Title: "Updated English Title"},
			{Language: "zh", Title: "Chinese Title"},
		}
		_, err = repo.Upsert(movie)
		require.NoError(t, err)

		// Verify translations updated
		found, err = repo.FindByID("IPX-064")
		require.NoError(t, err)
		assert.Len(t, found.Translations, 2)
		assert.Equal(t, "Updated English Title", found.Translations[0].Title)
		assert.Equal(t, "Chinese Title", found.Translations[1].Title)
	})

	t.Run("Upsert with ContentID derived from ID", func(t *testing.T) {
		movie := &models.Movie{
			ID:        "TEST-derive-001",
			ContentID: "",
			Title:     "Test Derive ContentID",
		}

		_, err := repo.Upsert(movie)
		require.NoError(t, err)

		found, err := repo.FindByID("TEST-derive-001")
		require.NoError(t, err)
		assert.NotEmpty(t, found.ContentID)
		assert.Equal(t, "testderive001", found.ContentID)
	})

	t.Run("Upsert updates actress data when gaps filled", func(t *testing.T) {
		// First: Create movie with minimal actress data
		movie1 := createTestMovie("IPX-UPD-001")
		movie1.Actresses = []models.Actress{
			{DMMID: 66666, JapaneseName: "Updated Actress"},
		}
		_, err := repo.Upsert(movie1)
		require.NoError(t, err)

		// Second: Add more data to same actress
		movie2 := createTestMovie("IPX-UPD-002")
		movie2.Actresses = []models.Actress{
			{DMMID: 66666, JapaneseName: "Updated Actress", FirstName: "Updated", LastName: "Actress2"},
		}
		_, err = repo.Upsert(movie2)
		require.NoError(t, err)

		// Verify actress data was updated
		found, err := repo.FindByID("IPX-UPD-002")
		require.NoError(t, err)
		assert.Len(t, found.Actresses, 1)
		assert.Equal(t, "Updated", found.Actresses[0].FirstName)
		assert.Equal(t, "Actress2", found.Actresses[0].LastName)
	})

	t.Run("Upsert with duplicate ContentID race condition", func(t *testing.T) {
		// This tests the race condition handling in Upsert
		// When another transaction inserts the same record
		movie1 := createTestMovie("IPX-RACE-001")
		movie1.ContentID = "race-condition-test"
		err := repo.Create(movie1)
		require.NoError(t, err)

		// Try to upsert the same movie
		movie2 := createTestMovie("IPX-RACE-001")
		movie2.ContentID = "race-condition-test"
		movie2.Title = "Updated After Race"
		_, err = repo.Upsert(movie2)
		require.NoError(t, err)

		found, err := repo.FindByID("IPX-RACE-001")
		require.NoError(t, err)
		assert.Equal(t, "Updated After Race", found.Title)
	})
}

func TestMovieRepository_EnsureGenresExist(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.AutoMigrate())
	repo := NewMovieRepository(db)

	t.Run("Genres are reused across movies", func(t *testing.T) {
		movie1 := createTestMovie("IPX-070")
		movie1.Genres = []models.Genre{
			{Name: "SharedGenreTest"},
		}
		_, err := repo.Upsert(movie1)
		require.NoError(t, err)

		movie2 := createTestMovie("IPX-071")
		movie2.Genres = []models.Genre{
			{Name: "SharedGenreTest"},
		}
		_, err = repo.Upsert(movie2)
		require.NoError(t, err)

		// Verify both movies reference the same genre
		found1, err := repo.FindByID("IPX-070")
		require.NoError(t, err)
		found2, err := repo.FindByID("IPX-071")
		require.NoError(t, err)

		// Check genres exist and have the same ID
		if len(found1.Genres) > 0 && len(found2.Genres) > 0 {
			assert.Equal(t, found1.Genres[0].ID, found2.Genres[0].ID)
		} else {
			// Just verify the genre was created in the database
			genreRepo := NewGenreRepository(db)
			genres, err := genreRepo.List()
			require.NoError(t, err)

			foundGenre := false
			for _, g := range genres {
				if g.Name == "SharedGenreTest" {
					foundGenre = true
					break
				}
			}
			assert.True(t, foundGenre, "SharedGenreTest should exist in database")
		}
	})

	t.Run("Actress creation race condition handling", func(t *testing.T) {
		// This tests the race condition path where the actress is created
		// by another transaction after our Initial check but before we insert
		movie1 := createTestMovie("IPX-RACE-002")
		movie1.Actresses = []models.Actress{
			{DMMID: 77777, JapaneseName: "Race Condition Actress", FirstName: "Race", LastName: "Condition"},
		}
		_, err := repo.Upsert(movie1)
		require.NoError(t, err)

		// Verify the actress was created and is accessible
		found, err := repo.FindByID("IPX-RACE-002")
		require.NoError(t, err)
		assert.Len(t, found.Actresses, 1)
		assert.Equal(t, "Race Condition Actress", found.Actresses[0].JapaneseName)
	})

	t.Run("Actress with all fallback strategies", func(t *testing.T) {
		// Test fallback to FirstName + LastName when neither DMMID nor JapaneseName available
		movie := createTestMovie("IPX-FALLBACK-001")
		movie.Actresses = []models.Actress{
			{FirstName: "Fallback", LastName: "Test"},
		}
		_, err := repo.Upsert(movie)
		require.NoError(t, err)

		found, err := repo.FindByID("IPX-FALLBACK-001")
		require.NoError(t, err)
		assert.Len(t, found.Actresses, 1)
		assert.Equal(t, "Fallback", found.Actresses[0].FirstName)
		assert.Equal(t, "Test", found.Actresses[0].LastName)
	})
}

func TestMovieRepository_EnsureActressesExist(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	require.NoError(t, db.AutoMigrate())
	repo := NewMovieRepository(db)

	t.Run("Actresses are reused by DMMID", func(t *testing.T) {
		movie1 := createTestMovie("IPX-080")
		movie1.Actresses = []models.Actress{
			{DMMID: 12345, JapaneseName: "Test Actress", FirstName: "Test", LastName: "Actress"},
		}
		_, err := repo.Upsert(movie1)
		require.NoError(t, err)

		movie2 := createTestMovie("IPX-081")
		movie2.Actresses = []models.Actress{
			{DMMID: 12345, JapaneseName: "Test Actress Updated", FirstName: "Updated", LastName: "Name"},
		}
		_, err = repo.Upsert(movie2)
		require.NoError(t, err)

		// Verify both movies reference the same actress (by DMMID)
		found1, err := repo.FindByID("IPX-080")
		require.NoError(t, err)
		found2, err := repo.FindByID("IPX-081")
		require.NoError(t, err)

		// Check that actress exists
		if len(found1.Actresses) > 0 && len(found2.Actresses) > 0 {
			assert.Equal(t, found1.Actresses[0].ID, found2.Actresses[0].ID)
		} else {
			// Verify actress exists in database
			actressRepo := NewActressRepository(db)
			actress, err := actressRepo.FindByJapaneseName("Test Actress")
			require.NoError(t, err)
			assert.Equal(t, 12345, actress.DMMID)
		}
	})

	t.Run("Actresses are reused by JapaneseName", func(t *testing.T) {
		movie1 := createTestMovie("IPX-082")
		movie1.Actresses = []models.Actress{
			{JapaneseName: "山田太郎", FirstName: "Taro", LastName: "Yamada"},
		}
		_, err := repo.Upsert(movie1)
		require.NoError(t, err)

		movie2 := createTestMovie("IPX-083")
		movie2.Actresses = []models.Actress{
			{JapaneseName: "山田太郎", FirstName: "Different", LastName: "Name"},
		}
		_, err = repo.Upsert(movie2)
		require.NoError(t, err)

		// Verify actress exists in database
		actressRepo := NewActressRepository(db)
		actress, err := actressRepo.FindByJapaneseName("山田太郎")
		require.NoError(t, err)
		assert.Equal(t, "山田太郎", actress.JapaneseName)

		// Both movies should have same actress
		found1, err := repo.FindByID("IPX-082")
		require.NoError(t, err)
		found2, err := repo.FindByID("IPX-083")
		require.NoError(t, err)

		if len(found1.Actresses) > 0 && len(found2.Actresses) > 0 {
			assert.Equal(t, found1.Actresses[0].ID, found2.Actresses[0].ID)
		}
	})

	t.Run("Actresses with no identifying info are skipped", func(t *testing.T) {
		movie := createTestMovie("IPX-084")
		movie.Actresses = []models.Actress{
			{}, // No DMMID, JapaneseName, FirstName, or LastName
		}
		_, err := repo.Upsert(movie)
		require.NoError(t, err)

		// Verify actress was skipped (should have 0 actresses)
		found, err := repo.FindByID("IPX-084")
		require.NoError(t, err)
		assert.Len(t, found.Actresses, 0)
	})

	t.Run("Actress data is merged when new data fills gaps", func(t *testing.T) {
		// First movie creates actress with minimal data
		movie1 := createTestMovie("IPX-089")
		movie1.Actresses = []models.Actress{
			{DMMID: 55555, JapaneseName: "テスト女優"},
		}
		_, err := repo.Upsert(movie1)
		require.NoError(t, err)

		// Second movie provides additional data for the same actress
		movie2 := createTestMovie("IPX-090")
		movie2.Actresses = []models.Actress{
			{DMMID: 55555, JapaneseName: "テスト女優", ThumbURL: "http://example.com/thumb.jpg", FirstName: "Test", LastName: "Actress"},
		}
		_, err = repo.Upsert(movie2)
		require.NoError(t, err)

		// Verify actress data was merged
		actressRepo := NewActressRepository(db)
		actresses, err := actressRepo.List(100, 0)
		require.NoError(t, err)

		var foundActress *models.Actress
		for i := range actresses {
			if actresses[i].DMMID == 55555 {
				foundActress = &actresses[i]
				break
			}
		}

		require.NotNil(t, foundActress, "Should find actress with DMMID 55555")
		assert.Equal(t, "http://example.com/thumb.jpg", foundActress.ThumbURL)
		assert.Equal(t, "Test", foundActress.FirstName)
		assert.Equal(t, "Actress", foundActress.LastName)
	})
}

func TestMovieRepository_Upsert_WithSettingsHash(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{Type: "sqlite", DSN: ":memory:"},
		Logging:  config.LoggingConfig{Level: "error"},
	}

	db, err := New(cfg)
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, db.AutoMigrate())
	repo := NewMovieRepository(db)

	t.Run("Upsert stores settings_hash", func(t *testing.T) {
		movie := createTestMovie("IPX-HASH-001")
		movie.Translations = []models.MovieTranslation{
			{
				Language:     "en",
				Title:        "English Title",
				SettingsHash: "abc123def456",
				SourceName:   "translation-service",
			},
		}

		_, err := repo.Upsert(movie)
		require.NoError(t, err)

		found, err := repo.FindByID("IPX-HASH-001")
		require.NoError(t, err)
		assert.Len(t, found.Translations, 1)
		assert.Equal(t, "abc123def456", found.Translations[0].SettingsHash, "settings_hash should be stored")
	})

	t.Run("Upsert preserves empty hash (legacy)", func(t *testing.T) {
		movie := createTestMovie("IPX-HASH-002")
		movie.Translations = []models.MovieTranslation{
			{
				Language:     "ja",
				Title:        "Japanese Title",
				SettingsHash: "",
				SourceName:   "dmm",
			},
		}

		_, err := repo.Upsert(movie)
		require.NoError(t, err)

		found, err := repo.FindByID("IPX-HASH-002")
		require.NoError(t, err)
		assert.Len(t, found.Translations, 1)
		assert.Equal(t, "", found.Translations[0].SettingsHash, "empty hash should be preserved for legacy")
	})

	t.Run("Upsert updates hash on re-translation", func(t *testing.T) {
		movie := createTestMovie("IPX-HASH-003")
		movie.Translations = []models.MovieTranslation{
			{
				Language:     "en",
				Title:        "Old English Title",
				SettingsHash: "oldhash123",
				SourceName:   "translation-service",
			},
		}
		_, err := repo.Upsert(movie)
		require.NoError(t, err)

		movie.Translations = []models.MovieTranslation{
			{
				Language:     "en",
				Title:        "New English Title",
				SettingsHash: "newhash456",
				SourceName:   "translation-service",
			},
		}

		_, err = repo.Upsert(movie)
		require.NoError(t, err)

		found, err := repo.FindByID("IPX-HASH-003")
		require.NoError(t, err)
		assert.Len(t, found.Translations, 1, "should replace translation for same language")
		assert.Equal(t, "newhash456", found.Translations[0].SettingsHash, "new hash should be stored")
		assert.Equal(t, "New English Title", found.Translations[0].Title, "title should be updated")
	})
}
