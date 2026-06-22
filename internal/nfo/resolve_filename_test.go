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
		result := ResolveNFOFilename(nil, movie, NFONameConfig{FilenameTemplate: "<ID>.nfo"})
		assert.Equal(t, "IPX-123.nfo", result)
	})

	t.Run("custom template", func(t *testing.T) {
		result := ResolveNFOFilename(nil, movie, NFONameConfig{FilenameTemplate: "<ID> - <TITLE>.nfo"})
		assert.Equal(t, "IPX-123 - Test Movie.nfo", result)
	})

	t.Run("multipart with part suffix", func(t *testing.T) {
		result := ResolveNFOFilename(nil, movie, NFONameConfig{FilenameTemplate: "<ID>.nfo", PerFile: true, IsMultiPart: true, PartSuffix: "-pt1"})
		assert.Equal(t, "IPX-123-pt1.nfo", result)
	})

	t.Run("multipart without perFile ignores suffix", func(t *testing.T) {
		result := ResolveNFOFilename(nil, movie, NFONameConfig{FilenameTemplate: "<ID>.nfo", IsMultiPart: true, PartSuffix: "-pt1"})
		assert.Equal(t, "IPX-123.nfo", result)
	})

	t.Run("empty part suffix", func(t *testing.T) {
		result := ResolveNFOFilename(nil, movie, NFONameConfig{FilenameTemplate: "<ID>.nfo", PerFile: true, IsMultiPart: true})
		assert.Equal(t, "IPX-123.nfo", result)
	})

	t.Run("invalid template falls back to ID", func(t *testing.T) {
		result := ResolveNFOFilename(nil, movie, NFONameConfig{FilenameTemplate: "<IF UNCLOSED>"})
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
		result := ResolveNFOFilename(nil, m, NFONameConfig{FilenameTemplate: "<ACTORS> - <ID>.nfo", GroupActress: true})
		assert.Contains(t, result, "IPX-456")
		assert.Contains(t, result, ".nfo")
	})

	t.Run("template with groupActress and custom GroupActressName", func(t *testing.T) {
		m := &models.Movie{
			ID:    "IPX-789",
			Title: "Custom Group Movie",
			Actresses: []models.Actress{
				{FirstName: "A1"}, {FirstName: "A2"},
			},
		}
		result := ResolveNFOFilename(nil, m, NFONameConfig{FilenameTemplate: "<ACTORS> - <ID>.nfo", GroupActress: true, GroupActressName: "Multiple"})
		assert.Equal(t, "Multiple - IPX-789.nfo", result)
	})

	t.Run("template producing empty sanitization falls back to ID", func(t *testing.T) {
		m := &models.Movie{ID: "ABC-789", Title: "Normal"}
		result := ResolveNFOFilename(nil, m, NFONameConfig{FilenameTemplate: "<UNKNOWN_TAG>.nfo"})
		assert.Equal(t, "ABC-789.nfo", result)
	})

	t.Run("double .nfo extension prevented", func(t *testing.T) {
		result := ResolveNFOFilename(nil, movie, NFONameConfig{FilenameTemplate: "<ID>.nfo"})
		assert.Equal(t, "IPX-123.nfo", result)
		assert.NotContains(t, result, ".nfo.nfo")
	})

	t.Run("firstNameOrder affects actress name format", func(t *testing.T) {
		m := &models.Movie{
			ID:    "IPX-535",
			Title: "Test Movie",
			Actresses: []models.Actress{
				{FirstName: "Yui", LastName: "Hatano"},
			},
		}
		// LastName-first (default, firstNameOrder=false)
		resultLastName := ResolveNFOFilename(nil, m, NFONameConfig{FilenameTemplate: "<ACTORS> - <ID>.nfo"})
		assert.Equal(t, "Hatano Yui - IPX-535.nfo", resultLastName)

		// FirstName-first (firstNameOrder=true)
		resultFirstName := ResolveNFOFilename(nil, m, NFONameConfig{FilenameTemplate: "<ACTORS> - <ID>.nfo", FirstNameOrder: true})
		assert.Equal(t, "Yui Hatano - IPX-535.nfo", resultFirstName)
	})
}
