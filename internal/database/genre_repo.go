package database

import (
	"fmt"

	"github.com/javinizer/javinizer-go/internal/models"
)

// GenreRepository provides database operations for genres
type GenreRepository struct {
	db *DB
}

// NewGenreRepository creates a new genre repository
func NewGenreRepository(db *DB) *GenreRepository {
	return &GenreRepository{db: db}
}

// FindOrCreate finds a genre or creates a new one
func (r *GenreRepository) FindOrCreate(name string) (*models.Genre, error) {
	var genre models.Genre
	err := r.db.FirstOrCreate(&genre, models.Genre{Name: name}).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("genre %s", name), err)
	}
	return &genre, nil
}

// List returns all genres
func (r *GenreRepository) List() ([]models.Genre, error) {
	var genres []models.Genre
	err := r.db.Find(&genres).Error
	if err != nil {
		return nil, wrapDBErr("find", "genres", err)
	}
	return genres, nil
}
