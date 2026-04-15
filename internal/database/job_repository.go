package database

import (
	"errors"
	"fmt"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
)

type JobRepository struct {
	db *DB
}

func NewJobRepository(db *DB) *JobRepository {
	return &JobRepository{db: db}
}

func (r *JobRepository) Create(job *models.Job) error {
	if err := r.db.Create(job).Error; err != nil {
		return wrapDBErr("create", fmt.Sprintf("job %s", job.ID), err)
	}
	return nil
}

func (r *JobRepository) Update(job *models.Job) error {
	if err := r.db.Save(job).Error; err != nil {
		return wrapDBErr("update", fmt.Sprintf("job %s", job.ID), err)
	}
	return nil
}

func (r *JobRepository) FindByID(id string) (*models.Job, error) {
	var job models.Job
	err := r.db.First(&job, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("find job %s: %w", id, ErrNotFound)
		}
		return nil, wrapDBErr("find", fmt.Sprintf("job %s", id), err)
	}
	return &job, nil
}

func (r *JobRepository) List() ([]models.Job, error) {
	var jobs []models.Job
	err := r.db.Order("started_at DESC").Find(&jobs).Error
	if err != nil {
		return nil, wrapDBErr("find", "jobs", err)
	}
	return jobs, nil
}

func (r *JobRepository) Delete(id string) error {
	if err := r.db.Delete(&models.Job{}, "id = ?", id).Error; err != nil {
		return wrapDBErr("delete", fmt.Sprintf("job %s", id), err)
	}
	return nil
}

func (r *JobRepository) DeleteOrganizedOlderThan(date time.Time) error {
	if err := r.db.Where("status = ? AND organized_at < ?", "organized", date).Delete(&models.Job{}).Error; err != nil {
		return wrapDBErr("delete", "organized jobs", err)
	}
	return nil
}
