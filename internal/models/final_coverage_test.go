package models

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

type queryResolvingScraper struct {
	query   string
	matched bool
}

func (s queryResolvingScraper) Name() string { return "resolver" }
func (s queryResolvingScraper) Search(context.Context, string) (*ScraperResult, error) {
	return nil, nil
}
func (s queryResolvingScraper) IsEnabled() bool                                { return true }
func (s queryResolvingScraper) GetURL(context.Context, string) (string, error) { return "", nil }
func (s queryResolvingScraper) Config() *ScraperSettings                       { return nil }
func (s queryResolvingScraper) Close() error                                   { return nil }
func (s queryResolvingScraper) ResolveSearchQuery(string) (string, bool)       { return s.query, s.matched }

func TestFormatActressNameRemainingOrders(t *testing.T) {
	assert.Equal(t, "First Last", FormatActressName(Actress{FirstName: "First", LastName: "Last"}, FormatActressNameOptions{FirstNameOrder: true}))
	assert.Equal(t, "First", FormatActressName(Actress{FirstName: "First"}, FormatActressNameOptions{FirstNameOrder: true}))
	assert.Equal(t, "Last", FormatActressName(Actress{LastName: "Last"}, FormatActressNameOptions{FirstNameOrder: true}))
	assert.Equal(t, "Last First", FormatActressName(Actress{FirstName: "First", LastName: "Last"}, FormatActressNameOptions{}))
	assert.Equal(t, "Last", FormatActressName(Actress{LastName: "Last"}, FormatActressNameOptions{}))
	assert.Equal(t, "First", FormatActressName(Actress{FirstName: "First"}, FormatActressNameOptions{}))
}

func TestSplitFullNameWhitespaceOnlyAndQueryResolution(t *testing.T) {
	first, last := SplitFullName("\u2003")
	assert.Empty(t, first)
	assert.Empty(t, last)

	assert.Equal(t, "mapped", mustResolvedQuery(t, queryResolvingScraper{query: " mapped ", matched: true}))
	for _, scraper := range []Scraper{
		queryResolvingScraper{query: "ignored", matched: false},
		queryResolvingScraper{query: "   ", matched: true},
	} {
		query, ok := ResolveSearchQueryForScraper(scraper, "input")
		assert.False(t, ok)
		assert.Empty(t, query)
	}
}

func mustResolvedQuery(t *testing.T, scraper Scraper) string {
	t.Helper()
	query, ok := ResolveSearchQueryForScraper(scraper, "input")
	assert.True(t, ok)
	return query
}
