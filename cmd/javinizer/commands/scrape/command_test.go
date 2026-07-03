package scrape_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/scrape"
	"github.com/javinizer/javinizer-go/internal/commandutil"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraper"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	// Register scraper defaults for Finalize
	_ "github.com/javinizer/javinizer-go/internal/scraper/dmm"
	_ "github.com/javinizer/javinizer-go/internal/scraper/r18dev"
)

func init() {
	reg := scraperutil.NewScraperRegistry()
	scraper.RegisterAll(reg)
}

// Tests

func TestScrapeCommand_Structure(t *testing.T) {
	cmd := scrape.NewCommand()

	assert.Equal(t, "scrape [id]", cmd.Use)
	assert.Contains(t, cmd.Short, "Scrape metadata")
	assert.NotNil(t, cmd.RunE, "RunE should be set")

	// Verify command has Args validation set (can't compare functions directly)
	assert.NotNil(t, cmd.Args, "Args validation should be set")
}

func TestScrapeCommand_Flags(t *testing.T) {
	cmd := scrape.NewCommand()

	tests := []struct {
		name         string
		flag         string
		shorthand    string
		expectedType string
		hasDefault   bool
	}{
		{"force", "force", "f", "bool", true},
		{"scrapers", "scrapers", "s", "stringSlice", false},
		{"scrape-actress", "scrape-actress", "", "bool", false},
		{"no-scrape-actress", "no-scrape-actress", "", "bool", false},
		{"browser", "browser", "", "bool", false},
		{"no-browser", "no-browser", "", "bool", false},
		{"browser-timeout", "browser-timeout", "", "int", false},
		{"actress-db", "actress-db", "", "bool", false},
		{"no-actress-db", "no-actress-db", "", "bool", false},
		{"genre-replacement", "genre-replacement", "", "bool", false},
		{"no-genre-replacement", "no-genre-replacement", "", "bool", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flag := cmd.Flags().Lookup(tt.flag)
			assert.NotNil(t, flag, "Flag %s should exist", tt.flag)

			if tt.shorthand != "" {
				assert.Equal(t, tt.shorthand, flag.Shorthand, "Flag %s shorthand mismatch", tt.flag)
			}
		})
	}
}

func TestScrapeCommand_FlagDefaults(t *testing.T) {
	cmd := scrape.NewCommand()

	// Test default values
	forceFlag := cmd.Flags().Lookup("force")
	assert.Equal(t, "false", forceFlag.DefValue, "force should default to false")

	browserTimeoutFlag := cmd.Flags().Lookup("browser-timeout")
	assert.Equal(t, "0", browserTimeoutFlag.DefValue, "browser-timeout should default to 0")
}

func TestScrapeCommand_HelpText(t *testing.T) {
	cmd := scrape.NewCommand()

	// Capture help output
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--help"})

	_ = cmd.Execute()

	output := buf.String()

	// Verify help text contains key information
	assert.Contains(t, output, "scrape", "Help should mention command name")
	assert.Contains(t, output, "Flags:", "Help should show flags section")
	assert.Contains(t, output, "--force", "Help should document --force flag")
	assert.Contains(t, output, "--scrapers", "Help should document --scrapers flag")
}

func TestScrapeCommand_FlagParsing(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		checkFlag   string
		expectedVal string
	}{
		{
			name:        "force flag short form",
			args:        []string{"-f", "TEST-001"},
			checkFlag:   "force",
			expectedVal: "true",
		},
		{
			name:        "force flag long form",
			args:        []string{"--force", "TEST-001"},
			checkFlag:   "force",
			expectedVal: "true",
		},
		{
			name:        "scrapers flag",
			args:        []string{"--scrapers", "dmm,r18dev", "TEST-001"},
			checkFlag:   "scrapers",
			expectedVal: "[dmm,r18dev]",
		},
		{
			name:        "scrapers flag short form",
			args:        []string{"-s", "dmm", "TEST-001"},
			checkFlag:   "scrapers",
			expectedVal: "[dmm]",
		},
		{
			name:        "browser timeout",
			args:        []string{"--browser-timeout", "30", "TEST-001"},
			checkFlag:   "browser-timeout",
			expectedVal: "30",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := scrape.NewCommand()
			cmd.SetArgs(tt.args)

			// Don't actually execute, just parse flags
			err := cmd.ParseFlags(tt.args)
			assert.NoError(t, err, "Flag parsing should succeed")

			flag := cmd.Flags().Lookup(tt.checkFlag)
			assert.NotNil(t, flag, "Flag %s should exist", tt.checkFlag)
			assert.Equal(t, tt.expectedVal, flag.Value.String(), "Flag value mismatch")
		})
	}
}

