package downloader

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/stretchr/testify/require"
)

// Retain the real engine for every method except the supported Execute seam.
type fallbackErrorEngine struct {
	template.EngineInterface
	calls int
}

func (e *fallbackErrorEngine) Execute(string, *template.Context) (string, error) {
	e.calls++
	return "discarded-on-error", errors.New("template execution failed")
}

func TestMediaPathResolverSanitizedFallbackNames(t *testing.T) {
	for _, tc := range []struct {
		name, id, wantID string
	}{
		{"sanitized ID", " SAMPLE/001? ", "SAMPLE-001"},
		{"unknown ID", " . *?\x00 ", "unknown"},
		{"empty ID", "", "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			movie := &models.Movie{ID: tc.id, TrailerURL: "https://media.invalid/trailer.webm"}
			ctx := template.NewContextFromMovie(movie)
			// This is a nonempty, valid literal template whose output becomes
			// empty only after filename sanitization, not a stubbed sanitizer.
			format := " . *?\x00 "
			resolver := NewMediaPathResolver(organizer.MediaFormatConfig{
				PosterFormat: format, FanartFormat: format, TrailerFormat: format,
			}, nil)
			for _, folder := range []string{"", filepath.Join("library", "movie")} {
				require.Equal(t, filepath.Join(folder, tc.wantID+"-poster.jpg"), resolver.ResolvePosterPath(movie, nil, true, ctx, folder))
				require.Equal(t, filepath.Join(folder, tc.wantID+"-fanart.jpg"), resolver.ResolveFanartPath(movie, nil, true, ctx, folder))
				require.Equal(t, filepath.Join(folder, tc.wantID+"-trailer.webm"), resolver.ResolveTrailerPath(movie, true, ctx, folder))
			}
			require.Equal(t, tc.id, ctx.ID, "fallback naming must not rewrite the input context")
		})
	}
}

func TestMediaPathResolverTemplateFailureFallbackNames(t *testing.T) {
	for _, executionError := range []bool{false, true} {
		name := "empty template"
		if executionError {
			name = "execution error"
		}
		t.Run(name, func(t *testing.T) {
			movie := &models.Movie{ID: "SAMPLE/002?", TrailerURL: "https://media.invalid/trailer"}
			ctx := template.NewContextFromMovie(movie)
			var engine template.EngineInterface = template.NewEngine()
			failing := &fallbackErrorEngine{EngineInterface: engine}
			if executionError {
				engine = failing
			}
			resolver := NewMediaPathResolver(organizer.MediaFormatConfig{}, engine)
			require.Equal(t, "SAMPLE-002-poster.jpg", resolver.ResolvePosterPath(movie, nil, true, ctx, ""))
			require.Equal(t, "SAMPLE-002-fanart.jpg", resolver.ResolveFanartPath(movie, nil, true, ctx, ""))
			require.Equal(t, "SAMPLE-002-trailer.mp4", resolver.ResolveTrailerPath(movie, true, ctx, ""))
			if executionError {
				require.Equal(t, 3, failing.calls)
			}
		})
	}
}

func TestMediaPathResolverScreenshotSanitizedFallbackNames(t *testing.T) {
	for _, tc := range []struct {
		name    string
		padding int
		want    []string
	}{
		{"padded", 3, []string{"fanart001.jpg", "fanart002.jpg", "fanart003.jpg"}},
		{"unpadded", 0, []string{"fanart1.jpg", "fanart2.jpg", "fanart3.jpg"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			movie := &models.Movie{ID: "SAMPLE-003", Screenshots: []string{"one", "two", "three"}}
			ctx := template.NewContextFromMovie(movie)
			ctx.Index = 42
			resolver := NewMediaPathResolver(organizer.MediaFormatConfig{
				ScreenshotFormat: " . *?\x00 ", ScreenshotPadding: tc.padding,
			}, nil)
			require.Equal(t, tc.want, resolver.ResolveScreenshotNames(movie, true, ctx))
			require.Equal(t, 42, ctx.Index, "per-screenshot indices must not mutate the caller's context")
			require.Empty(t, resolver.ResolveScreenshotNames(movie, false, ctx))
		})
	}
}
