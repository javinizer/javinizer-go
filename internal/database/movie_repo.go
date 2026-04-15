package database

import (
	"errors"
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
)

type MovieRepository struct {
	db *DB
}

func NewMovieRepository(db *DB) *MovieRepository {
	return &MovieRepository{db: db}
}

func movieEntityID(movie *models.Movie) string {
	if movie.ContentID != "" {
		return movie.ContentID
	}
	return movie.ID
}

func (r *MovieRepository) Create(movie *models.Movie) error {
	if err := r.db.Create(movie).Error; err != nil {
		return wrapDBErr("create", fmt.Sprintf("movie %s", movieEntityID(movie)), err)
	}
	return nil
}

func (r *MovieRepository) Update(movie *models.Movie) error {
	if err := r.db.Save(movie).Error; err != nil {
		return wrapDBErr("update", fmt.Sprintf("movie %s", movieEntityID(movie)), err)
	}
	return nil
}

func (r *MovieRepository) Upsert(movie *models.Movie) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if strings.TrimSpace(movie.ContentID) == "" {
			if strings.TrimSpace(movie.ID) == "" {
				return fmt.Errorf("content_id is required when using ContentID as primary key")
			}
			movie.ContentID = strings.ToLower(strings.ReplaceAll(movie.ID, "-", ""))
		}

		movie.Actresses = filterIdentifiableActresses(movie.Actresses)

		var existing models.Movie
		var existingFound bool
		if movie.ContentID != "" {
			err := tx.Select("content_id", "created_at").First(&existing, "content_id = ?", movie.ContentID).Error
			if err == nil {
				existingFound = true
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return wrapDBErr("find", fmt.Sprintf("movie %s", movie.ContentID), err)
			}
		}
		if !existingFound && movie.ID != "" {
			err := tx.Select("content_id", "created_at").First(&existing, "id = ?", movie.ID).Error
			if err == nil {
				existingFound = true
				movie.ContentID = existing.ContentID
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return wrapDBErr("find", fmt.Sprintf("movie %s", movie.ID), err)
			}
		}

		if !existingFound {
			if err := tx.Omit("Actresses", "Genres").Create(movie).Error; err != nil {
				if errors.Is(err, gorm.ErrDuplicatedKey) {
					var existingMovie models.Movie
					if loadErr := tx.Select("created_at").First(&existingMovie, "content_id = ?", movie.ContentID).Error; loadErr == nil {
						movie.CreatedAt = existingMovie.CreatedAt
						if err := r.saveMovieWithAssociations(tx, movie); err != nil {
							return wrapDBErr("save duplicate", fmt.Sprintf("movie %s", movie.ContentID), err)
						}
						return nil
					}
				}
				return wrapDBErr("create", fmt.Sprintf("movie %s", movie.ContentID), err)
			}
		} else {
			movie.CreatedAt = existing.CreatedAt
		}

		if err := r.ensureGenresExistTx(tx, movie.Genres); err != nil {
			return wrapDBErr("ensure genres", fmt.Sprintf("for movie %s", movie.ContentID), err)
		}
		if err := r.ensureActressesExistTx(tx, movie.Actresses); err != nil {
			return wrapDBErr("ensure actresses", fmt.Sprintf("for movie %s", movie.ContentID), err)
		}

		translations := movie.Translations
		movie.Translations = nil
		if err := upsertMovieCore(tx, r.db, movie, translations); err != nil {
			return wrapDBErr("save", fmt.Sprintf("movie %s", movie.ContentID), err)
		}
		return nil
	})
}

func (r *MovieRepository) saveMovieWithAssociations(tx *gorm.DB, movie *models.Movie) error {
	if err := r.ensureGenresExistTx(tx, movie.Genres); err != nil {
		return err
	}
	if err := r.ensureActressesExistTx(tx, movie.Actresses); err != nil {
		return err
	}

	translations := movie.Translations
	movie.Translations = nil
	return upsertMovieCore(tx, r.db, movie, translations)
}

func (r *MovieRepository) ensureGenresExistTx(tx *gorm.DB, genres []models.Genre) error {
	for i := range genres {
		var existing models.Genre
		err := tx.Where("name = ?", genres[i].Name).First(&existing).Error
		if err == nil {
			genres[i] = existing
		} else if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := raceRetryCreate(tx, &genres[i], func(tx *gorm.DB) error {
				var found models.Genre
				if err := tx.Where("name = ?", genres[i].Name).First(&found).Error; err != nil {
					return err
				}
				genres[i] = found
				return nil
			}); err != nil {
				return err
			}
		} else {
			return err
		}
	}
	return nil
}

func (r *MovieRepository) mergeActressData(existing *models.Actress, new models.Actress) bool {
	needsUpdate := false

	if new.ThumbURL != "" && existing.ThumbURL == "" {
		existing.ThumbURL = new.ThumbURL
		needsUpdate = true
	}

	if new.FirstName != "" && existing.FirstName == "" {
		existing.FirstName = new.FirstName
		needsUpdate = true
	}
	if new.LastName != "" && existing.LastName == "" {
		existing.LastName = new.LastName
		needsUpdate = true
	}

	return needsUpdate
}