func TestScrapeCommand_MutuallyExclusiveFlags(t *testing.T) {
	// Test that mutually exclusive flags can both be defined
	// (the actual mutual exclusion is enforced in the command logic, not by cobra)
	cmd := scrape.NewCommand()

	tests := []struct {
		name  string
		flag1 string
		flag2 string
	}{
		{"scrape-actress flags", "scrape-actress", "no-scrape-actress"},
		{"browser flags", "browser", "no-browser"},
		{"actress-db flags", "actress-db", "no-actress-db"},
		{"genre-replacement flags", "genre-replacement", "no-genre-replacement"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flag1 := cmd.Flags().Lookup(tt.flag1)
			flag2 := cmd.Flags().Lookup(tt.flag2)

			assert.NotNil(t, flag1, "Flag %s should exist", tt.flag1)
			assert.NotNil(t, flag2, "Flag %s should exist", tt.flag2)
		})
	}
}

func TestScrapeCommand_RequiresArgument(t *testing.T) {
	cmd := scrape.NewCommand()

	// Command should have Args validation set
	assert.NotNil(t, cmd.Args, "Args validation should be set")

	// Test that command fails without argument
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err, "Command should fail without ID argument")
	assert.Contains(t, err.Error(), "accepts 1 arg(s), received 0")
}

func TestScrapeCommand_Integration_CachedMovie(t *testing.T) {
	t.Skip("Skipping integration test - requires full command context with root command")

	// Note: This test is skipped because the scrape command needs to be run
	// within the context of the root command to have access to the --config flag.
	// The business logic for caching is tested in internal/commandutil/scraping_test.go
	// and this command-level test would require significant test infrastructure.
}

func TestScrapeCommand_Integration_ForceRefresh(t *testing.T) {
	t.Skip("Skipping integration test - requires full command context with root command")

	// Note: This test is skipped because the scrape command needs to be run
	// within the context of the root command to have access to the --config flag.
	// The business logic for force refresh is tested in internal/commandutil/scraping_test.go
}

func TestScrapeCommand_CanBeInstantiated(t *testing.T) {
	cmd := scrape.NewCommand()

	// Verify command can be created without errors
	assert.NotNil(t, cmd)
	assert.Equal(t, "scrape [id]", cmd.Use)
	assert.NotNil(t, cmd.RunE, "RunE should be set")
}

