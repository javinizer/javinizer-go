package batch

import (
	"context"
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
)

func findAuthoritativeMovie(ctx context.Context, repo database.MovieRepositoryInterface, snapshot *models.Movie, matcherAlias string) (*models.Movie, error) {
	if repo == nil || snapshot == nil {
		return nil, nil
	}
	if contentID := strings.TrimSpace(snapshot.ContentID); contentID != "" {
		movie, err := repo.FindByContentID(ctx, contentID)
		if err == nil && movie != nil {
			return movie, nil
		}
		if err != nil && !database.IsNotFound(err) {
			return nil, err
		}
	}
	if canonicalID := strings.TrimSpace(snapshot.ID); canonicalID != "" {
		movie, err := repo.FindByID(ctx, canonicalID)
		if err == nil && movie != nil && strings.TrimSpace(movie.ID) == canonicalID {
			return movie, nil
		}
		if err != nil && !database.IsNotFound(err) {
			return nil, err
		}
	}
	alias := strings.TrimSpace(matcherAlias)
	if alias == "" || alias == strings.TrimSpace(snapshot.ID) {
		return nil, nil
	}
	movie, err := repo.FindByID(ctx, alias)
	if err != nil {
		if database.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return movie, nil
}

func authoritativePreviewMovie(ctx context.Context, deps *core.APIDeps, jobID, resultID string, override *contracts.MovieView) (*models.Movie, *previewResolveError) {
	job, ok := deps.GetJobStore().GetBatchJob(jobID)
	if !ok {
		return nil, &previewResolveError{Status: 404, Err: jobNotFoundMessage}
	}
	result, _, found := lookupResultByResultID(job, resultID)
	if !found || result == nil || result.Movie == nil {
		return nil, &previewResolveError{Status: 404, Err: fmt.Sprintf("Result %s not found in job", resultID)}
	}
	authority, err := findAuthoritativeMovie(ctx, deps.Repos.MovieRepo, result.Movie, result.FileMatchInfo.MovieID)
	if err != nil {
		return nil, &previewResolveError{Status: 500, Err: fmt.Sprintf("failed to load authoritative movie: %v", err)}
	}
	if authority == nil {
		authority = result.Movie
	}
	if override == nil {
		copyMovie := *authority
		copyMovie.Actresses = append([]models.Actress(nil), authority.Actresses...)
		copyMovie.Credits = append([]models.MovieCredit(nil), authority.Credits...)
		return &copyMovie, nil
	}
	movie := contracts.MovieViewToModel(override)
	movie.ContentID = authority.ContentID
	if strings.TrimSpace(movie.ID) == "" {
		movie.ID = authority.ID
	}
	movie.Actresses = append([]models.Actress(nil), authority.Actresses...)
	movie.Credits = append([]models.MovieCredit(nil), authority.Credits...)
	movie.CreatedAt = authority.CreatedAt
	movie.UpdatedAt = authority.UpdatedAt
	return movie, nil
}
