package aggregator

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// aggregator.go – Config() and TemplateEngine() non-nil receiver paths
// ---------------------------------------------------------------------------

func TestConfig_NonNilReceiver(t *testing.T) {
	cfg := &Config{
		Metadata:         &MetadataConfig{},
		ScrapersPriority: []string{"r18dev"},
	}
	agg := New(cfg, nil, nil, nil)
	require.NotNil(t, agg)

	result, err := agg.Config()
	assert.NoError(t, err)
	assert.Same(t, cfg, result)
}

func TestTemplateEngine_NonNilReceiver(t *testing.T) {
	cfg := &Config{
		Metadata:         &MetadataConfig{},
		ScrapersPriority: []string{"r18dev"},
	}
	agg := New(cfg, nil, nil, nil)
	require.NotNil(t, agg)

	te, err := agg.TemplateEngine()
	assert.NoError(t, err)
	assert.NotNil(t, te)
}

// ---------------------------------------------------------------------------
// aggregator.go – New() with nil TemplateEngine (line 104-106)
// ---------------------------------------------------------------------------

func TestNew_NilTemplateEngineUsesDefault(t *testing.T) {
	cfg := &Config{
		Metadata:         &MetadataConfig{},
		ScrapersPriority: []string{"r18dev"},
		TemplateEngine:   nil, // explicitly nil → triggers default creation
	}
	agg := New(cfg, nil, nil, nil)
	require.NotNil(t, agg)
	assert.NotNil(t, agg.templateEngine, "New should create default engine when TemplateEngine is nil")
}

// ---------------------------------------------------------------------------
// aggregate.go – isUnknownActress: LastName match path (line 281-283)
// ---------------------------------------------------------------------------

func TestIsUnknownActress_LastNameMatch(t *testing.T) {
	// When nameKey doesn't match but LastName alone does.
	// Example: FirstName is set, so nameKey = "yui unknown" (not just "unknown"),
	// but normalizeNameKey(LastName) = "unknown" → should still return true.
	info := models.ActressInfo{FirstName: "Yui", LastName: "Unknown"}
	nameKey := resolveNameKey(info.JapaneseName, info.FirstName, info.LastName)
	// nameKey = "yui unknown" which != "unknown", so the nameKey check fails
	// but normalizeNameKey(LastName) = "unknown" matches
	assert.True(t, isUnknownActress(info, nameKey, "unknown"), "LastName should match unknown text even when nameKey differs")
}

// ---------------------------------------------------------------------------
// aggregate.go – resolveNameKey: lastName+firstName fallback (line 287-289, 300)
// ---------------------------------------------------------------------------

func TestResolveNameKey_LastNameFirstFallback(t *testing.T) {
	// When japaneseName is empty and firstName+lastName is also empty but lastName is set,
	// it falls back to lastName + " " + firstName
	key := resolveNameKey("", "", "Hatano")
	assert.Equal(t, "hatano", key, "should fall back to lastName + space + firstName when japaneseName and full name are empty")
}

func TestResolveNameKey_FirstNameOnly(t *testing.T) {
	// Only firstName provided, lastName empty → "firstname " normalized → "firstname"
	key := resolveNameKey("", "Yui", "")
	assert.Equal(t, "yui", key, "should return firstName normalized when that's all we have")
}

func TestResolveNameKey_JapaneseNamePreferred(t *testing.T) {
	key := resolveNameKey("波多野結衣", "Yui", "Hatano")
	assert.Equal(t, "波多野結衣", key, "japaneseName should be preferred over FirstName+LastName")
}

func TestResolveNameKey_FirstNameLastNameWhenNoJapanese(t *testing.T) {
	key := resolveNameKey("", "Yui", "Hatano")
	assert.Equal(t, "yui hatano", key, "should use firstName + lastName when no japaneseName")
}

// ---------------------------------------------------------------------------
// actress_merger.go – DMMID collision with different IDs (line 114-116, 123-125)
// ---------------------------------------------------------------------------

