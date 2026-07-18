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

func TestApplyDisplayTitleFromSource_EmptyTemplate_UpdatesDisplayTitleWhenTitleChanges(t *testing.T) {
	m := &models.Movie{ID: "TEST-001", Title: "New Title", DisplayTitle: "Old Title"}
	ApplyDisplayTitleFromSource(context.Background(), m, m, "", template.NewEngine(), nfo.NFONameConfig{})
	assert.Equal(t, "New Title", m.DisplayTitle, "with no template, DisplayTitle tracks Title unconditionally")
}

// TestStepDisplayTitle_NoTemplate_OverwritesStaleDisplayTitle covers the P1
// finding: when no display_title template is configured, stepDisplayTitle
// must unconditionally set DisplayTitle = Title (not only when DisplayTitle
// is empty), so a stale DisplayTitle from a prior scrape is overwritten.
func TestStepDisplayTitle_NoTemplate_OverwritesStaleDisplayTitle(t *testing.T) {
	movie := &models.Movie{ID: "TEST-001", Title: "Edited Title", DisplayTitle: "Stale Old Title"}
	state := &applyPipelineState{movie: movie}
	steps := &stepCompletion{}

	o := &applyOrchImpl{
		applyCfg:       ApplyConfig{DisplayTitle: ""},
		templateEngine: nil,
	}

	err := o.stepDisplayTitle(context.Background(), ApplyCmd{Movie: movie}, state, steps)
	assert.NoError(t, err)
	assert.Equal(t, "Edited Title", state.movie.DisplayTitle,
		"with no template, stepDisplayTitle must overwrite stale DisplayTitle with Title")
	assert.True(t, steps.DisplayTitle, "step should be marked complete")
}

func TestWorkflowFactory_RenderDisplayTitle_NilReceiver(t *testing.T) {
	var f *WorkflowFactory
	assert.Equal(t, "", f.RenderDisplayTitle(context.Background(), &models.Movie{ID: "TEST-001", Title: "Hello"}))
}

func TestWorkflowFactory_RenderDisplayTitle_NilMovie(t *testing.T) {
	factory := &WorkflowFactory{
		fc: workflowFactoryConfig{
			ApplyCfg:       ApplyConfig{DisplayTitle: "[<ID>] <TITLE>"},
			TemplateEngine: template.NewEngine(),
		},
	}
	assert.Equal(t, "", factory.RenderDisplayTitle(context.Background(), nil))
}

func TestWorkflowFactory_RenderDisplayTitle_RendersTemplate(t *testing.T) {
	factory := &WorkflowFactory{
		fc: workflowFactoryConfig{
			ApplyCfg: ApplyConfig{
				DisplayTitle: "[<ID>] <TITLE>",
				NFONameCfg:   nfo.NFONameConfig{},
			},
			TemplateEngine: template.NewEngine(),
		},
	}
	rendered := factory.RenderDisplayTitle(context.Background(), &models.Movie{ID: "TEST-001", Title: "Hello"})
	assert.Equal(t, "[TEST-001] Hello", rendered)
}
