package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
)

// ensureJobWritable rejects operation creation/mutation after retention has
// claimed the owning organized job. The query and the later write are used by
// callers whose write is already conditional or protected by BEGIN IMMEDIATE;
// this helper also keeps the ordinary repository paths fail-closed.
func ensureJobWritable(db *gorm.DB, jobID string) error {
	if jobID == "" {
		return nil
	}
	var row struct{ Status models.JobStatus }
	result := db.Model(&models.Job{}).Select("status").Where("id = ?", jobID).First(&row)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil
	}
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 && row.Status == pruningJobStatus {
		return ErrJobPruning
	}
	return nil
}

// ensureOperationWritableConn is the transaction-local form used by the
// BEGIN IMMEDIATE journal/non-journal writers. A claim that wins the writer
// lock before this check is therefore observed before any operation mutation.
func ensureOperationWritableConn(ctx context.Context, conn *sql.Conn, opID uint, allowMaintenance bool) error {
	if allowMaintenance && pruneMaintenance(ctx) {
		return nil
	}
	var status string
	err := conn.QueryRowContext(ctx, `
		SELECT jobs.status
		FROM batch_file_operations AS ops
		JOIN jobs ON jobs.id = ops.batch_job_id
		WHERE ops.id = ?`, opID).Scan(&status)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return fmt.Errorf("check operation %d pruning fence: %w", opID, err)
	}
	if models.JobStatus(status) == pruningJobStatus {
		return ErrJobPruning
	}
	return nil
}

// ensureOperationWritable is the repository-level operation-id form used by
// status updates whose SQL statement reports zero affected rows. The status
// lookup is only diagnostic after the conditional UPDATE; the UPDATE itself
// is the race-free fence.
func ensureOperationWritable(db *gorm.DB, opID uint) error {
	var row struct {
		Status models.JobStatus
	}
	result := db.Raw("SELECT jobs.status AS status FROM batch_file_operations AS ops JOIN jobs ON jobs.id = ops.batch_job_id WHERE ops.id = ?", opID).Scan(&row)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 && row.Status == pruningJobStatus {
		return ErrJobPruning
	}
	return nil
}
