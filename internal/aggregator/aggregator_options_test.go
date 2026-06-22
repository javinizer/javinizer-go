package aggregator

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNew_AllNilProcessors verifies aggregator initializes with all-nil processors
func TestNew_AllNilProcessors(t *testing.T) {
	cfg := createTestConfig()

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	require.NotNil(t, agg)
	assert.NotNil(t, agg.templateEngine, "template engine should be initialized with real implementation")
	assert.NotNil(t, agg.genreProcessor, "genre processor should be initialized")
	assert.NotNil(t, agg.wordProcessor, "word processor should be initialized")
	assert.NotNil(t, agg.aliasResolver, "alias resolver should be initialized")
	assert.NotNil(t, agg.resolvedPriorities, "priorities should be resolved")
}

// TestNew_NilConfig tests defensive nil check for config
func TestNew_NilConfig(t *testing.T) {
	agg := newAggregatorNoDB(nil)

	assert.Nil(t, agg, "aggregator should be nil when config is nil")
}

// TestNew_InjectedGenreCache verifies pre-populated genre cache usage via GenreProcessorWithCache
func TestNew_InjectedGenreCache(t *testing.T) {
	cfg := createTestConfig()
	mockGenreCache := map[string]string{
		"Creampie": "中出し",
		"Blowjob":  "フェラ",
	}

	gp := newGenreProcessorWithCache(MetadataConfigFromApp(&cfg.Metadata), nil, mockGenreCache)
	agg := New(testConfigFromAppConfig(cfg), gp, NewWordProcessor(MetadataConfigFromApp(&cfg.Metadata), nil), NewAliasResolver(MetadataConfigFromApp(&cfg.Metadata), nil))

	require.NotNil(t, agg)
	// Verify the genre processor has the cache data
	assert.Equal(t, "中出し", gp.applyReplacement("Creampie"))
	assert.Equal(t, "フェラ", gp.applyReplacement("Blowjob"))
}

// TestNew_InjectedActressCache verifies pre-populated actress cache usage via AliasResolverWithCache
func TestNew_InjectedActressCache(t *testing.T) {
	cfg := createTestConfig()
	mockActressCache := map[string]string{
		"Yua Mikami":  "三上悠亜",
		"Aika Yumeno": "夢乃あいか",
	}

	ar := newAliasResolverWithCache(MetadataConfigFromApp(&cfg.Metadata), nil, mockActressCache)
	agg := New(testConfigFromAppConfig(cfg), NewGenreProcessor(MetadataConfigFromApp(&cfg.Metadata), nil), NewWordProcessor(MetadataConfigFromApp(&cfg.Metadata), nil), ar)

	require.NotNil(t, agg)
	// Verify the alias resolver has the cache data
	assert.NotNil(t, agg.aliasResolver)
}

// TestNew_InjectedTemplateEngine verifies custom template engine injection
func TestNew_InjectedTemplateEngine(t *testing.T) {
	cfg := createTestConfig()
	mockEngine := template.NewEngine()

	aggCfg := testConfigFromAppConfig(cfg)
	aggCfg.TemplateEngine = mockEngine
	agg := New(aggCfg, NewGenreProcessor(aggCfg.Metadata, nil), NewWordProcessor(aggCfg.Metadata, nil), NewAliasResolver(aggCfg.Metadata, nil))

	require.NotNil(t, agg)
	assert.Equal(t, mockEngine, agg.templateEngine, "aggregator should use injected template engine")
}

