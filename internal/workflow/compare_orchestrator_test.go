package workflow

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockNFOFieldMerger implements nfo.NFOFieldMerger for testing the compare orchestrator.
type mockNFOFieldMerger struct{}

func (m *mockNFOFieldMerger) MergeWithExistingNFO(_ *models.Movie, _ nfo.MergeWithExistingOptions) nfo.MergeWithExistingResult {
	return nfo.MergeWithExistingResult{}
}

func (m *mockNFOFieldMerger) ResolveNFOFilename(_ *models.Movie, _ nfo.NFONameConfig) string {
	return ""
}

func (m *mockNFOFieldMerger) ResolveNFOPath(_ string, _ *models.Movie, _ nfo.NFONameConfig, _ string) (string, []string) {
	return "", nil
}

// writeTestNFO writes a minimal NFO file to the in-memory filesystem for
// compare orchestrator tests that call nfo.ParseNFO directly.
func writeTestNFO(t *testing.T, fs afero.Fs, path string, movie *models.Movie) {
	t.Helper()
	nfoContent := "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<movie>\n"
	nfoContent += "  <id>" + movie.ID + "</id>\n"
	nfoContent += "  <title>" + movie.Title + "</title>\n"
	nfoContent += "</movie>"
	require.NoError(t, afero.WriteFile(fs, path, []byte(nfoContent), 0644))
}

// mockScraperInterface implements scrape.ScraperInterface for testing.
type mockScraperInterface struct {
	result *scrape.ScrapeResult
	err    error
}

func (m *mockScraperInterface) Scrape(_ context.Context, _ scrape.ScrapeCmd, _ scrape.ProgressFunc) (*scrape.ScrapeResult, error) {
	return m.result, m.err
}

// --- Compare orchestrator preset integration tests ---

