package template

import "github.com/javinizer/javinizer-go/internal/models"

// RenderOptions is the shared actress/name policy for output templates.
// Construction options select the names; the remaining options format them.
// Output-specific titles, multipart data, and fallbacks stay with the caller.
type RenderOptions struct {
	ContextOptions
	GroupActress            bool
	GroupActressMin         int
	GroupActressName        string
	GroupUnknownActressName string
	UnknownActressMode      models.UnknownActressMode
	ActressLanguageJa       bool
	ActressDelimiter        string
}

// NewContext builds a movie context and applies its rendering policy.
func (opts RenderOptions) NewContext(movie *models.Movie) *Context {
	ctx := NewContextFromMovieWithOptions(movie, opts.ContextOptions)
	opts.ApplyTo(ctx)
	return ctx
}

// ApplyTo sets rendering policy without rebuilding constructor-selected names.
// RenderCredits and UseCreditedName apply only during context construction.
func (opts RenderOptions) ApplyTo(ctx *Context) {
	ctx.GroupActress = opts.GroupActress
	ctx.GroupActressMin = opts.GroupActressMin
	ctx.GroupActressName = opts.GroupActressName
	ctx.GroupUnknownActressName = opts.GroupUnknownActressName
	ctx.UnknownActressMode = opts.UnknownActressMode
	ctx.FirstNameOrder = opts.FirstNameOrder
	ctx.ActressLanguageJa = opts.ActressLanguageJa
	ctx.ActressDelimiter = opts.ActressDelimiter
}
