package models

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
)

// TestActressFullName tests the Actress.FullName() method
func TestActressFullName(t *testing.T) {
	tests := []struct {
		name     string
		actress  Actress
		expected string
	}{
		{
			name: "both first and last names",
			actress: Actress{
				FirstName: "Yui",
				LastName:  "Hatano",
			},
			expected: "Hatano Yui",
		},
		{
			name: "first name only",
			actress: Actress{
				FirstName:    "Yui",
				JapaneseName: "波多野結衣",
			},
			expected: "Yui",
		},
		{
			name: "japanese name only",
			actress: Actress{
				JapaneseName: "波多野結衣",
			},
			expected: "波多野結衣",
		},
		{
			name: "all three names",
			actress: Actress{
				FirstName:    "Yui",
				LastName:     "Hatano",
				JapaneseName: "波多野結衣",
			},
			expected: "Hatano Yui",
		},
		{
			name:     "empty actress",
			actress:  Actress{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.actress.FullName()
			if result != tt.expected {
				t.Errorf("FullName() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestActressInfoFullName tests the ActressInfo.FullName() method
func TestActressInfoFullName(t *testing.T) {
	tests := []struct {
		name     string
		actress  ActressInfo
		expected string
	}{
		{
			name: "both first and last names",
			actress: ActressInfo{
				FirstName: "Ai",
				LastName:  "Sayama",
			},
			expected: "Sayama Ai",
		},
		{
			name: "first name only",
			actress: ActressInfo{
				FirstName:    "Ai",
				JapaneseName: "佐山愛",
			},
			expected: "Ai",
		},
		{
			name: "japanese name only",
			actress: ActressInfo{
				JapaneseName: "佐山愛",
			},
			expected: "佐山愛",
		},
		{
			name: "all three names",
			actress: ActressInfo{
				FirstName:    "Ai",
				LastName:     "Sayama",
				JapaneseName: "佐山愛",
			},
			expected: "Sayama Ai",
		},
		{
			name:     "empty actress info",
			actress:  ActressInfo{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.actress.FullName()
			if result != tt.expected {
				t.Errorf("FullName() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestTableName tests the TableName() methods
func TestTableName(t *testing.T) {
	tests := []struct {
		name     string
		model    interface{ TableName() string }
		expected string
	}{
		{"Movie", Movie{}, "movies"},
		{"MovieTranslation", MovieTranslation{}, "movie_translations"},
		{"Actress", Actress{}, "actresses"},
		{"Genre", Genre{}, "genres"},
		{"MovieTag", MovieTag{}, "movie_tags"},
		{"History", History{}, "history"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.model.TableName()
			if result != tt.expected {
				t.Errorf("%s.TableName() = %q, want %q", tt.name, result, tt.expected)
			}
		})
	}
}

// MockScraper is a mock implementation of the Scraper interface for testing
type MockScraper struct {
	name    string
	enabled bool
}

func (m *MockScraper) Name() string {
	return m.name
}

func (m *MockScraper) Search(id string) (*ScraperResult, error) {
	return nil, nil
}

func (m *MockScraper) GetURL(id string) (string, error) {
	return "", nil
}

func (m *MockScraper) IsEnabled() bool {
	return m.enabled
}

func (m *MockScraper) Config() *config.ScraperConfig {
	return &config.ScraperConfig{
		Enabled: m.enabled,
	}
}

func (m *MockScraper) Close() error {
	return nil
}

type MockResolverScraper struct {
	*MockScraper
	matches bool
	query   string
}

func (m *MockResolverScraper) ResolveSearchQuery(input string) (string, bool) {
	if !m.matches {
		return "", false
	}
	return m.query, true
}

// TestNewScraperRegistry tests creation of a new registry
func TestNewScraperRegistry(t *testing.T) {
	registry := NewScraperRegistry()

	if registry == nil {
		t.Fatal("NewScraperRegistry() returned nil")
	}

	if registry.scrapers == nil {
		t.Error("ScraperRegistry.scrapers map not initialized")
	}

	// Should start empty
	if len(registry.scrapers) != 0 {
		t.Errorf("New registry should be empty, got %d scrapers", len(registry.scrapers))
	}
}

// TestScraperRegistryRegister tests registering scrapers
func TestScraperRegistryRegister(t *testing.T) {
	registry := NewScraperRegistry()

	scraper1 := &MockScraper{name: "scraper1", enabled: true}
	scraper2 := &MockScraper{name: "scraper2", enabled: true}

	registry.Register(scraper1)
	registry.Register(scraper2)

	if len(registry.scrapers) != 2 {
		t.Errorf("Expected 2 scrapers, got %d", len(registry.scrapers))
	}

	// Test that we can retrieve them
	retrieved, exists := registry.Get("scraper1")
	if !exists {
		t.Error("scraper1 should exist in registry")
	}
	if retrieved != scraper1 {
		t.Error("Retrieved scraper1 is not the same instance")
	}
}

// TestScraperRegistryGet tests getting scrapers by name
func TestScraperRegistryGet(t *testing.T) {
	registry := NewScraperRegistry()
	scraper := &MockScraper{name: "test-scraper", enabled: true}
	registry.Register(scraper)

	tests := []struct {
		name       string
		searchName string
		wantExists bool
	}{
		{
			name:       "existing scraper",
			searchName: "test-scraper",
			wantExists: true,
		},
		{
			name:       "non-existing scraper",
			searchName: "nonexistent",
			wantExists: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, exists := registry.Get(tt.searchName)
			if exists != tt.wantExists {
				t.Errorf("Get(%q) exists = %v, want %v", tt.searchName, exists, tt.wantExists)
			}
		})
	}
}

// TestScraperRegistryGetAll tests getting all scrapers
func TestScraperRegistryGetAll(t *testing.T) {
	registry := NewScraperRegistry()

	// Empty registry
	all := registry.GetAll()
	if len(all) != 0 {
		t.Errorf("Empty registry GetAll() = %d scrapers, want 0", len(all))
	}

	// Add scrapers
	scraper1 := &MockScraper{name: "scraper1", enabled: true}
	scraper2 := &MockScraper{name: "scraper2", enabled: false}
	scraper3 := &MockScraper{name: "scraper3", enabled: true}

	registry.Register(scraper1)
	registry.Register(scraper2)
	registry.Register(scraper3)

	all = registry.GetAll()
	if len(all) != 3 {
		t.Errorf("GetAll() = %d scrapers, want 3", len(all))
	}
}

// TestScraperRegistryGetEnabled tests getting only enabled scrapers
func TestScraperRegistryGetEnabled(t *testing.T) {
	registry := NewScraperRegistry()

	scraper1 := &MockScraper{name: "enabled1", enabled: true}
	scraper2 := &MockScraper{name: "disabled", enabled: false}
	scraper3 := &MockScraper{name: "enabled2", enabled: true}

	registry.Register(scraper1)
	registry.Register(scraper2)
	registry.Register(scraper3)

	enabled := registry.GetEnabled()

	if len(enabled) != 2 {
		t.Errorf("GetEnabled() = %d scrapers, want 2", len(enabled))
	}

	// Verify only enabled scrapers are returned
	for _, s := range enabled {
		if !s.IsEnabled() {
			t.Errorf("GetEnabled() returned disabled scraper: %s", s.Name())
		}
	}
}

// TestScraperRegistryGetByPriority tests getting scrapers by priority order
func TestScraperRegistryGetByPriority(t *testing.T) {
	registry := NewScraperRegistry()

	scraper1 := &MockScraper{name: "dmm", enabled: true}
	scraper2 := &MockScraper{name: "r18dev", enabled: true}
	scraper3 := &MockScraper{name: "javlibrary", enabled: false}

	registry.Register(scraper1)
	registry.Register(scraper2)
	registry.Register(scraper3)

	tests := []struct {
		name      string
		priority  []string
		wantLen   int
		wantOrder []string
	}{
		{
			name:     "empty priority - returns all enabled",
			priority: []string{},
			wantLen:  2,
		},
		{
			name:     "nil priority - returns all enabled",
			priority: nil,
			wantLen:  2,
		},
		{
			name:      "priority order respected",
			priority:  []string{"r18dev", "dmm"},
			wantLen:   2,
			wantOrder: []string{"r18dev", "dmm"},
		},
		{
			name:      "disabled scraper not included",
			priority:  []string{"javlibrary", "dmm"},
			wantLen:   1,
			wantOrder: []string{"dmm"},
		},
		{
			name:      "non-existent scraper ignored",
			priority:  []string{"nonexistent", "dmm"},
			wantLen:   1,
			wantOrder: []string{"dmm"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := registry.GetByPriority(tt.priority)

			if len(result) != tt.wantLen {
				t.Errorf("GetByPriority() = %d scrapers, want %d", len(result), tt.wantLen)
			}

			// Check order if specified
			if tt.wantOrder != nil {
				for i, scraper := range result {
					if scraper.Name() != tt.wantOrder[i] {
						t.Errorf("GetByPriority() position %d = %q, want %q", i, scraper.Name(), tt.wantOrder[i])
					}
				}
			}
		})
	}
}

func TestResolveSearchQueryForScraper(t *testing.T) {
	plain := &MockScraper{name: "plain", enabled: true}
	resolver := &MockResolverScraper{
		MockScraper: &MockScraper{name: "resolver", enabled: true},
		matches:     true,
		query:       "normalized-id",
	}

	if q, ok := ResolveSearchQueryForScraper(plain, "input"); ok || q != "" {
		t.Fatalf("expected plain scraper to have no resolver, got query=%q matched=%v", q, ok)
	}

	if q, ok := ResolveSearchQueryForScraper(resolver, "input"); !ok || q != "normalized-id" {
		t.Fatalf("expected resolver query to match, got query=%q matched=%v", q, ok)
	}
}

func TestScraperRegistryGetByPriorityForInput(t *testing.T) {
	registry := NewScraperRegistry()

	plainA := &MockScraper{name: "plain-a", enabled: true}
	matchingResolver := &MockResolverScraper{
		MockScraper: &MockScraper{name: "resolver-match", enabled: true},
		matches:     true,
		query:       "mapped-query",
	}
	nonMatchingResolver := &MockResolverScraper{
		MockScraper: &MockScraper{name: "resolver-no-match", enabled: true},
		matches:     false,
		query:       "",
	}

	registry.Register(plainA)
	registry.Register(matchingResolver)
	registry.Register(nonMatchingResolver)

	priority := []string{"plain-a", "resolver-no-match", "resolver-match"}

	ordered := registry.GetByPriorityForInput(priority, "1pon_020326_001")
	if len(ordered) != 3 {
		t.Fatalf("expected 3 scrapers, got %d", len(ordered))
	}

	// Matching resolver should be promoted to front.
	if ordered[0].Name() != "resolver-match" {
		t.Fatalf("expected resolver-match to be first, got %s", ordered[0].Name())
	}

	// If no resolver matches, ordering should remain unchanged.
	nonMatchingResolver.matches = false
	matchingResolver.matches = false
	ordered = registry.GetByPriorityForInput(priority, "unknown")
	for i, want := range priority {
		if ordered[i].Name() != want {
			t.Fatalf("expected order[%d]=%s, got %s", i, want, ordered[i].Name())
		}
	}
}