func TestRun_CacheHit_HashMismatch(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	// Setup mock OpenAI server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// OpenAI expects content to be a JSON array string
		response := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"content": json.RawMessage(`["New Translation Title"]`),
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create temp config file with NEW translation settings (gpt-4)
	configContent := `
config_version: 3
database:
  dsn: ":memory:"
scrapers:
  priority: ["mock"]
metadata:
  translation:
    enabled: true
    provider: openai
    source_language: ja
    target_language: en
    fields:
      title: true
    openai:
      model: gpt-4
      api_key: test-key
      base_url: ` + server.URL + `
  priority:
    id: ["mock"]
    content_id: ["mock"]
    title: ["mock"]
    description: ["mock"]
matching:
  extensions: [".mp4"]
  regex_enabled: false
`
	tmpFile := t.TempDir() + "/config.yaml"
	require.NoError(t, os.WriteFile(tmpFile, []byte(configContent), 0644))

	// Load config and create database
	cfg, err := config.Load(tmpFile)
	require.NoError(t, err)
	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	// Create cached movie with OLD translation (different hash - gpt-3.5-turbo)
	oldConfig := config.TranslationConfig{
		Provider:       "openai",
		TargetLanguage: "en",
		OpenAI:         config.OpenAITranslationConfig{Model: "gpt-3.5-turbo"},
	}
	oldHash := oldConfig.SettingsHash()

	cachedMovie := &models.Movie{
		ContentID: "test001",
		ID:        "TEST-001",
		Title:     "Cached Title",
		Translations: []models.MovieTranslation{
			{
				Language:     "en",
				Title:        "Old Translation",
				SettingsHash: oldHash,
				SourceName:   "translation-service",
			},
		},
	}
	movieRepo := database.NewMovieRepository(db)
	require.NoError(t, movieRepo.Create(context.TODO(), cachedMovie))

	// Setup command and dependencies
	cmd := scrape.NewCommand()
	registry := scraperutil.NewScraperRegistry()
	deps, err := commandutil.NewDependenciesWithOptions(cfg, &commandutil.DependenciesOptions{
		DB:              db,
		ScraperRegistry: registry,
	})
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()

	// Run scrape - should hit cache, detect hash mismatch, re-translate
	movie, results, err := scrape.Run(context.Background(), cmd, []string{"TEST-001"}, tmpFile, deps)

	require.NoError(t, err)
	assert.NotNil(t, movie)
	assert.Len(t, results, 1, "Cache hit returns one synthesized scraper result")
	assert.NotEmpty(t, results[0].Source)

	// Verify: should have updated the translation (replaced old with new)
	assert.Len(t, movie.Translations, 1, "should replace old translation with new one")

	// Verify translation has NEW hash
	newHash := cfg.Metadata.Translation.SettingsHash()
	assert.Equal(t, newHash, movie.Translations[0].SettingsHash, "should have new hash")
	assert.Equal(t, "en", movie.Translations[0].Language)
	assert.Equal(t, "New Translation Title", movie.Translations[0].Title)
}

func TestRun_CacheHit_HashMatch(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}

	// Create temp config file
	configContent := `
config_version: 3
database:
  dsn: ":memory:"
scrapers:
  priority: ["mock"]
metadata:
  translation:
    enabled: true
    provider: openai
    source_language: ja
    target_language: en
    fields:
      title: true
    openai:
      model: gpt-4
      api_key: test-key
  priority:
    id: ["mock"]
    content_id: ["mock"]
    title: ["mock"]
    description: ["mock"]
matching:
  extensions: [".mp4"]
  regex_enabled: false
`
	tmpFile := t.TempDir() + "/config.yaml"
	require.NoError(t, os.WriteFile(tmpFile, []byte(configContent), 0644))

	// Load config and create database
	cfg, err := config.Load(tmpFile)
	require.NoError(t, err)
	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	// Create cached movie with MATCHING hash
	matchingHash := cfg.Metadata.Translation.SettingsHash()

	cachedMovie := &models.Movie{
		ContentID: "test002",
		ID:        "TEST-002",
		Title:     "Cached Title",
		Translations: []models.MovieTranslation{
			{
				Language:     "en",
				Title:        "Cached Translation",
				SettingsHash: matchingHash,
				SourceName:   "translation-service",
			},
		},
	}
	movieRepo := database.NewMovieRepository(db)
	require.NoError(t, movieRepo.Create(context.TODO(), cachedMovie))

	// Setup command and dependencies
	cmd := scrape.NewCommand()
	registry := scraperutil.NewScraperRegistry()
	deps, err := commandutil.NewDependenciesWithOptions(cfg, &commandutil.DependenciesOptions{
		DB:              db,
		ScraperRegistry: registry,
	})
	require.NoError(t, err)
	defer func() { _ = deps.Close() }()

	// Run scrape - should hit cache, hash matches, NO re-translation
	movie, results, err := scrape.Run(context.Background(), cmd, []string{"TEST-002"}, tmpFile, deps)

	require.NoError(t, err)
	assert.NotNil(t, movie)
	assert.Len(t, results, 1, "Cache hit returns one synthesized scraper result")
	assert.NotEmpty(t, results[0].Source)

	// Verify: should have only 1 translation (no new one added)
	assert.Len(t, movie.Translations, 1, "should NOT add duplicate translation")
	assert.Equal(t, matchingHash, movie.Translations[0].SettingsHash)
}
