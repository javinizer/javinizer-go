package movie

import (
	"context"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/poster"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

// WorkflowFunc returns a workflow instance for handling scrape and compare operations.
// This function type allows handlers to obtain a workflow without depending on
// *core.APIDeps directly — callers inject the factory at construction time.
type WorkflowFunc func() workflow.WorkflowInterface

// MovieDeps holds the dependencies that movie handlers need.
// Replaces the removed MovieService — handlers take this directly,
// matching the ActressDeps pattern used in the actress package.
type MovieDeps struct {
	MovieRepo   database.MovieRepositoryInterface
	WorkflowFn  WorkflowFunc
	PosterGen   poster.PosterGenerator
	AllowedDirs []string
}

// NewMovieDeps creates a MovieDeps from the given repository and options.
func NewMovieDeps(movieRepo database.MovieRepositoryInterface, opts ...MovieDepsOption) MovieDeps {
	d := MovieDeps{MovieRepo: movieRepo}
	for _, opt := range opts {
		opt(&d)
	}
	return d
}

// MovieDepsOption configures a MovieDeps instance.
type MovieDepsOption func(*MovieDeps)

// WithWorkflow sets the workflow factory function for scrape and compare operations.
func WithWorkflow(fn WorkflowFunc) MovieDepsOption {
	return func(d *MovieDeps) { d.WorkflowFn = fn }
}

// WithAllowedDirs sets the allowed directories for NFO path validation.
func WithAllowedDirs(dirs []string) MovieDepsOption {
	return func(d *MovieDeps) { d.AllowedDirs = dirs }
}

// WithPosterGen sets the poster generator for temp poster creation during scrape/rescrape.
func WithPosterGen(pg poster.PosterGenerator) MovieDepsOption {
	return func(d *MovieDeps) { d.PosterGen = pg }
}

// getWorkflow returns a workflow instance or nil if unavailable.
func (d MovieDeps) getWorkflow() workflow.WorkflowInterface {
	if d.WorkflowFn == nil {
		return nil
	}
	return d.WorkflowFn()
}

// getAllowedDirs returns the configured allowed directories.
func (d MovieDeps) getAllowedDirs() []string {
	return d.AllowedDirs
}

// FindByID returns a movie by ID.
func (d MovieDeps) FindByID(ctx context.Context, id string) (*models.Movie, error) {
	return d.MovieRepo.FindByID(ctx, id)
}

// List returns a paginated list of movies.
func (d MovieDeps) List(ctx context.Context, limit, offset int) ([]models.Movie, error) {
	return d.MovieRepo.List(ctx, limit, offset)
}