func TestCompareOrchestrator_PresetConservative(t *testing.T) {
	fs := afero.NewMemMapFs()
	nfoData := &models.Movie{ID: "TEST-001", Title: "NFO Title"}
	writeTestNFO(t, fs, "/source/TEST-001.nfo", nfoData)

	scrapedData := &models.Movie{ID: "TEST-001", Title: "Scraped Title"}

	orch := newCompareOrchestrator(
		fs,
		&mockNFOFieldMerger{},
		&mockScraperInterface{
			result: &scrape.ScrapeResult{Movie: scrapedData},
		},
		nil,
	)

	result, err := orch.Execute(context.Background(), CompareCmd{
		MovieID:        "TEST-001",
		NFOPath:        "/source/TEST-001.nfo",
		ScalarStrategy: nfo.PreferScraper,
		ArrayStrategy:  false,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.NFOExists, "NFO should exist after parse")
}

func TestCompareOrchestrator_PresetAggressive(t *testing.T) {
	fs := afero.NewMemMapFs()
	nfoData := &models.Movie{ID: "TEST-001", Title: "NFO Title"}
	writeTestNFO(t, fs, "/source/TEST-001.nfo", nfoData)

	scrapedData := &models.Movie{ID: "TEST-001", Title: "Scraped Title"}

	orch := newCompareOrchestrator(
		fs,
		&mockNFOFieldMerger{},
		&mockScraperInterface{
			result: &scrape.ScrapeResult{Movie: scrapedData},
		},
		nil,
	)

	result, err := orch.Execute(context.Background(), CompareCmd{
		MovieID:        "TEST-001",
		NFOPath:        "/source/TEST-001.nfo",
		ScalarStrategy: nfo.PreferNFO,
		ArrayStrategy:  true,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestCompareOrchestrator_NoPreset_UsesProvidedStrategy(t *testing.T) {
	fs := afero.NewMemMapFs()
	nfoData := &models.Movie{ID: "TEST-001", Title: "NFO Title"}
	writeTestNFO(t, fs, "/source/TEST-001.nfo", nfoData)

	scrapedData := &models.Movie{ID: "TEST-001", Title: "Scraped Title"}

	orch := newCompareOrchestrator(
		fs,
		&mockNFOFieldMerger{},
		&mockScraperInterface{
			result: &scrape.ScrapeResult{Movie: scrapedData},
		},
		nil,
	)

	result, err := orch.Execute(context.Background(), CompareCmd{
		MovieID:        "TEST-001",
		NFOPath:        "/source/TEST-001.nfo",
		ScalarStrategy: nfo.PreferScraper,
		ArrayStrategy:  true,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
}

// --- noOpCompareOrchestrator tests ---

func TestNoOpCompareOrchestrator_Execute_ReturnsError(t *testing.T) {
	orch := noOpCompareOrchestrator{}
	result, err := orch.Execute(context.Background(), CompareCmd{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "compare not configured")
	assert.Nil(t, result)
}

// --- identifyDifferences tests ---

func TestIdentifyDifferences_NoDifferences(t *testing.T) {
	nfoMovie := &models.Movie{ID: "TEST-001", Title: "Same Title", Maker: "Same Maker"}
	scrapedMovie := &models.Movie{ID: "TEST-001", Title: "Same Title", Maker: "Same Maker"}
	mergedMovie := &models.Movie{ID: "TEST-001", Title: "Same Title", Maker: "Same Maker"}

	diffs := identifyDifferences(nfoMovie, scrapedMovie, mergedMovie)
	assert.Empty(t, diffs)
}

func TestIdentifyDifferences_TitleDiffers(t *testing.T) {
	nfoMovie := &models.Movie{ID: "TEST-001", Title: "NFO Title"}
	scrapedMovie := &models.Movie{ID: "TEST-001", Title: "Scraped Title"}
	mergedMovie := &models.Movie{ID: "TEST-001", Title: "Scraped Title"}

	diffs := identifyDifferences(nfoMovie, scrapedMovie, mergedMovie)
	require.Len(t, diffs, 1)
	assert.Equal(t, "title", diffs[0].Field)
	assert.Equal(t, "NFO Title", diffs[0].NFOValue)
	assert.Equal(t, "Scraped Title", diffs[0].ScrapedValue)
	assert.Equal(t, "Scraped Title", diffs[0].MergedValue)
}

func TestIdentifyDifferences_MultipleDifferences(t *testing.T) {
	nfoMovie := &models.Movie{
		ID:          "TEST-001",
		Title:       "NFO Title",
		Maker:       "NFO Maker",
		Description: "NFO Desc",
		Actresses:   []models.Actress{{FirstName: "A1"}},
		Genres:      []models.Genre{{Name: "G1"}},
	}
	scrapedMovie := &models.Movie{
		ID:          "TEST-001",
		Title:       "Scraped Title",
		Maker:       "Scraped Maker",
		Description: "Scraped Desc",
		Actresses:   []models.Actress{{FirstName: "B1"}, {FirstName: "B2"}},
		Genres:      []models.Genre{{Name: "H1"}, {Name: "H2"}},
	}
	mergedMovie := &models.Movie{
		ID:          "TEST-001",
		Title:       "Scraped Title",
		Maker:       "NFO Maker",
		Description: "Scraped Desc",
		Actresses:   []models.Actress{{FirstName: "A1"}, {FirstName: "B1"}, {FirstName: "B2"}},
		Genres:      []models.Genre{{Name: "G1"}, {Name: "H1"}, {Name: "H2"}},
	}

	diffs := identifyDifferences(nfoMovie, scrapedMovie, mergedMovie)
	// title, description, maker differ; actresses and genres differ by count
	assert.Len(t, diffs, 5)

	fields := make(map[string]FieldDifference, len(diffs))
	for _, d := range diffs {
		fields[d.Field] = d
	}
	assert.Contains(t, fields, "title")
	assert.Contains(t, fields, "description")
	assert.Contains(t, fields, "maker")
	assert.Contains(t, fields, "actresses")
	assert.Contains(t, fields, "genres")
}

func TestIdentifyDifferences_ArrayLengthDiffers(t *testing.T) {
	nfoMovie := &models.Movie{ID: "TEST-001", Actresses: []models.Actress{{FirstName: "A1"}}}
	scrapedMovie := &models.Movie{ID: "TEST-001", Actresses: []models.Actress{{FirstName: "B1"}, {FirstName: "B2"}}}
	mergedMovie := &models.Movie{ID: "TEST-001", Actresses: []models.Actress{{FirstName: "A1"}, {FirstName: "B1"}, {FirstName: "B2"}}}

	diffs := identifyDifferences(nfoMovie, scrapedMovie, mergedMovie)
	require.Len(t, diffs, 1)
	assert.Equal(t, "actresses", diffs[0].Field)
	assert.Equal(t, "1 actresses: A1", diffs[0].NFOValue)
	assert.Equal(t, "2 actresses: B1, B2", diffs[0].ScrapedValue)
	assert.Equal(t, "3 actresses: A1, B1, B2", diffs[0].MergedValue)
}

func TestCompareOrchestrator_DifferencesPopulated(t *testing.T) {
	fs := afero.NewMemMapFs()
	nfoData := &models.Movie{ID: "TEST-001", Title: "NFO Title"}
	writeTestNFO(t, fs, "/source/TEST-001.nfo", nfoData)

	scrapedData := &models.Movie{ID: "TEST-001", Title: "Scraped Title"}

	orch := newCompareOrchestrator(
		fs,
		&mockNFOFieldMerger{},
		&mockScraperInterface{
			result: &scrape.ScrapeResult{Movie: scrapedData},
		},
		nil,
	)

	result, err := orch.Execute(context.Background(), CompareCmd{
		MovieID:        "TEST-001",
		NFOPath:        "/source/TEST-001.nfo",
		ScalarStrategy: nfo.PreferScraper,
		ArrayStrategy:  false,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	// The orchestrator should have populated Differences
	assert.NotEmpty(t, result.Differences, "CompareResult.Differences should be populated by the orchestrator")
	assert.Equal(t, "title", result.Differences[0].Field)
	assert.Equal(t, "NFO Title", result.Differences[0].NFOValue)
	assert.Equal(t, "Scraped Title", result.Differences[0].ScrapedValue)
}
