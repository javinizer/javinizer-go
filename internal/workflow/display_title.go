package workflow

import (
	"context"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/template"
)

// ApplyDisplayTitleFromSource applies the DisplayTitle template to the scraped movie using the
// provided titleSource for template context. This is the sole DisplayTitle entry point —
// used by Workflow.Scrape and Workflow.Apply. The templateEngine must be the shared
// workflow engine (language-aware), never a fresh default engine.
//
// nameCfg carries the actress-rendering options (group actress, JA preference, custom
// delimiter, etc.) so that <ACTORS>/<ACTRESSES> tags inside the display-title template
// resolve identically to folder/file/NFO templates. A zero-value NFONameConfig yields the
// plain name rendering (Latin, grouped only when configured).
func ApplyDisplayTitleFromSource(ctx context.Context, scraped *models.Movie, titleSource *models.Movie, displayTitleTmpl string, templateEngine template.EngineInterface, nameCfg nfo.NFONameConfig) {
	if scraped == nil || titleSource == nil || templateEngine == nil {
		if scraped != nil && scraped.DisplayTitle == "" {
			scraped.DisplayTitle = scraped.Title
		}
		return
	}
	if displayTitleTmpl != "" {
		displayCtx := template.NewContextFromMovie(scraped)
		displayCtx.Title = titleSource.Title
		if titleSource.OriginalTitle != "" {
			displayCtx.OriginalTitle = titleSource.OriginalTitle
		}
		// Thread the actress-rendering options so display-title templates using
		// <ACTORS>/<ACTRESSES> honor the configured delimiter/grouping/JA preference.
		displayCtx.GroupActress = nameCfg.GroupActress
		displayCtx.GroupActressName = nameCfg.GroupActressName
		displayCtx.GroupUnknownActressName = nameCfg.GroupUnknownActressName
		displayCtx.FirstNameOrder = nameCfg.FirstNameOrder
		displayCtx.ActressLanguageJa = nameCfg.ActressLanguageJA
		displayCtx.ActressDelimiter = nameCfg.ActressDelimiter
		if displayName, err := templateEngine.ExecuteWithContext(ctx, displayTitleTmpl, displayCtx); err == nil {
			scraped.DisplayTitle = displayName
		} else if scraped.DisplayTitle == "" {
			scraped.DisplayTitle = scraped.Title
		}
	} else if scraped.DisplayTitle == "" {
		scraped.DisplayTitle = scraped.Title
	}
}
