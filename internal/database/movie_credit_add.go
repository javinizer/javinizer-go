package database

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
)

// ErrMovieCreditAmbiguous prevents an explicit edit from choosing between movies.
var ErrMovieCreditAmbiguous = errors.New("movie ID is ambiguous")

// ErrMovieCreditSuppressed prevents an add from silently undoing suppression.
var ErrMovieCreditSuppressed = errors.New("credit is suppressed; restore it explicitly before adding")

// ErrMovieCreditUnverified requires candidates to be verified before attachment.
var ErrMovieCreditUnverified = errors.New("actress must be verified before adding a user credit")

// AddUserCredit adds an explicit, scrape-resistant credit without replacing the
// movie's cast. Existing credits retain their display overrides and ordering.
// This updates database render state only; it does not rewrite existing files.
func (r *MovieCreditRepository) AddUserCredit(ctx context.Context, movieID string, actressID uint) (*models.MovieCredit, error) {
	movieID = strings.TrimSpace(movieID)
	if movieID == "" || actressID == 0 {
		return nil, fmt.Errorf("add user credit: movie ID and positive actress ID are required: %w", ErrInvalidLookup)
	}
	var result models.MovieCredit
	err := retryOnLocked(func() error {
		result = models.MovieCredit{}
		return r.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			// Acquire SQLite write admission before any lookup/snapshot. Unlike SELECT
			// FOR UPDATE (ignored by SQLite), even a zero-row UPDATE holds the writer
			// lock, preventing stale read/modify/write decisions across independent pools.
			if err := tx.Exec("UPDATE movies SET content_id = content_id WHERE 0").Error; err != nil {
				return err
			}
			var matches []models.Movie
			if err := tx.Where("content_id = ? OR id = ?", movieID, movieID).Limit(2).Find(&matches).Error; err != nil {
				return err
			}
			if len(matches) == 0 {
				return fmt.Errorf("movie %q: %w", movieID, ErrNotFound)
			}
			if len(matches) != 1 {
				return fmt.Errorf("movie %q: %w", movieID, ErrMovieCreditAmbiguous)
			}
			movie := matches[0]
			if strings.TrimSpace(movie.ContentID) == "" {
				return fmt.Errorf("movie %q has no content ID: %w", movieID, ErrInvalidLookup)
			}
			var actress models.Actress
			if err := tx.First(&actress, actressID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return fmt.Errorf("actress %d: %w", actressID, ErrNotFound)
				}
				return err
			}
			if !actress.Verified {
				return fmt.Errorf("actress %d: %w", actressID, ErrMovieCreditUnverified)
			}
			return mutateMovieRenderInputsTx(tx, []string{movie.ContentID}, func() error {
				if err := preserveLegacyCastCreditsTx(tx, movie.ContentID); err != nil {
					return err
				}
				var credit models.MovieCredit
				err := tx.Where("movie_content_id = ? AND actress_id = ?", movie.ContentID, actressID).First(&credit).Error
				switch {
				case err == nil:
					if credit.Suppressed {
						return fmt.Errorf("credit %d: %w", credit.ID, ErrMovieCreditSuppressed)
					}
					if credit.Origin != string(models.CreditOriginUser) {
						if err := tx.Model(&credit).Updates(map[string]any{"origin": string(models.CreditOriginUser)}).Error; err != nil {
							return err
						}
					}
				case errors.Is(err, gorm.ErrRecordNotFound):
					var nextOrder int
					if err := tx.Model(&models.MovieCredit{}).Where("movie_content_id = ?", movie.ContentID).Select("COALESCE(MAX(order_index), -1) + 1").Scan(&nextOrder).Error; err != nil {
						return err
					}
					credit = models.MovieCredit{MovieContentID: movie.ContentID, ActressID: actressID, Origin: string(models.CreditOriginUser), CreditedName: actress.FullName(), CreditedJapaneseName: actress.JapaneseName, ReportedThumbURL: actress.ThumbURL, OrderIndex: nextOrder}
					if err := r.upsertDeferredTx(tx, &credit); err != nil {
						return err
					}
				default:
					return err
				}
				if err := tx.Exec("INSERT OR IGNORE INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?)", movie.ContentID, actressID).Error; err != nil {
					return err
				}
				if err := recomputeActressCandidateQuarantineTx(tx); err != nil {
					return err
				}
				// Return the committed operation's snapshot, not a post-commit re-read.
				return tx.Preload("Actress").First(&result, credit.ID).Error
			})
		})
	})
	if err != nil {
		return nil, wrapDBErr("add", "user movie credit", err)
	}
	return &result, nil
}

// Preserve projection-only legacy cast members when introducing authoritative
// credits. Existing credits (including suppression/override state) are untouched.
func preserveLegacyCastCreditsTx(tx *gorm.DB, contentID string) error {
	var actresses []models.Actress
	if err := tx.Model(&models.Actress{}).
		Joins("JOIN movie_actresses ma ON ma.actress_id = actresses.id").
		Where("ma.movie_content_id = ? AND NOT EXISTS (SELECT 1 FROM movie_credits mc WHERE mc.movie_content_id = ma.movie_content_id AND mc.actress_id = ma.actress_id)", contentID).
		Order("actresses.id ASC").Find(&actresses).Error; err != nil {
		return err
	}
	if len(actresses) == 0 {
		return nil
	}
	var nextOrder int
	if err := tx.Model(&models.MovieCredit{}).Where("movie_content_id = ?", contentID).Select("COALESCE(MAX(order_index), -1) + 1").Scan(&nextOrder).Error; err != nil {
		return err
	}
	for i, actress := range actresses {
		credit := models.MovieCredit{MovieContentID: contentID, ActressID: actress.ID, CreditedName: actress.FullName(), CreditedJapaneseName: actress.JapaneseName, ReportedThumbURL: actress.ThumbURL, Source: "legacy", Origin: string(models.CreditOriginUser), LegacyInferred: true, OrderIndex: nextOrder + i}
		if err := tx.Create(&credit).Error; err != nil {
			return err
		}
	}
	return nil
}
