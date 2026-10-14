package database

import (
	"errors"
	"fmt"
	"reflect"
	"sort"

	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
)

type capturedMovieRender struct {
	projection         movieRenderInputs
	renderGeneration   int64
	openCollisionCount int64
}

type movieRenderSnapshot map[string]capturedMovieRender

func uniqueContentIDs(contentIDs []string) []string {
	seen := make(map[string]struct{}, len(contentIDs))
	out := make([]string, 0, len(contentIDs))
	for _, contentID := range contentIDs {
		if contentID == "" {
			continue
		}
		if _, ok := seen[contentID]; ok {
			continue
		}
		seen[contentID] = struct{}{}
		out = append(out, contentID)
	}
	sort.Strings(out)
	return out
}

func movieContentIDsForActressesTx(tx *gorm.DB, actressIDs ...uint) ([]string, error) {
	if len(actressIDs) == 0 {
		return nil, nil
	}
	var contentIDs []string
	if err := tx.Raw(`
SELECT movie_content_id
FROM movie_credits
WHERE actress_id IN ?
UNION
SELECT movie_content_id
FROM movie_actresses
WHERE actress_id IN ?
  AND movie_content_id IS NOT NULL`, actressIDs, actressIDs).Scan(&contentIDs).Error; err != nil {
		return nil, wrapDBErr("snapshot render", "movies for actresses", err)
	}
	return uniqueContentIDs(contentIDs), nil
}

func movieContentIDForCreditTx(tx *gorm.DB, creditID uint) (string, error) {
	var contentID string
	if err := tx.Model(&models.MovieCredit{}).Where("id = ?", creditID).Pluck("movie_content_id", &contentID).Error; err != nil {
		return "", err
	}
	if contentID == "" {
		return "", fmt.Errorf("movie credit %d: %w", creditID, ErrNotFound)
	}
	return contentID, nil
}

func captureMovieRenderSnapshotsTx(tx *gorm.DB, contentIDs []string) (movieRenderSnapshot, error) {
	snapshots := make(movieRenderSnapshot, len(contentIDs))
	for _, contentID := range uniqueContentIDs(contentIDs) {
		movie, err := loadPersistedMovieForRenderComparison(tx, contentID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return nil, wrapDBErr("snapshot render", fmt.Sprintf("movie %s", contentID), err)
		}
		var openCollisionCount int64
		if err := tx.Model(&models.CreditCollision{}).Where("movie_content_id = ? AND status = ?", contentID, models.CollisionStatusOpen).Count(&openCollisionCount).Error; err != nil {
			return nil, wrapDBErr("snapshot render eligibility", fmt.Sprintf("movie %s", contentID), err)
		}
		snapshots[contentID] = capturedMovieRender{
			projection: artifactRenderProjection(movie), renderGeneration: movie.RenderGeneration, openCollisionCount: openCollisionCount,
		}
	}
	return snapshots, nil
}

func invalidateChangedMovieRenderInputsTx(tx *gorm.DB, before movieRenderSnapshot, contentIDs []string) error {
	for _, contentID := range uniqueContentIDs(contentIDs) {
		previous, ok := before[contentID]
		if !ok {
			continue
		}
		after, err := loadPersistedMovieForRenderComparison(tx, contentID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return wrapDBErr("reload render", fmt.Sprintf("movie %s", contentID), err)
		}
		var openCollisionCount int64
		if err := tx.Model(&models.CreditCollision{}).Where("movie_content_id = ? AND status = ?", contentID, models.CollisionStatusOpen).Count(&openCollisionCount).Error; err != nil {
			return wrapDBErr("reload render eligibility", fmt.Sprintf("movie %s", contentID), err)
		}
		if !reflect.DeepEqual(previous.projection, artifactRenderProjection(after)) || previous.openCollisionCount != openCollisionCount {
			beforeMovie := &models.Movie{ContentID: contentID, RenderGeneration: previous.renderGeneration}
			if err := invalidateMovieRenderGenerationTx(tx, beforeMovie, after); err != nil {
				return err
			}
		}
	}
	return nil
}

func mutateMovieRenderInputsTx(tx *gorm.DB, contentIDs []string, mutate func() error) error {
	contentIDs = uniqueContentIDs(contentIDs)
	before, err := captureMovieRenderSnapshotsTx(tx, contentIDs)
	if err != nil {
		return err
	}
	if err := mutate(); err != nil {
		return err
	}
	return invalidateChangedMovieRenderInputsTx(tx, before, contentIDs)
}
