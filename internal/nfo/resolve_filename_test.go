package nfo

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestResolveNFOFilename(t *testing.T) {
	movie := &models.Movie{
		ID:    "IPX-123",
		Title: "Test Movie",
	}

	t.Run("default template", func(t *testing.T) {
		result := ResolveNFOFilename(movie, "<ID>.nfo", false, false, false, "")
		assert.Equal(t, "IPX-123.nfo", result)
	})

	t.Run("custom template", func(t *testing.T) {
		result := ResolveNFOFilename(movie, "<ID> - <TITLE>.nfo", false, false, false, "")
		assert.Equal(t, "IPX-123 - Test Movie.nfo", result)
	})

	t.Run("multipart with part suffix", func(t *testing.T) {
		result := ResolveNFOFilename(movie, "<ID>.nfo", false, true, true, "-pt1")
		assert.Equal(t, "IPX-123-pt1.nfo", result)
	})

	t.Run("multipart without perFile ignores suffix", func(t *testing.T) {
		result := ResolveNFOFilename(movie, "<ID>.nfo", false, false, true, "-pt1")
		assert.Equal(t, "IPX-123.nfo", result)
	})

	t.Run("empty part suffix", func(t *testing.T) {
		result := ResolveNFOFilename(movie, "<ID>.nfo", false, true, true, "")
		assert.Equal(t, "IPX-123.nfo", result)
	})

	t.Run("invalid template falls back to ID", func(t *testing.T) {
		result := ResolveNFOFilename(movie, "<IF UNCLOSED>", false, false, false, "")
		assert.Contains(t, result, ".nfo")
	})

	t.Run("template with groupActress", func(t *testing.T) {
		m := &models.Movie{
			ID:    "IPX-456",
			Title: "Group Movie",
			Actresses: []models.Actress{
				{FirstName: "A1"}, {FirstName: "A2"}, {FirstName: "A3"},
			},
		}
		result := ResolveNFOFilename(m, "<ACTORS> - <ID>.nfo", true, false, false, "")
		assert.Contains(t, result, "IPX-456")
		assert.Contains(t, result, ".nfo")
	})

	t.Run("template producing empty sanitization falls back to ID", func(t *testing.T) {
		m := &models.Movie{ID: "ABC-789", Title: "Normal"}
		result := ResolveNFOFilename(m, "<UNKNOWN_TAG>.nfo", false, false, false, "")
		assert.Equal(t, "ABC-789.nfo", result)
	})

	t.Run("double .nfo extension prevented", func(t *testing.T) {
		result := ResolveNFOFilename(movie, "<ID>.nfo", false, false, false, "")
		assert.Equal(t, "IPX-123.nfo", result)
		assert.NotContains(t, result, ".nfo.nfo")
	})
}