func TestActressMerger_DifferentDMMIDsBothNonZero(t *testing.T) {
	// When an existing actress has a non-zero DMMID and the new info has a
	// different non-zero DMMID, the code at line 123 inserts the existing
	// actress under the new DMMID key as well (multi-key alias). Since both
	// map keys point to the same actress pointer, iteration produces 2 entries
	// but they share the same field values.
	merger := newActressMerger()
	sources := []actressSource{
		{
			Source: "r18dev",
			Actresses: []models.ActressInfo{
				{
					DMMID:        100,
					FirstName:    "Yui",
					LastName:     "Hatano",
					JapaneseName: "波多野結衣",
				},
			},
		},
		{
			Source: "dmm",
			Actresses: []models.ActressInfo{
				{
					DMMID:        200,
					FirstName:    "Yui",
					LastName:     "Hatano",
					JapaneseName: "波多野結衣",
				},
			},
		},
	}
	opts := actressMergeOptions{
		Priority: []string{"r18dev", "dmm"},
	}

	actresses := merger.Merge(sources, opts)

	// Two sources return the same actress (matched by name) with DIFFERENT
	// non-zero DMMIDs. The first DMMID (higher priority r18dev) wins and the
	// actress is emitted ONCE. (The buggy else-if re-indexed the same pointer
	// under the second DMMID key, causing Phase 2 to emit a duplicate.)
	require.Len(t, actresses, 1)
	// DMMID should come from r18dev (higher priority)
	for _, a := range actresses {
		assert.Equal(t, "Yui", a.FirstName)
		assert.Equal(t, "Hatano", a.LastName)
		assert.Equal(t, "波多野結衣", a.JapaneseName)
	}
}

func TestActressMerger_SameDMMIDMergeFillFields(t *testing.T) {
	// When same DMMID from two sources, fill empty fields from lower priority
	merger := newActressMerger()
	sources := []actressSource{
		{
			Source: "r18dev",
			Actresses: []models.ActressInfo{
				{
					DMMID:    12345,
					ThumbURL: "https://r18dev.example.com/thumb.jpg",
				},
			},
		},
		{
			Source: "dmm",
			Actresses: []models.ActressInfo{
				{
					DMMID:        12345,
					FirstName:    "Yui",
					LastName:     "Hatano",
					JapaneseName: "波多野結衣",
				},
			},
		},
	}
	opts := actressMergeOptions{
		Priority: []string{"r18dev", "dmm"},
	}

	actresses := merger.Merge(sources, opts)
	require.Len(t, actresses, 1)
	assert.Equal(t, 12345, actresses[0].DMMID)
	assert.Equal(t, "Yui", actresses[0].FirstName, "should fill FirstName from dmm")
	assert.Equal(t, "Hatano", actresses[0].LastName, "should fill LastName from dmm")
	assert.Equal(t, "波多野結衣", actresses[0].JapaneseName, "should fill JapaneseName from dmm")
	assert.Equal(t, "https://r18dev.example.com/thumb.jpg", actresses[0].ThumbURL, "should keep ThumbURL from r18dev")
}

// ---------------------------------------------------------------------------
// aggregate_translation.go – legacy translation merge with existing (lines 117-137)
// ---------------------------------------------------------------------------

func TestBuildTranslations_LegacyMergeIntoExisting(t *testing.T) {
	// Scenario: A scraper provides both explicit Translations and a Language field.
	// The explicit Translations are processed first. Then, if the scraper is a
	// "winner" (its fields match the aggregated movie), a legacy translation is
	// created and merged into the existing translation from the explicit list.
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Priority: []string{"r18dev"},
			},
		},
	}

	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	results := []*models.ScraperResult{
		{
			Source:   "r18dev",
			Language: "en",
			Title:    "English Title",
			// Provide explicit Translations that create an "en" entry with Title only
			Translations: []models.MovieTranslation{
				{Language: "en", Title: "English Title"},
			},
		},
	}

	// Movie has matching title → r18dev is a "winner"
	movie := &models.Movie{Title: "English Title"}

	translations := agg.buildTranslations(results, movie)
	require.Len(t, translations, 1)
	assert.Equal(t, "en", translations[0].Language)
	assert.Equal(t, "English Title", translations[0].Title)
	// SourceName is only set when the legacy path appends a NEW translation;
	// when merging into an existing translation, SourceName was already set
	// by the explicit Translations path (which doesn't set SourceName on
	// MovieTranslation). This is expected behavior.
}

