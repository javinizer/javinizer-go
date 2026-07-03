package scrape

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestBuildFieldSourcesFromCachedMovie_V5_AllFields(t *testing.T) {
	movie := &models.Movie{
		ID:          "ABC-123",
		Title:       "Test Title",
		Maker:       "Test Maker",
		Description: "Test Desc",
		SourceName:  "r18dev",
	}

	sources := buildFieldSourcesFromCachedMovie(movie)
	assert.NotEmpty(t, sources)
	// Should have entries for non-empty fields (lowercase keys)
	if _, ok := sources["id"]; !ok {
		t.Error("expected id in field sources")
	}
	if _, ok := sources["title"]; !ok {
		t.Error("expected title in field sources")
	}
}

func TestBuildFieldSourcesFromCachedMovie_V5_NilMovie(t *testing.T) {
	sources := buildFieldSourcesFromCachedMovie(nil)
	assert.Empty(t, sources)
}

func TestBuildActressSourcesFromCachedMovie_V5_WithActresses(t *testing.T) {
	movie := &models.Movie{
		ID: "ABC-123",
		Actresses: []models.Actress{
			{JapaneseName: "Test1", DMMID: 100},
			{JapaneseName: "Test2"},
		},
	}

	sources := buildActressSourcesFromCachedMovie(movie)
	assert.NotEmpty(t, sources)
}

func TestBuildActressSourcesFromCachedMovie_V5_EmptySourceName(t *testing.T) {
	movie := &models.Movie{
		ID: "ABC-123",
		Actresses: []models.Actress{
			{JapaneseName: "Test1"},
		},
	}

	sources := buildActressSourcesFromCachedMovie(movie)
	// Should handle empty source name gracefully
	for key := range sources {
		assert.NotEmpty(t, key, "source key should not be empty")
	}
}

func TestActressSourceKeyFromModel_V5_DMMID(t *testing.T) {
	actress := models.Actress{DMMID: 100, JapaneseName: "Test"}
	key := ActressSourceKey(actress)
	assert.Equal(t, "dmmid:100", key)
}

func TestActressSourceKeyFromModel_V5_JapaneseName(t *testing.T) {
	actress := models.Actress{JapaneseName: "田中"}
	key := ActressSourceKey(actress)
	assert.Contains(t, key, "田中")
}

func TestScrapeCmd_V5_Fields(t *testing.T) {
	cmd := ScrapeCmd{
		MovieID:          "ABC-123",
		SelectedScrapers: []string{"r18dev"},
		ForceRefresh:     true,
	}

	assert.Equal(t, "ABC-123", cmd.MovieID)
	assert.True(t, cmd.ForceRefresh)
}

func TestConfigFromAppConfig_V5(t *testing.T) {
	// Just verify the function exists and handles nil
	result := ConfigFromAppConfig(nil)
	assert.Nil(t, result)
}