func (r *MovieRepository) ensureActressesExistTx(tx *gorm.DB, actresses []models.Actress) error {
	for i := range actresses {
		var existing models.Actress
		var err error

		if actresses[i].DMMID != 0 {
			err = tx.Where("dmm_id = ?", actresses[i].DMMID).First(&existing).Error
		} else if actresses[i].JapaneseName != "" {
			err = tx.Where("japanese_name = ?", actresses[i].JapaneseName).First(&existing).Error
		} else if actresses[i].FirstName != "" || actresses[i].LastName != "" {
			if actresses[i].FirstName != "" && actresses[i].LastName != "" {
				err = tx.Where("first_name = ? AND last_name = ?", actresses[i].FirstName, actresses[i].LastName).First(&existing).Error
			} else if actresses[i].FirstName != "" {
				err = tx.Where("first_name = ?", actresses[i].FirstName).First(&existing).Error
			} else {
				err = tx.Where("last_name = ?", actresses[i].LastName).First(&existing).Error
			}
		} else {
			continue
		}

		if err == nil {
			if r.mergeActressData(&existing, actresses[i]) {
				if err := tx.Save(&existing).Error; err != nil {
					return err
				}
			}
			actresses[i] = existing
		} else if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := raceRetryCreate(tx, &actresses[i], func(tx *gorm.DB) error {
				var found models.Actress
				var findErr error
				if actresses[i].DMMID != 0 {
					findErr = tx.Where("dmm_id = ?", actresses[i].DMMID).First(&found).Error
				} else if actresses[i].JapaneseName != "" {
					findErr = tx.Where("japanese_name = ?", actresses[i].JapaneseName).First(&found).Error
				} else if actresses[i].FirstName != "" && actresses[i].LastName != "" {
					findErr = tx.Where("first_name = ? AND last_name = ?", actresses[i].FirstName, actresses[i].LastName).First(&found).Error
				} else if actresses[i].FirstName != "" {
					findErr = tx.Where("first_name = ?", actresses[i].FirstName).First(&found).Error
				} else {
					findErr = tx.Where("last_name = ?", actresses[i].LastName).First(&found).Error
				}
				if findErr != nil {
					return findErr
				}
				if r.mergeActressData(&found, actresses[i]) {
					if saveErr := tx.Save(&found).Error; saveErr != nil {
						return saveErr
					}
				}
				actresses[i] = found
				return nil
			}); err != nil {
				return err
			}
		} else {
			return err
		}
	}
	return nil
}

func (r *MovieRepository) FindByID(id string) (*models.Movie, error) {
	var movie models.Movie
	err := r.db.Preload("Actresses").Preload("Genres").Preload("Translations", func(db *gorm.DB) *gorm.DB { return db.Order("language ASC") }).First(&movie, "id = ?", id).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("movie by id %s", id), err)
	}
	return &movie, nil
}

func (r *MovieRepository) FindByContentID(contentID string) (*models.Movie, error) {
	var movie models.Movie
	err := r.db.Preload("Actresses").Preload("Genres").Preload("Translations", func(db *gorm.DB) *gorm.DB { return db.Order("language ASC") }).First(&movie, "content_id = ?", contentID).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("movie %s", contentID), err)
	}
	return &movie, nil
}

func (r *MovieRepository) Delete(id string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var movie models.Movie
		if err := tx.Model(&models.Movie{}).
			Select("content_id").
			Where("id = ?", id).
			First(&movie).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return wrapDBErr("find", fmt.Sprintf("movie for delete %s", id), err)
		}

		if movie.ContentID == "" {
			return nil
		}

		stub := &models.Movie{ContentID: movie.ContentID}
		if err := tx.Model(stub).Association("Actresses").Clear(); err != nil {
			return wrapDBErr("clear", fmt.Sprintf("actresses for movie %s", movie.ContentID), err)
		}
		if err := tx.Model(stub).Association("Genres").Clear(); err != nil {
			return wrapDBErr("clear", fmt.Sprintf("genres for movie %s", movie.ContentID), err)
		}

		if err := tx.Delete(&models.MovieTranslation{}, "movie_id = ?", movie.ContentID).Error; err != nil {
			return wrapDBErr("delete", fmt.Sprintf("translations for movie %s", movie.ContentID), err)
		}

		if err := tx.Delete(&models.MovieTag{}, "movie_id = ?", movie.ContentID).Error; err != nil {
			return wrapDBErr("delete", fmt.Sprintf("tags for movie %s", movie.ContentID), err)
		}

		if err := tx.Delete(&models.Movie{}, "content_id = ?", movie.ContentID).Error; err != nil {
			return wrapDBErr("delete", fmt.Sprintf("movie %s", movie.ContentID), err)
		}
		return nil
	})
}

func (r *MovieRepository) List(limit, offset int) ([]models.Movie, error) {
	var movies []models.Movie
	err := r.db.Preload("Actresses").Preload("Genres").Limit(limit).Offset(offset).Find(&movies).Error
	if err != nil {
		return nil, wrapDBErr("find", "movies", err)
	}
	return movies, nil
}