func TestBuildTranslations_LegacyMergeFillsEmptyFields(t *testing.T) {
	// Scenario: Explicit Translations create an "en" entry with Title only.
	// The legacy path then merges additional fields (Director, Maker, Label, Series)
	// into the existing "en" entry because the scraper won those fields.
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Priority: []string{"r18dev"},
			},
		},
	}
	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	results := []*models.ScraperResult{
		{
			Source:   "r18dev",
			Language: "en",
			Title:    "Title",
			Director: "Dir",
			Maker:    "Maker",
			Label:    "Label",
			Series:   "Series",
			Translations: []models.MovieTranslation{
				{Language: "en", Title: "Title"},
			},
		},
	}

	// Movie matches on Title so r18dev is a winner
	movie := &models.Movie{Title: "Title", Director: "Dir", Maker: "Maker", Label: "Label", Series: "Series"}

	translations := agg.buildTranslations(results, movie)
	require.Len(t, translations, 1)
	assert.Equal(t, "Title", translations[0].Title)
	assert.Equal(t, "Dir", translations[0].Director, "legacy merge should fill Director")
	assert.Equal(t, "Maker", translations[0].Maker, "legacy merge should fill Maker")
	assert.Equal(t, "Label", translations[0].Label, "legacy merge should fill Label")
	assert.Equal(t, "Series", translations[0].Series, "legacy merge should fill Series")
}

func TestBuildTranslations_LegacyMergeDoesNotOverwrite(t *testing.T) {
	// Scenario: Explicit Translations have Title filled. Legacy path has Title too.
	// The legacy merge should NOT overwrite the existing Title.
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Priority: []string{"r18dev"},
			},
		},
	}
	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	results := []*models.ScraperResult{
		{
			Source:   "r18dev",
			Language: "en",
			Title:    "Winner Title",
			Translations: []models.MovieTranslation{
				{Language: "en", Title: "Explicit Title", Description: "Explicit Desc"},
			},
		},
	}

	movie := &models.Movie{Title: "Winner Title"}

	translations := agg.buildTranslations(results, movie)
	require.Len(t, translations, 1)
	assert.Equal(t, "Explicit Title", translations[0].Title, "should NOT overwrite explicit Title with legacy Title")
	assert.Equal(t, "Explicit Desc", translations[0].Description, "should keep explicit Description")
}

func TestBuildTranslations_LegacyMergeFillsOriginalTitleAndDescription(t *testing.T) {
	// Scenario: Explicit Translations have an "en" entry with only Title.
	// The legacy path fills OriginalTitle and Description into the same entry.
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Priority: []string{"r18dev"},
			},
		},
	}
	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	results := []*models.ScraperResult{
		{
			Source:        "r18dev",
			Language:      "en",
			Title:         "Title",
			OriginalTitle: "Original",
			Description:   "Desc",
			Translations: []models.MovieTranslation{
				{Language: "en", Title: "Title"},
			},
		},
	}

	movie := &models.Movie{Title: "Title", OriginalTitle: "Original", Description: "Desc"}

	translations := agg.buildTranslations(results, movie)
	require.Len(t, translations, 1)
	assert.Equal(t, "Title", translations[0].Title)
	assert.Equal(t, "Original", translations[0].OriginalTitle, "legacy merge should fill OriginalTitle")
	assert.Equal(t, "Desc", translations[0].Description, "legacy merge should fill Description")
}

// ---------------------------------------------------------------------------
// aggregator_validation.go – uncovered field aliases (line 33-52, 82-84)
// ---------------------------------------------------------------------------

