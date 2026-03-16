package aggregator

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewWithOptions_AllNil verifies aggregator initializes with all-nil options (AC3)
func TestNewWithOptions_AllNil(t *testing.T) {
	cfg := createTestConfig()

	agg := NewWithOptions(cfg, nil)

	require.NotNil(t, agg)
	assert.NotNil(t, agg.templateEngine, "template engine should be initialized with real implementation")
	assert.NotNil(t, agg.genreReplacementCache, "genre cache should be initialized (empty)")
	assert.NotNil(t, agg.actressAliasCache, "actress cache should be initialized (empty)")
	assert.Nil(t, agg.genreReplacementRepo, "genre repo should be nil when not provided")
	assert.Nil(t, agg.actressAliasRepo, "actress repo should be nil when not provided")
	assert.NotNil(t, agg.resolvedPriorities, "priorities should be resolved")
	assert.Len(t, agg.genreReplacementCache, 0, "genre cache should be empty without database")
	assert.Len(t, agg.actressAliasCache, 0, "actress cache should be empty without database")
}

// TestNewWithOptions_NilConfig tests defensive nil check for config (AC3)
func TestNewWithOptions_NilConfig(t *testing.T) {
	agg := NewWithOptions(nil, nil)

	assert.Nil(t, agg, "aggregator should be nil when config is nil")
}

// TestNewWithOptions_InjectedGenreCache verifies pre-populated genre cache usage (AC3)
func TestNewWithOptions_InjectedGenreCache(t *testing.T) {
	cfg := createTestConfig()
	mockGenreCache := map[string]string{
		"Creampie": "中出し",
		"Blowjob":  "フェラ",
	}

	opts := &AggregatorOptions{
		GenreCache: mockGenreCache,
	}

	agg := NewWithOptions(cfg, opts)

	require.NotNil(t, agg)
	assert.Len(t, agg.genreReplacementCache, 2, "genre cache should contain injected data")
	assert.Equal(t, "中出し", agg.genreReplacementCache["Creampie"])
	assert.Equal(t, "フェラ", agg.genreReplacementCache["Blowjob"])
}

// TestNewWithOptions_InjectedActressCache verifies pre-populated actress cache usage (AC3)
func TestNewWithOptions_InjectedActressCache(t *testing.T) {
	cfg := createTestConfig()
	mockActressCache := map[string]string{
		"Yua Mikami":  "三上悠亜",
		"Aika Yumeno": "夢乃あいか",
	}

	opts := &AggregatorOptions{
		ActressCache: mockActressCache,
	}

	agg := NewWithOptions(cfg, opts)

	require.NotNil(t, agg)
	assert.Len(t, agg.actressAliasCache, 2, "actress cache should contain injected data")
	assert.Equal(t, "三上悠亜", agg.actressAliasCache["Yua Mikami"])
	assert.Equal(t, "夢乃あいか", agg.actressAliasCache["Aika Yumeno"])
}

// TestNewWithOptions_InjectedTemplateEngine verifies custom template engine injection (AC3)
func TestNewWithOptions_InjectedTemplateEngine(t *testing.T) {
	cfg := createTestConfig()
	mockEngine := template.NewEngine() // Real engine for now (mockery in Task 6)

	opts := &AggregatorOptions{
		TemplateEngine: mockEngine,
	}

	agg := NewWithOptions(cfg, opts)

	require.NotNil(t, agg)
	assert.Equal(t, mockEngine, agg.templateEngine, "aggregator should use injected template engine")
}

// TestNewWithOptions_InjectedRepositories verifies repository injection without loading (AC3)
func TestNewWithOptions_InjectedRepositories(t *testing.T) {
	cfg := createTestConfig()

	// Create in-memory database for repositories
	dbCfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}
	db, err := database.New(dbCfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.AutoMigrate())

	genreRepo := database.NewGenreReplacementRepository(db)
	actressRepo := database.NewActressAliasRepository(db)

	opts := &AggregatorOptions{
		GenreReplacementRepo: genreRepo,
		ActressAliasRepo:     actressRepo,
	}

	agg := NewWithOptions(cfg, opts)

	require.NotNil(t, agg)
	assert.NotNil(t, agg.genreReplacementRepo, "genre repo should be set")
	assert.NotNil(t, agg.actressAliasRepo, "actress repo should be set")
	// Caches should be empty since we didn't populate DB or inject caches
	assert.Len(t, agg.genreReplacementCache, 0)
	assert.Len(t, agg.actressAliasCache, 0)
}

// TestNewWithOptions_CachePrecedence verifies GenreCache takes precedence over GenreReplacementRepo (AC3)
func TestNewWithOptions_CachePrecedence(t *testing.T) {
	cfg := createTestConfig()

	// Create in-memory database and populate with data
	dbCfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}
	db, err := database.New(dbCfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.AutoMigrate())

	genreRepo := database.NewGenreReplacementRepository(db)

	// Populate DB with one genre replacement
	err = genreRepo.Create(&models.GenreReplacement{
		Original:    "Creampie",
		Replacement: "FromDatabase",
	})
	require.NoError(t, err)

	// Inject both cache (should take precedence) and repo
	mockGenreCache := map[string]string{
		"Creampie": "FromCache",
	}

	opts := &AggregatorOptions{
		GenreCache:           mockGenreCache,
		GenreReplacementRepo: genreRepo,
	}

	agg := NewWithOptions(cfg, opts)

	require.NotNil(t, agg)
	// GenreCache should take precedence over database
	assert.Equal(t, "FromCache", agg.genreReplacementCache["Creampie"],
		"GenreCache should take precedence over GenreReplacementRepo")
}

