package database

import (
	"context"
	"sort"
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
)

const projectionQueryVariableLimit = 30000

func projectionIDs(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func projectionChunks(values []string) [][]string {
	if len(values) == 0 {
		return nil
	}
	chunks := make([][]string, 0, (len(values)+projectionQueryVariableLimit-1)/projectionQueryVariableLimit)
	for start := 0; start < len(values); start += projectionQueryVariableLimit {
		end := start + projectionQueryVariableLimit
		if end > len(values) {
			end = len(values)
		}
		chunks = append(chunks, values[start:end])
	}
	return chunks
}

// FindAuthoritativeProjections loads movie cast projections in bounded query phases.
func (r *MovieRepository) FindAuthoritativeProjections(ctx context.Context, contentIDs, canonicalIDs []string) (*AuthoritativeMovieProjection, error) {
	projection := &AuthoritativeMovieProjection{
		ByContentID:   make(map[string]*models.Movie),
		ByCanonicalID: make(map[string]*models.Movie),
	}
	contentIDs = projectionIDs(contentIDs)
	canonicalIDs = projectionIDs(canonicalIDs)
	if len(contentIDs) == 0 && len(canonicalIDs) == 0 {
		return projection, ctx.Err()
	}

	type lookup struct {
		value     string
		canonical bool
	}
	lookups := make([]lookup, 0, len(contentIDs)+len(canonicalIDs))
	for _, id := range contentIDs {
		lookups = append(lookups, lookup{value: id})
	}
	for _, id := range canonicalIDs {
		lookups = append(lookups, lookup{value: id, canonical: true})
	}
	moviesByContentID := make(map[string]models.Movie)
	for start := 0; start < len(lookups); start += projectionQueryVariableLimit {
		end := start + projectionQueryVariableLimit
		if end > len(lookups) {
			end = len(lookups)
		}
		contentChunk, canonicalChunk := make([]string, 0), make([]string, 0)
		for _, item := range lookups[start:end] {
			if item.canonical {
				canonicalChunk = append(canonicalChunk, item.value)
			} else {
				contentChunk = append(contentChunk, item.value)
			}
		}
		query := r.GetDB().WithContext(ctx).Order("content_id ASC")
		switch {
		case len(contentChunk) > 0 && len(canonicalChunk) > 0:
			query = query.Where("content_id IN ? OR id IN ?", contentChunk, canonicalChunk)
		case len(contentChunk) > 0:
			query = query.Where("content_id IN ?", contentChunk)
		case len(canonicalChunk) > 0:
			query = query.Where("id IN ?", canonicalChunk)
		}
		var movies []models.Movie
		if err := query.Find(&movies).Error; err != nil {
			return nil, wrapDBErr("find", "authoritative movie projections", err)
		}
		for i := range movies {
			moviesByContentID[movies[i].ContentID] = movies[i]
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	foundContentIDs := make([]string, 0, len(moviesByContentID))
	for contentID := range moviesByContentID {
		foundContentIDs = append(foundContentIDs, contentID)
	}
	sort.Strings(foundContentIDs)
	creditsByMovie := make(map[string][]models.MovieCredit)
	actressIDs := make(map[uint]struct{})
	for _, chunk := range projectionChunks(foundContentIDs) {
		var credits []models.MovieCredit
		if err := r.GetDB().WithContext(ctx).
			Where("movie_content_id IN ? AND suppressed = ?", chunk, false).
			Order("movie_content_id ASC, order_index ASC, id ASC").
			Find(&credits).Error; err != nil {
			return nil, wrapDBErr("list", "authoritative movie projection credits", err)
		}
		for i := range credits {
			creditsByMovie[credits[i].MovieContentID] = append(creditsByMovie[credits[i].MovieContentID], credits[i])
			actressIDs[credits[i].ActressID] = struct{}{}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	orderedActressIDs := make([]uint, 0, len(actressIDs))
	for id := range actressIDs {
		orderedActressIDs = append(orderedActressIDs, id)
	}
	sort.Slice(orderedActressIDs, func(i, j int) bool { return orderedActressIDs[i] < orderedActressIDs[j] })
	actressesByID := make(map[uint]models.Actress, len(orderedActressIDs))
	for start := 0; start < len(orderedActressIDs); start += projectionQueryVariableLimit {
		end := start + projectionQueryVariableLimit
		if end > len(orderedActressIDs) {
			end = len(orderedActressIDs)
		}
		var actresses []models.Actress
		if err := r.GetDB().WithContext(ctx).Where("id IN ?", orderedActressIDs[start:end]).Order("id ASC").Find(&actresses).Error; err != nil {
			return nil, wrapDBErr("list", "authoritative movie projection actresses", err)
		}
		for i := range actresses {
			actressesByID[actresses[i].ID] = actresses[i]
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	for _, contentID := range foundContentIDs {
		movie := moviesByContentID[contentID]
		credits := creditsByMovie[contentID]
		movie.Credits = make([]models.MovieCredit, len(credits))
		movie.Actresses = make([]models.Actress, 0, len(credits))
		for i := range credits {
			movie.Credits[i] = credits[i]
			if actress, ok := actressesByID[credits[i].ActressID]; ok {
				identity := actress
				movie.Credits[i].Actress = &identity
				if identity.Verified {
					movie.Actresses = append(movie.Actresses, identity)
				}
			}
		}
		movieCopy := movie
		projection.ByContentID[movie.ContentID] = &movieCopy
		if _, exists := projection.ByCanonicalID[movie.ID]; !exists {
			projection.ByCanonicalID[movie.ID] = &movieCopy
		}
	}
	return projection, nil
}

var _ MovieProjectionRepositoryInterface = (*MovieRepository)(nil)