func TestValidateRequiredFieldsScraped_PlotAlias(t *testing.T) {
	movie := &models.Movie{Description: ""}
	err := validateRequiredFieldsScraped(movie, []string{"plot"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Description")
}

func TestValidateRequiredFieldsScraped_StudioAlias(t *testing.T) {
	movie := &models.Movie{Maker: ""}
	err := validateRequiredFieldsScraped(movie, []string{"studio"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Maker")
}

func TestValidateRequiredFieldsScraped_SetAlias(t *testing.T) {
	movie := &models.Movie{Series: ""}
	err := validateRequiredFieldsScraped(movie, []string{"set"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Series")
}

func TestValidateRequiredFieldsScraped_PremieredAlias(t *testing.T) {
	movie := &models.Movie{ReleaseDate: nil}
	err := validateRequiredFieldsScraped(movie, []string{"premiered"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ReleaseDate")
}

func TestValidateRequiredFieldsScraped_GenreAlias(t *testing.T) {
	movie := &models.Movie{Genres: nil}
	err := validateRequiredFieldsScraped(movie, []string{"genre"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Genres")
}

func TestValidateRequiredFieldsScraped_ActressAlias(t *testing.T) {
	movie := &models.Movie{Actresses: nil}
	err := validateRequiredFieldsScraped(movie, []string{"actress"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Actresses")
}

func TestValidateRequiredFieldsScraped_OriginalTitleAlias(t *testing.T) {
	movie := &models.Movie{OriginalTitle: ""}
	err := validateRequiredFieldsScraped(movie, []string{"originaltitle"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "OriginalTitle")
}

func TestValidateRequiredFieldsScraped_TrailerURLAlias(t *testing.T) {
	movie := &models.Movie{TrailerURL: ""}
	err := validateRequiredFieldsScraped(movie, []string{"trailer_url"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "TrailerURL")
}

// ---------------------------------------------------------------------------
// alias_resolver.go – NewAliasResolver with repo+enabled (line 37-39)
// ---------------------------------------------------------------------------

func TestNewAliasResolver_WithRepoAndEnabled(t *testing.T) {
	mockRepo := &mockAliasLookupRepo{
		aliases: map[string]string{"Yui Hatano": "Hatano Yui"},
	}
	cfg := &MetadataConfig{
		ActressDatabase: actressDatabaseConfigView{
			Enabled:      true,
			ConvertAlias: true,
		},
	}
	ar := NewAliasResolver(cfg, mockRepo)
	require.NotNil(t, ar)

	// Verify cache was loaded
	actress := &models.Actress{FirstName: "Yui", LastName: "Hatano"}
	ar.Resolve(actress)
	assert.Equal(t, "Hatano", actress.LastName)
	assert.Equal(t, "Yui", actress.FirstName)
}

func TestAliasResolver_Resolve_DisabledWhenNotEnabled(t *testing.T) {
	cfg := &MetadataConfig{
		ActressDatabase: actressDatabaseConfigView{
			Enabled:      false,
			ConvertAlias: true,
		},
	}
	ar := newAliasResolverWithCache(cfg, nil, map[string]string{
		"Yui Hatano": "Hatano Yui",
	})
	actress := &models.Actress{FirstName: "Yui", LastName: "Hatano"}
	ar.Resolve(actress)
	assert.Equal(t, "Yui", actress.FirstName, "should not resolve when actress database disabled")
	assert.Equal(t, "Hatano", actress.LastName)
}

func TestAliasResolver_Resolve_NilActress(t *testing.T) {
	cfg := &MetadataConfig{
		ActressDatabase: actressDatabaseConfigView{
			Enabled:      true,
			ConvertAlias: true,
		},
	}
	ar := newAliasResolverWithCache(cfg, nil, map[string]string{
		"Yui Hatano": "Hatano Yui",
	})
	ar.Resolve(nil) // should not panic
}

func TestAliasResolver_Resolve_OnlyFirstNameWithCache(t *testing.T) {
	// When only FirstName is set (no LastName), the FirstName+LastName path
	// is skipped, but if JapaneseName is set and matches, that path is hit.
	cfg := &MetadataConfig{
		ActressDatabase: actressDatabaseConfigView{
			Enabled:      true,
			ConvertAlias: true,
		},
	}
	ar := newAliasResolverWithCache(cfg, nil, map[string]string{
		"Yui": "Canonical Yui",
	})
	actress := &models.Actress{JapaneseName: "Yui"}
	ar.Resolve(actress)
	assert.Equal(t, "Canonical Yui", actress.JapaneseName)
}

func TestAliasResolver_Resolve_ReverseNameLookup(t *testing.T) {
	// When FirstName+LastName doesn't match but LastName+FirstName does
	cfg := &MetadataConfig{
		ActressDatabase: actressDatabaseConfigView{
			Enabled:      true,
			ConvertAlias: true,
		},
	}
	ar := newAliasResolverWithCache(cfg, nil, map[string]string{
		"Hatano Yui": "Canonical Hatano Yui",
	})
	actress := &models.Actress{FirstName: "Yui", LastName: "Hatano"}
	ar.Resolve(actress)
	// FirstName+LastName = "Yui Hatano" → not found
	// LastName+FirstName = "Hatano Yui" → found
	assert.Equal(t, "Hatano", actress.LastName)
	assert.Equal(t, "Yui", actress.FirstName)
}

func TestAliasResolver_Resolve_CanonicalMultiPart(t *testing.T) {
	// When canonical name has 3+ parts, it's stored as JapaneseName
	cfg := &MetadataConfig{
		ActressDatabase: actressDatabaseConfigView{
			Enabled:      true,
			ConvertAlias: true,
		},
	}
	ar := newAliasResolverWithCache(cfg, nil, map[string]string{
		"Yui Hatano": "Some Long Canonical Name",
	})
	actress := &models.Actress{FirstName: "Yui", LastName: "Hatano"}
	ar.Resolve(actress)
	assert.Equal(t, "Some Long Canonical Name", actress.JapaneseName, "3+ part canonical should go to JapaneseName")
}

// ---------------------------------------------------------------------------
// alias_resolver.go – Reload with nil receiver (line 116-118)
// ---------------------------------------------------------------------------

func TestAliasResolver_Reload_NilReceiver(t *testing.T) {
	var ar *aliasResolver
	ar.Reload(context.Background()) // should not panic
}

// ---------------------------------------------------------------------------
// config.go – MetadataConfigFromApp with nil input (line 54-56)
// ---------------------------------------------------------------------------

func TestMetadataConfigFromApp_NilInput(t *testing.T) {
	result := MetadataConfigFromApp(nil)
	assert.Nil(t, result)
}

// ---------------------------------------------------------------------------
// genre_processor.go – NewGenreProcessor with repo+enabled (line 47-49)
// ---------------------------------------------------------------------------

func TestNewGenreProcessor_WithRepoAndEnabled(t *testing.T) {
	mockRepo := &mockGenreLookupRepo{
		replacements: map[string]string{"ドラマ": "Drama"},
	}
	cfg := &MetadataConfig{
		GenreReplacement: genreReplacementConfigView{
			Enabled: true,
			AutoAdd: false,
		},
	}
	gp := NewGenreProcessor(cfg, mockRepo)
	require.NotNil(t, gp)
	assert.Equal(t, "Drama", gp.applyReplacement("ドラマ"))
}

func TestGenreProcessor_ApplyReplacement_AutoAddWithRepo(t *testing.T) {
	// Tests the auto-add path when a genre is not in cache, repo is available,
	// and auto-add is enabled. This covers lines 101-104.
	mockRepo := &mockGenreLookupRepo{
		replacements: map[string]string{},
		createErr:    nil,
	}
	cfg := &MetadataConfig{
		GenreReplacement: genreReplacementConfigView{
			Enabled: true,
			AutoAdd: true,
		},
	}
	gp := NewGenreProcessor(cfg, mockRepo)

	// Genre not in cache → triggers auto-add
	result := gp.applyReplacement("NewGenre")
	assert.Equal(t, "NewGenre", result, "identity mapping should be returned")

	// Second call should find it in cache now (no second Create)
	result2 := gp.applyReplacement("NewGenre")
	assert.Equal(t, "NewGenre", result2)
}

// ---------------------------------------------------------------------------
// genre_processor.go – isIgnored nil receiver (line 114-116)
// ---------------------------------------------------------------------------

func TestGenreProcessor_IsIgnored_NilReceiver(t *testing.T) {
	var gp *genreProcessor
	assert.False(t, gp.isIgnored("anything"), "nil genreProcessor should not ignore anything")
}

// ---------------------------------------------------------------------------
// genre_processor.go – Reload nil receiver (line 142-144)
// ---------------------------------------------------------------------------

func TestGenreProcessor_Reload_NilReceiver(t *testing.T) {
	var gp *genreProcessor
	gp.Reload(context.Background()) // should not panic
}

// ---------------------------------------------------------------------------
// genre_processor.go – loadCacheLocked with nil repo (line 165-167)
// ---------------------------------------------------------------------------

func TestGenreProcessor_LoadCacheLocked_NilRepo(t *testing.T) {
	cfg := &MetadataConfig{
		GenreReplacement: genreReplacementConfigView{Enabled: true},
	}
	gp := NewGenreProcessor(cfg, nil)
	require.NotNil(t, gp)
	// loadCacheLocked is called internally during Reload if repo is nil → early return
	gp.Reload(context.Background())
	// Verify cache is still empty
	assert.Equal(t, "Test", gp.applyReplacement("Test"), "no replacement when no repo")
}

// ---------------------------------------------------------------------------
// priority.go – getFieldPriorityFromConfig nil cfg (line 113-115)
// ---------------------------------------------------------------------------

func TestGetFieldPriorityFromConfig_NilCfg(t *testing.T) {
	result := getFieldPriorityFromConfig(nil, "Title")
	assert.Nil(t, result)
}

// ---------------------------------------------------------------------------
// priority.go – toSnakeCase edge cases (line 155-157: single char or ending uppercase)
// ---------------------------------------------------------------------------

func TestToSnakeCase_SingleChar(t *testing.T) {
	assert.Equal(t, "id", toSnakeCase("ID"))
}

func TestToSnakeCase_EndingUppercase(t *testing.T) {
	// "PosterURL" → the L before URL is uppercase, next char (R) is also uppercase,
	// but the U after R is uppercase and the L at end (after U) is lowercase
	// Testing: "ContentID" → the 'I' in 'ID' is uppercase, 'D' is uppercase, next is end-of-string
	assert.Equal(t, "content_id", toSnakeCase("ContentID"))
}

func TestToSnakeCase_AllLower(t *testing.T) {
	assert.Equal(t, "title", toSnakeCase("Title"))
}

func TestToSnakeCase_ConsecutiveUppercase(t *testing.T) {
	// "HTMLParser" → "html_parser"
	assert.Equal(t, "html_parser", toSnakeCase("HTMLParser"))
}

// ---------------------------------------------------------------------------
// word_processor.go – NewWordProcessor with repo+enabled (line 61-63)
// ---------------------------------------------------------------------------

func TestNewWordProcessor_WithRepoAndEnabled(t *testing.T) {
	mockRepo := &mockWordLookupRepo{
		replacements: map[string]string{"old": "new", "bad": "good"},
	}
	cfg := &MetadataConfig{
		WordReplacement: wordReplacementConfigView{Enabled: true},
	}
	wp := NewWordProcessor(cfg, mockRepo)
	require.NotNil(t, wp)
	assert.Equal(t, "new", wp.Apply("old"))
	assert.Equal(t, "good", wp.Apply("bad"))
}

// ---------------------------------------------------------------------------
// word_processor.go – Reload nil receiver (line 131-133)
// ---------------------------------------------------------------------------

func TestWordProcessor_Reload_NilReceiver(t *testing.T) {
	var wp *wordProcessor
	wp.Reload(context.Background()) // should not panic
}

// ---------------------------------------------------------------------------
// Integration: full Aggregate with translation legacy merge covering all fields
// ---------------------------------------------------------------------------

func TestAggregate_TranslationLegacyMergeAllFields(t *testing.T) {
	// This test exercises the legacy translation merge paths where a scraper
	// with Language set is a "winner" and its translation merges into an
	// existing translation entry created by explicit Translations.
	cfg := &config.Config{
		Scrapers: config.ScrapersConfig{
			Priority: []string{"r18dev"},
		},
		Metadata: config.MetadataConfig{
			Priority: config.PriorityConfig{
				Priority: []string{"r18dev"},
			},
		},
	}
	agg := newAggregatorNoDB(testConfigFromAppConfig(cfg))

	releaseDate := time.Date(2021, 1, 8, 0, 0, 0, 0, time.UTC)

	results := []*models.ScraperResult{
		{
			Source:        "r18dev",
			Language:      "en",
			ID:            "IPX-001",
			Title:         "English Title",
			OriginalTitle: "English Original",
			Description:   "English Desc",
			Director:      "English Dir",
			Maker:         "English Maker",
			Label:         "English Label",
			Series:        "English Series",
			ReleaseDate:   &releaseDate,
			// Provide an explicit translation with only Title set
			Translations: []models.MovieTranslation{
				{
					Language: "en",
					Title:    "English Title",
				},
			},
		},
	}

	movie, aggResult, err := agg.Aggregate(results)
	require.NoError(t, err)
	require.NotNil(t, movie)
	require.NotNil(t, aggResult)

	// Verify the legacy translation filled all other fields into the existing "en" entry
	require.Len(t, movie.Translations, 1)
	tr := movie.Translations[0]
	assert.Equal(t, "en", tr.Language)
	assert.Equal(t, "English Title", tr.Title)
	assert.Equal(t, "English Original", tr.OriginalTitle)
	assert.Equal(t, "English Desc", tr.Description)
	assert.Equal(t, "English Dir", tr.Director)
	assert.Equal(t, "English Maker", tr.Maker)
	assert.Equal(t, "English Label", tr.Label)
	assert.Equal(t, "English Series", tr.Series)
}

// ---------------------------------------------------------------------------
// Mock repositories for testing
// ---------------------------------------------------------------------------

type mockGenreLookupRepo struct {
	replacements map[string]string
	createErr    error
}

func (m *mockGenreLookupRepo) GetReplacementMap(_ context.Context) (map[string]string, error) {
	return m.replacements, nil
}

func (m *mockGenreLookupRepo) Create(_ context.Context, replacement *models.GenreReplacement) error {
	if m.replacements == nil {
		m.replacements = make(map[string]string)
	}
	m.replacements[replacement.Original] = replacement.Replacement
	return m.createErr
}

type mockWordLookupRepo struct {
	replacements map[string]string
}

func (m *mockWordLookupRepo) GetReplacementMap(_ context.Context) (map[string]string, error) {
	return m.replacements, nil
}

type mockAliasLookupRepo struct {
	aliases map[string]string
}

func (m *mockAliasLookupRepo) GetAliasMap(_ context.Context) (map[string]string, error) {
	return m.aliases, nil
}

// TestIsUnknownActress_JapaneseNameMatches_NameKeyDiffers covers the
// NormalizeActressNameKey(info.JapaneseName) == unknownText branch: nameKey is
// set to a DIFFERENT value so the earlier nameKey == unknownText check is false,
// forcing the JapaneseName match path to return true.
func TestIsUnknownActress_JapaneseNameMatches_NameKeyDiffers(t *testing.T) {
	info := models.ActressInfo{JapaneseName: "未知の女優"}
	assert.True(t, isUnknownActress(info, "different", "未知の女優"),
		"JapaneseName matching unknownText must return true even when nameKey differs")
}