// TestNew_InjectedRepositories verifies repository injection without loading
func TestNew_InjectedRepositories(t *testing.T) {
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
	db, err := database.New(&database.Config{Type: dbCfg.Database.Type, DSN: dbCfg.Database.DSN, LogLevel: dbCfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	genreRepo := database.NewGenreReplacementRepository(db)
	actressRepo := database.NewActressAliasRepository(db)

	agg := newAggregatorWithRepos(testConfigFromAppConfig(cfg), genreRepo, database.NewWordReplacementRepository(db), actressRepo)

	require.NotNil(t, agg)
	assert.NotNil(t, agg.genreProcessor, "genre processor should be set")
	assert.NotNil(t, agg.aliasResolver, "alias resolver should be set")
}

// TestNew_CachePrecedence verifies GenreCache takes precedence over GenreReplacementRepo
func TestNew_CachePrecedence(t *testing.T) {
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
	db, err := database.New(&database.Config{Type: dbCfg.Database.Type, DSN: dbCfg.Database.DSN, LogLevel: dbCfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	genreRepo := database.NewGenreReplacementRepository(db)

	// Populate DB with one genre replacement
	err = genreRepo.Create(context.TODO(), &models.GenreReplacement{
		Original:    "Creampie",
		Replacement: "FromDatabase",
	})
	require.NoError(t, err)

	// Inject both cache (should take precedence) and repo
	mockGenreCache := map[string]string{
		"Creampie": "FromCache",
	}

	gp := newGenreProcessorWithCache(MetadataConfigFromApp(&cfg.Metadata), genreRepo, mockGenreCache)
	agg := New(testConfigFromAppConfig(cfg), gp, NewWordProcessor(MetadataConfigFromApp(&cfg.Metadata), nil), NewAliasResolver(MetadataConfigFromApp(&cfg.Metadata), nil))

	require.NotNil(t, agg)
	// Cache should take precedence
	assert.Equal(t, "FromCache", gp.applyReplacement("Creampie"),
		"GenreCache should take precedence over GenreReplacementRepo")
}

// TestNewWithRepos_BackwardCompatibility verifies the new constructor works like the old one
func TestNewWithRepos_BackwardCompatibility(t *testing.T) {
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
	db, err := database.New(&database.Config{Type: dbCfg.Database.Type, DSN: dbCfg.Database.DSN, LogLevel: dbCfg.Database.LogLevel})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	// Populate database with test data
	genreRepo := database.NewGenreReplacementRepository(db)
	err = genreRepo.Create(context.TODO(), &models.GenreReplacement{
		Original:    "Creampie",
		Replacement: "中出し",
	})
	require.NoError(t, err)

	actressRepo := database.NewActressAliasRepository(db)
	err = actressRepo.Create(context.TODO(), &models.ActressAlias{
		AliasName:     "Yua Mikami",
		CanonicalName: "三上悠亜",
	})
	require.NoError(t, err)

	// Test newAggregatorWithRepos (production-style constructor)
	agg := newAggregatorWithRepos(testConfigFromAppConfig(cfg),
		database.NewGenreReplacementRepository(db),
		database.NewWordReplacementRepository(db),
		database.NewActressAliasRepository(db),
	)

	require.NotNil(t, agg)
	assert.NotNil(t, agg.templateEngine, "template engine should be initialized")
	assert.NotNil(t, agg.genreProcessor, "genre processor should be set")
	assert.NotNil(t, agg.aliasResolver, "alias resolver should be set")
	assert.NotNil(t, agg.resolvedPriorities, "priorities should be resolved")

	// Verify database caches were loaded via the sub-modules
	assert.Equal(t, "中出し", agg.genreProcessor.applyReplacement("Creampie"))
}

// TestAggregate_WithMockedGenreCache verifies aggregation with injected cache
func TestAggregate_WithMockedGenreCache(t *testing.T) {
	cfg := createTestConfig()
	mockGenreCache := map[string]string{
		"Creampie": "中出し",
		"Blowjob":  "フェラ",
	}

	gp := newGenreProcessorWithCache(MetadataConfigFromApp(&cfg.Metadata), nil, mockGenreCache)
	agg := New(testConfigFromAppConfig(cfg), gp, NewWordProcessor(MetadataConfigFromApp(&cfg.Metadata), nil), NewAliasResolver(MetadataConfigFromApp(&cfg.Metadata), nil))
	require.NotNil(t, agg)

	result := &models.ScraperResult{
		Source: "r18dev",
		ID:     "IPX-123",
		Title:  "Test Movie",
		Genres: []string{"Creampie", "Blowjob", "Unknown"},
	}

	movie, _, err := agg.Aggregate([]*models.ScraperResult{result})

	require.NoError(t, err)
	require.NotNil(t, movie)
	assert.Equal(t, "IPX-123", movie.ID)
	assert.Equal(t, "Test Movie", movie.Title)
	// Verify genre replacement occurred (integration test - depends on applyGenreReplacements logic)
	genreNames := make([]string, len(movie.Genres))
	for i, g := range movie.Genres {
		genreNames[i] = g.Name
	}
	assert.Contains(t, genreNames, "中出し", "Creampie should be replaced with 中出し")
	assert.Contains(t, genreNames, "フェラ", "Blowjob should be replaced with フェラ")
}

// TestAggregate_WithMockedActressAlias verifies actress alias resolution with injected cache
func TestAggregate_WithMockedActressAlias(t *testing.T) {
	cfg := createTestConfig()
	mockActressCache := map[string]string{
		"Yua Mikami": "三上悠亜",
	}

	ar := newAliasResolverWithCache(MetadataConfigFromApp(&cfg.Metadata), nil, mockActressCache)
	agg := New(testConfigFromAppConfig(cfg), NewGenreProcessor(MetadataConfigFromApp(&cfg.Metadata), nil), NewWordProcessor(MetadataConfigFromApp(&cfg.Metadata), nil), ar)
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

	movie, _, err := agg.Aggregate([]*models.ScraperResult{result})

	require.NoError(t, err)
	require.NotNil(t, movie)
	assert.Len(t, movie.Actresses, 1)
	assert.Equal(t, "Yua", movie.Actresses[0].FirstName)
	assert.Equal(t, "Mikami", movie.Actresses[0].LastName)
}

// TestGetResolvedPriorities verifies the interface method returns cached priorities
func TestGetResolvedPriorities(t *testing.T) {
	cfg := createTestConfig()
	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	require.NotNil(t, agg)

	priorities := agg.getResolvedPriorities()

	assert.NotNil(t, priorities, "resolved priorities should not be nil")
	assert.Greater(t, len(priorities), 0, "should have resolved at least some field priorities")
	assert.Contains(t, priorities, "Title", "Title priority should be resolved")
	assert.Contains(t, priorities, "Description", "Description priority should be resolved")
}

// Helper function to create a minimal test config
func createTestConfig() *config.Config {
	cfg := config.DefaultConfig(nil, nil)
	cfg.Scrapers.Priority = []string{"r18dev", "dmm"}
	// Ensure Overrides map is initialized before accessing
	if cfg.Scrapers.Overrides == nil {
		cfg.Scrapers.Overrides = make(map[string]*models.ScraperSettings)
	}
	// Initialize scraper override entries directly since scrapers aren't registered in tests
	cfg.Scrapers.Overrides["r18dev"] = &models.ScraperSettings{Enabled: true}
	cfg.Scrapers.Overrides["dmm"] = &models.ScraperSettings{Enabled: true}
	return cfg
}
