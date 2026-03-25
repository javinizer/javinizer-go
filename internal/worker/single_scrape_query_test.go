package worker

import (
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
)

type resolverTestScraper struct {
	name     string
	enabled  bool
	mappings map[string]string
}

func (s *resolverTestScraper) Name() string { return s.name }

func (s *resolverTestScraper) Search(id string) (*models.ScraperResult, error) { return nil, nil }

func (s *resolverTestScraper) GetURL(id string) (string, error) { return "", nil }

func (s *resolverTestScraper) IsEnabled() bool { return s.enabled }

func (s *resolverTestScraper) ResolveSearchQuery(input string) (string, bool) {
	key := strings.ToLower(strings.TrimSpace(input))
	query, ok := s.mappings[key]
	return query, ok
}

func (s *resolverTestScraper) Close() error { return nil }

func (s *resolverTestScraper) Config() *config.ScraperConfig {
	return &config.ScraperConfig{Enabled: s.enabled}
}

func TestResolveScraperQueryForInputs(t *testing.T) {
	scraper := &resolverTestScraper{
		name:    "resolver",
		enabled: true,
		mappings: map[string]string{
			"1pon-020326-001": "1pon_020326_001",
			"pon-020326":      "pon_020326",
		},
	}

	got, ok := resolveScraperQueryForInputs(scraper, "1PON-020326-001", "PON-020326", "PON-020326")
	if !ok {
		t.Fatal("expected resolver to match at least one input")
	}
	if got != "1pon_020326_001" {
		t.Fatalf("expected first matching input to win, got %q", got)
	}
}

func TestResolveScraperQueryForInputsNoMatch(t *testing.T) {
	scraper := &resolverTestScraper{
		name:     "resolver",
		enabled:  true,
		mappings: map[string]string{},
	}

	if got, ok := resolveScraperQueryForInputs(scraper, "", "UNKNOWN", ""); ok || got != "" {
		t.Fatalf("expected no match, got query=%q matched=%v", got, ok)
	}
}

func TestAppendUniqueInput(t *testing.T) {
	inputs := []string{}
	inputs = appendUniqueInput(inputs, "1PON-020326-001")
	inputs = appendUniqueInput(inputs, "1pon-020326-001")
	inputs = appendUniqueInput(inputs, "")

	if len(inputs) != 1 {
		t.Fatalf("expected case-insensitive de-duplication, got %d entries", len(inputs))
	}
	if inputs[0] != "1PON-020326-001" {
		t.Fatalf("unexpected stored input: %q", inputs[0])
	}
}
