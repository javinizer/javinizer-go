package nfo

import (
	"testing"
	"unicode/utf8"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestRemapParsedNFOTitleForMergePreservesUnicodeBaseTitle(t *testing.T) {
	for _, tc := range []struct {
		name, title, want string
	}{
		{"bracketed Japanese", "[abc-001] 題名", "題名"},
		{"bare Japanese", "ABC-001 題名", "題名"},
		{"bracketed emoji", "[ABC-001] 🎬題名", "🎬題名"},
		{"Unicode punctuation", "[ABC-001] 「題名」", "「題名」"},
		{"ASCII hyphen separator", "[ABC-001] - 題名", "題名"},
		{"ASCII colon separator", "ABC-001: 題名", "題名"},
		{"ASCII title", "[ABC-001] Original title", "Original title"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, scraped := range []struct {
				name, title string
			}{
				{"empty scraped title", ""},
				{"whitespace scraped title", " \t\n "},
			} {
				t.Run(scraped.name, func(t *testing.T) {
					movie := &models.Movie{ID: "ABC-001", Title: tc.title}
					RemapParsedNFOTitleForMerge(movie, scraped.title)
					require.Equal(t, tc.want, movie.Title)
					require.True(t, utf8.ValidString(movie.Title))
					require.Empty(t, movie.DisplayTitle, "a sparse scrape must retain the NFO base title")
					require.Equal(t, "ABC-001", movie.ID)
				})
			}
		})
	}
}
