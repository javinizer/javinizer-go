package javstash_test

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraper/javstash"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/require"
)

func TestRegisteredJAVStashDefaultEndpointURLContract(t *testing.T) {
	for _, tc := range []struct{ name, baseURL, wantURL string }{
		{"omitted endpoint", "", "https://javstash.org/scenes/IPX-535"},
		{"whitespace endpoint", " \t\n ", "https://javstash.org/scenes/IPX-535"},
		{"explicit canonical endpoint", "https://javstash.org/graphql", "https://javstash.org/scenes/IPX-535"},
		{"supported www endpoint with surrounding space and slash", " https://www.javstash.org/graphql/ ", "https://www.javstash.org/scenes/IPX-535"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry := scraperutil.NewScraperRegistry()
			javstash.Register(registry)
			constructor, ok := registry.GetScraperConstructor("javstash")
			require.True(t, ok)
			// Use the public constructor directly: callers need not first merge module
			// defaults into their settings. The public URL observes the effective endpoint.
			settings := models.ScraperSettings{Enabled: true, APIKey: "fixture-not-a-real-credential", BaseURL: tc.baseURL, Timeout: 30}
			before := settings.Clone()
			scraper, err := constructor(scraperutil.ScraperDeps{Settings: settings})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, scraper.Close()); require.NoError(t, registry.Close()) })
			require.Equal(t, "javstash", scraper.Name())
			require.True(t, scraper.IsEnabled())
			got, err := scraper.GetURL(context.Background(), "IPX-535")
			require.NoError(t, err)
			require.Equal(t, tc.wantURL, got)
			require.Equal(t, before, settings, "construction must not mutate caller settings")
			for _, id := range []string{"", " \t\n "} {
				got, err := scraper.GetURL(context.Background(), id)
				require.EqualError(t, err, "javstash: id cannot be empty or whitespace")
				require.Empty(t, got)
			}
		})
	}
}
