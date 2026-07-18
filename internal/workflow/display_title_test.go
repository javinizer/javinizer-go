package workflow

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/stretchr/testify/assert"
)

// TestApplyDisplayTitleFromSource_ActressLanguageJa verifies the display-title
// template threading honors the actressLanguageJa flag for <ACTORS>.
// Regression for the threading gap found by code review (ported from worker).
func TestApplyDisplayTitleFromSource_ActressLanguageJa(t *testing.T) {
	movie := &models.Movie{
		ID: "IPX-123",
		Actresses: []models.Actress{
			{FirstName: "Yui", LastName: "Hatano", JapaneseName: "波多野結衣"},
		},
	}

	t.Run("Latin when flag is false", func(t *testing.T) {
		m := *movie
		ApplyDisplayTitleFromSource(context.Background(), &m, &m, "<ACTORS>",
			template.NewEngine(), nfo.NFONameConfig{FirstNameOrder: false, ActressLanguageJA: false, ActressDelimiter: ", "})
		assert.Equal(t, "Hatano Yui", m.DisplayTitle)
	})

	t.Run("Japanese when flag is true", func(t *testing.T) {
		m := *movie
		ApplyDisplayTitleFromSource(context.Background(), &m, &m, "<ACTORS>",
			template.NewEngine(), nfo.NFONameConfig{ActressLanguageJA: true, ActressDelimiter: ", "})
		assert.Equal(t, "波多野結衣", m.DisplayTitle)
	})
}

// TestApplyDisplayTitleFromSource_EmptyTemplate_UpdatesDisplayTitleWhenTitleChanges
// covers D4: with no display_title template, DisplayTitle tracks Title
// unconditionally (not only when DisplayTitle is empty), so a stale
// DisplayTitle never persists after Title changes.
func TestApplyDisplayTitleFromSource_EmptyTemplate_UpdatesDisplayTitleWhenTitleChanges(t *testing.T) {
	m := &models.Movie{ID: "TEST-001", Title: "New Title", DisplayTitle: "Old Title"}
	ApplyDisplayTitleFromSource(context.Background(), m, m, "", template.NewEngine(), nfo.NFONameConfig{})
	assert.Equal(t, "New Title", m.DisplayTitle, "with no template, DisplayTitle tracks Title unconditionally")
}
