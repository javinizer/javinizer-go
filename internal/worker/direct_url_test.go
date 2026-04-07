package worker

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
)

type mockDirectURLScraper struct {
	name         string
	enabled      bool
	scrapeResult *models.ScraperResult
	scrapeError  error
	canHandleURL bool
}

func (m *mockDirectURLScraper) Name() string { return m.name }
func (m *mockDirectURLScraper) Search(id string) (*models.ScraperResult, error) {
	return nil, errors.New("not implemented")
}
func (m *mockDirectURLScraper) GetURL(id string) (string, error) {
	return "", errors.New("not implemented")
}
func (m *mockDirectURLScraper) IsEnabled() bool                             { return m.enabled }
func (m *mockDirectURLScraper) Config() *config.ScraperSettings             { return nil }
func (m *mockDirectURLScraper) Close() error                                { return nil }
func (m *mockDirectURLScraper) CanHandleURL(url string) bool                { return m.canHandleURL }
func (m *mockDirectURLScraper) ExtractIDFromURL(url string) (string, error) { return "test-id", nil }
func (m *mockDirectURLScraper) ScrapeURL(url string) (*models.ScraperResult, error) {
	return m.scrapeResult, m.scrapeError
}

func TestScraperSearchWithURL_Success(t *testing.T) {
	expectedResult := &models.ScraperResult{
		Source: "test",
		ID:     "TEST-001",
		Title:  "Test Movie",
	}

	scraper := &mockDirectURLScraper{
		name:         "test",
		enabled:      true,
		scrapeResult: expectedResult,
		scrapeError:  nil,
	}

	ctx := context.Background()
	result, err := scraperSearchWithURL(ctx, scraper, "https://example.com/test")

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if result.ID != expectedResult.ID {
		t.Errorf("expected ID %s, got %s", expectedResult.ID, result.ID)
	}
}

func TestScraperSearchWithURL_Error(t *testing.T) {
	expectedError := models.NewScraperNotFoundError("test", "page not found")

	scraper := &mockDirectURLScraper{
		name:         "test",
		enabled:      true,
		scrapeResult: nil,
		scrapeError:  expectedError,
	}

	ctx := context.Background()
	result, err := scraperSearchWithURL(ctx, scraper, "https://example.com/test")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if result != nil {
		t.Errorf("expected nil result, got %+v", result)
	}

	scraperErr, ok := models.AsScraperError(err)
	if !ok {
		t.Fatalf("expected ScraperError, got %T", err)
	}
	if scraperErr.Kind != models.ScraperErrorKindNotFound {
		t.Errorf("expected NotFound kind, got %s", scraperErr.Kind)
	}
}

func TestScraperSearchWithURL_NormalizeMediaURLs(t *testing.T) {
	result := &models.ScraperResult{
		Source:    "test",
		ID:        "TEST-001",
		Title:     "Test Movie",
		PosterURL: "https://pics.dmm.co.jp/test/ps.jpg",
	}

	scraper := &mockDirectURLScraper{
		name:         "test",
		enabled:      true,
		scrapeResult: result,
		scrapeError:  nil,
	}

	ctx := context.Background()
	gotResult, err := scraperSearchWithURL(ctx, scraper, "https://example.com/test")

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if gotResult.PosterURL != "https://pics.dmm.co.jp/test/pl.jpg" {
		t.Errorf("expected normalized poster URL (pl.jpg), got: %s", gotResult.PosterURL)
	}
}

func TestScraperSearchWithURL_ContextCancellation(t *testing.T) {
	scraper := &mockDirectURLScraper{
		name:         "test",
		enabled:      true,
		scrapeResult: nil,
		scrapeError:  nil,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := scraperSearchWithURL(ctx, scraper, "https://example.com/test")

	if result != nil {
		t.Errorf("expected nil result, got %+v", result)
	}
	if err != context.Canceled {
		t.Errorf("expected context.Canceled error, got: %v", err)
	}
}

func TestScraperSearchWithURL_PanicRecovery(t *testing.T) {
	panicScraper := &mockPanicScraper{}

	ctx := context.Background()
	result, err := scraperSearchWithURL(ctx, panicScraper, "https://example.com/test")

	if result != nil {
		t.Errorf("expected nil result, got %+v", result)
	}
	if err == nil {
		t.Fatal("expected error from panic, got nil")
	}
	if !strings.Contains(err.Error(), "panic") {
		t.Errorf("expected panic error message, got: %v", err)
	}
}

type mockPanicScraper struct {
	mockDirectURLScraper
}

func (m *mockPanicScraper) ScrapeURL(url string) (*models.ScraperResult, error) {
	panic("intentional test panic")
}
