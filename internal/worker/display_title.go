package worker

import (
	"context"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
)

func applyDisplayTitle(ctx context.Context, job *BatchJob, cfg *config.Config, movie *models.Movie, titleSource *models.Movie) {
	if cfg != nil && cfg.Metadata.NFO.DisplayTitle != "" {
		displayTmplEngine := job.TemplateEngine()
		displayCtx := template.NewContextFromMovie(movie)
		displayCtx.Title = titleSource.Title
		if displayName, err := displayTmplEngine.ExecuteWithContext(ctx, cfg.Metadata.NFO.DisplayTitle, displayCtx); err == nil {
			movie.DisplayTitle = displayName
		} else if movie.DisplayTitle == "" {
			movie.DisplayTitle = movie.Title
		}
	} else if movie.DisplayTitle == "" {
		movie.DisplayTitle = movie.Title
	}
}

func ApplyDisplayTitle(ctx context.Context, movie *models.Movie, titleSource *models.Movie, displayTitleTmpl string, templateEngine *template.Engine) {
	if displayTitleTmpl != "" {
		displayCtx := template.NewContextFromMovie(movie)
		displayCtx.Title = titleSource.Title
		if displayName, err := templateEngine.ExecuteWithContext(ctx, displayTitleTmpl, displayCtx); err == nil {
			movie.DisplayTitle = displayName
		} else if movie.DisplayTitle == "" {
			movie.DisplayTitle = movie.Title
		}
	} else if movie.DisplayTitle == "" {
		movie.DisplayTitle = movie.Title
	}
}
