package database

import (
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
)

type JobRepository struct {
	db *DB
}

func NewJobRepository(db *DB) *JobRepository {
	return &JobRepository{db: db}
}

func (r *JobRepository) Create(job *models.Job) error {
	return r.db.Create(job).Error
}

func (r *JobRepository) Update(job *models.Job) error {
	return r.db.Save(job).Error
}

func (r *JobRepository) FindByID(id string) (*models.Job, error) {
	var job models.Job
	err := r.db.First(&job, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *JobRepository) List() ([]models.Job, error) {
	var jobs []models.Job
	err := r.db.Order("started_at DESC").Find(&jobs).Error
	return jobs, err
}

func (r *JobRepository) Delete(id string) error {
	return r.db.Delete(&models.Job{}, "id = ?", id).Error
}

func (r *JobRepository) DeleteOrganizedOlderThan(date time.Time) error {
	return r.db.Where("status = ? AND organized_at < ?", "organized", date).Delete(&models.Job{}).Error
}