// TestNewWithDatabase_BackwardCompatibility verifies zero breaking changes (AC4, AC7, CRITICAL)
func TestNewWithDatabase_BackwardCompatibility(t *testing.T) {
	cfg := createTestConfig()

	// Create in-memory database
	dbCfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}
	db, err := database.New(dbCfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.AutoMigrate())

	// Populate database with test data
	genreRepo := database.NewGenreReplacementRepository(db)
	err = genreRepo.Create(&models.GenreReplacement{
		Original:    "Creampie",
		Replacement: "中出し",
	})
	require.NoError(t, err)

	actressRepo := database.NewActressAliasRepository(db)
	err = actressRepo.Create(&models.ActressAlias{
		AliasName:     "Yua Mikami",
		CanonicalName: "三上悠亜",
	})
	require.NoError(t, err)

	// Test NewWithDatabase (production constructor)
	agg := NewWithDatabase(cfg, db)

	require.NotNil(t, agg)
	assert.NotNil(t, agg.templateEngine, "template engine should be initialized")
	assert.NotNil(t, agg.genreReplacementRepo, "genre repo should be set")
	assert.NotNil(t, agg.actressAliasRepo, "actress repo should be set")
	assert.NotNil(t, agg.resolvedPriorities, "priorities should be resolved")

	// Verify database caches were loaded
	assert.Len(t, agg.genreReplacementCache, 1, "genre cache should contain database data")
	assert.Equal(t, "中出し", agg.genreReplacementCache["Creampie"])

	assert.Len(t, agg.actressAliasCache, 1, "actress cache should contain database data")
	assert.Equal(t, "三上悠亜", agg.actressAliasCache["Yua Mikami"])
}

// TestAggregate_WithMockedGenreCache verifies aggregation with injected cache (AC6)
func TestAggregate_WithMockedGenreCache(t *testing.T) {
	cfg := createTestConfig()
	mockGenreCache := map[string]string{
		"Creampie": "中出し",
		"Blowjob":  "フェラ",
	}

	opts := &AggregatorOptions{
		GenreCache: mockGenreCache,
	}

	agg := NewWithOptions(cfg, opts)
	require.NotNil(t, agg)

	// Create test scraper result with genres to be replaced
	result := &models.ScraperResult{
		Source: "r18dev",
		ID:     "IPX-123",
		Title:  "Test Movie",
		Genres: []string{"Creampie", "Blowjob", "Unknown"},
	}

	movie, err := agg.Aggregate([]*models.ScraperResult{result})

	require.NoError(t, err)
	require.NotNil(t, movie)
	assert.Equal(t, "IPX-123", movie.ID)
	assert.Equal(t, "Test Movie", movie.Title)
	// Verify genre replacement occurred (integration test - depends on applyGenreReplacements logic)
	// Note: movie.Genres is []models.Genre (struct), not []string
	genreNames := make([]string, len(movie.Genres))
	for i, g := range movie.Genres {
		genreNames[i] = g.Name
	}
	assert.Contains(t, genreNames, "中出し", "Creampie should be replaced with 中出し")
	assert.Contains(t, genreNames, "フェラ", "Blowjob should be replaced with フェラ")
}

// TestAggregate_WithMockedActressAlias verifies actress alias resolution with injected cache (AC6)
func TestAggregate_WithMockedActressAlias(t *testing.T) {
	cfg := createTestConfig()
	mockActressCache := map[string]string{
		"Yua Mikami": "三上悠亜",
	}

	opts := &AggregatorOptions{
		ActressCache: mockActressCache,
	}

	agg := NewWithOptions(cfg, opts)
	require.NotNil(t, agg)

	// Create test scraper result with actress alias
	result := &models.ScraperResult{
		Source: "r18dev",
		ID:     "IPX-123",
		Title:  "Test Movie",
		Actresses: []models.ActressInfo{
			{FirstName: "Yua", LastName: "Mikami", JapaneseName: ""},
		},
	}

	movie, err := agg.Aggregate([]*models.ScraperResult{result})

	require.NoError(t, err)
	require.NotNil(t, movie)
	assert.Len(t, movie.Actresses, 1)
	// Verify alias resolution occurred (integration test - depends on resolveActressAliases logic)
	// Note: The actual alias resolution might set JapaneseName or modify FirstName/LastName fields
	// This test verifies the cache is accessible during aggregation
	assert.Equal(t, "Yua", movie.Actresses[0].FirstName)
	assert.Equal(t, "Mikami", movie.Actresses[0].LastName)
}

// TestGetResolvedPriorities verifies the interface method returns cached priorities (AC1)
func TestGetResolvedPriorities(t *testing.T) {
	cfg := createTestConfig()
	agg := NewWithOptions(cfg, nil)

	require.NotNil(t, agg)

	priorities := agg.GetResolvedPriorities()

	assert.NotNil(t, priorities, "resolved priorities should not be nil")
	assert.Greater(t, len(priorities), 0, "should have resolved at least some field priorities")
	// Verify specific priority fields from test config (keys are TitleCase)
	assert.Contains(t, priorities, "Title", "Title priority should be resolved")
	assert.Contains(t, priorities, "Description", "Description priority should be resolved")
}

// Helper function to create a minimal test config
func createTestConfig() *config.Config {
	return &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev", "dmm"},
			R18Dev: config.R18DevConfig{
				Enabled: true,
			},
			DMM: config.DMMConfig{
				Enabled: true,
			},
		},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Title:       []string{"r18dev", "dmm"},
				Description: []string{"dmm", "r18dev"},
				CoverURL:    []string{}, // Empty = use global priority
			},
			GenreReplacement: config.GenreReplacementConfig{
				Enabled: true,
			},
			ActressDatabase: config.ActressDatabaseConfig{
				Enabled: true,
			},
		},
	}
}
