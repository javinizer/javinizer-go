package mocks_test

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// TestMockMovieRepositoryExample demonstrates using generated mocks with expecter pattern
func TestMockMovieRepositoryExample(t *testing.T) {
	// Create mock repository using generated constructor
	mockRepo := mocks.NewMockMovieRepositoryInterface(t)

	// Expected movie data
	expectedMovie := &models.Movie{
		ID:          "IPX-123",
		ContentID:   "ipx00123",
		Title:       "Test Movie",
		Description: "Test Description",
	}

	// Set up expectation using EXPECT() fluent API (expecter pattern)
	mockRepo.EXPECT().
		FindByID("IPX-123").
		Return(expectedMovie, nil).
		Once()

	// Call the mock
	movie, err := mockRepo.FindByID("IPX-123")

	// Assertions
	assert.NoError(t, err)
	assert.Equal(t, expectedMovie, movie)
	assert.Equal(t, "IPX-123", movie.ID)
	assert.Equal(t, "Test Movie", movie.Title)

	// mockery automatically verifies all expectations were met via t.Cleanup()
}

// TestMockScraperExample demonstrates using the Scraper interface mock
func TestMockScraperExample(t *testing.T) {
	// Create mock scraper
	mockScraper := mocks.NewMockScraper(t)

	// Expected scraper result
	expectedResult := &models.ScraperResult{
		ID:          "IPX-123",
		Title:       "Test JAV Movie",
		Description: "Test Description",
	}

	// Set up expectation with EXPECT() pattern
	mockScraper.EXPECT().
		Search(mock.Anything, "IPX-123").
		Return(expectedResult, nil).
		Once()

	mockScraper.EXPECT().
		Name().
		Return("test-scraper").
		Maybe() // This expectation is optional

	// Use the mock
	result, err := mockScraper.Search(context.Background(), "IPX-123")

	// Verify
	assert.NoError(t, err)
	assert.Equal(t, expectedResult, result)
	assert.Equal(t, "IPX-123", result.ID)

	// Get scraper name (demonstrates Maybe() - won't fail if not called)
	name := mockScraper.Name()
	assert.Equal(t, "test-scraper", name)
}

// TestMockHTTPClientExample demonstrates mocking HTTP client for scraper tests
func TestMockHTTPClientExample(t *testing.T) {
	// This test demonstrates how scrapers can use mocked HTTP clients
	// to test without making real network requests

	mockClient := mocks.NewMockHTTPClient(t)

	// In real scraper tests, you would:
	// 1. Create a mock HTTP client
	// 2. Set up expectations for Do() method with specific requests
	// 3. Return mock responses
	// 4. Inject mock client into scraper
	// 5. Test scraper logic without network calls

	// Example expectation (would need actual *http.Request and *http.Response):
	// mockClient.EXPECT().
	// 	Do(mock.MatchedBy(func(req *http.Request) bool {
	// 		return req.URL.String() == "https://example.com/movie/IPX-123"
	// 	})).
	// 	Return(&http.Response{
	// 		StatusCode: 200,
	// 		Body:       io.NopCloser(strings.NewReader("<html>...</html>")),
	// 	}, nil).
	// 	Once()

	_ = mockClient // mockClient would be injected into scraper in real tests
}
