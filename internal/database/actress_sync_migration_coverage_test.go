package database

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func migrationTaskFixture(t *testing.T, withTarget bool, sourceStatus, sourceScope, targetStatus, targetScope string) (*DB, models.ActressSyncTask, models.ActressSyncTask) {
	t.Helper()
	db := newDatabaseTestDB(t)
	now := time.Now().UTC()
	sourceJob := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobRunning, Scope: sourceScope, CreatedAt: now}
	sourceActressID := uint(20)
	sourceTask := models.ActressSyncTask{ID: uuid.NewString(), JobID: sourceJob.ID, ActressID: &sourceActressID, Status: sourceStatus, Stage: "resolving", DedupeKey: uuid.NewString(), Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now}
	require.NoError(t, db.Create(sourceJob).Error)
	require.NoError(t, db.Create(&sourceTask).Error)
	var targetTask models.ActressSyncTask
	if withTarget {
		targetJob := &models.ActressSyncJob{ID: uuid.NewString(), Status: models.ActressSyncJobPending, Scope: targetScope, CreatedAt: now}
		targetActressID := uint(10)
		targetTask = models.ActressSyncTask{ID: uuid.NewString(), JobID: targetJob.ID, ActressID: &targetActressID, Status: targetStatus, Stage: "queued", DedupeKey: uuid.NewString(), Messages: []string{}, UpdatedFields: []string{}, CreatedAt: now.Add(time.Second)}
		require.NoError(t, db.Create(targetJob).Error)
		require.NoError(t, db.Create(&targetTask).Error)
	}
	return db, sourceTask, targetTask
}

func forceMigrationUpdateError(t *testing.T, db *DB, call int) func() {
	t.Helper()
	name := "coverage:migrate-update:" + uuid.NewString()
	seen := 0
	require.NoError(t, db.DB.Callback().Update().After("gorm:update").Register(name, func(tx *gorm.DB) {
		seen++
		if seen == call {
			tx.AddError(errForcedActressCoverage)
		}
	}))
	return func() { require.NoError(t, db.DB.Callback().Update().Remove(name)) }
}

func forceMigrationTaskMoveError(t *testing.T, db *DB) func() {
	t.Helper()
	name := "coverage:migrate-move:" + uuid.NewString()
	require.NoError(t, db.DB.Callback().Update().After("gorm:update").Register(name, func(tx *gorm.DB) {
		fields, ok := tx.Statement.Dest.(map[string]any)
		if !ok {
			return
		}
		if _, exists := fields["updated_fields"]; exists {
			tx.AddError(errForcedActressCoverage)
		}
	}))
	return func() { require.NoError(t, db.DB.Callback().Update().Remove(name)) }
}

func TestMigrateActiveActressSyncTasksErrors(t *testing.T) {
	t.Run("source task query", func(t *testing.T) {
		db, _, _ := migrationTaskFixture(t, false, models.ActressSyncTaskRunning, "missing", "", "")
		remove := forceQueryErrorOnCall(t, db, 1)
		defer remove()
		require.ErrorIs(t, migrateActiveActressSyncTasksTx(db.DB, 10, 20), errForcedActressCoverage)
	})
	t.Run("source job query", func(t *testing.T) {
		db, _, _ := migrationTaskFixture(t, false, models.ActressSyncTaskRunning, "missing", "", "")
		remove := forceQueryErrorOnCall(t, db, 2)
		defer remove()
		require.ErrorIs(t, migrateActiveActressSyncTasksTx(db.DB, 10, 20), errForcedActressCoverage)
	})
	t.Run("target task query", func(t *testing.T) {
		db, _, _ := migrationTaskFixture(t, true, models.ActressSyncTaskRunning, "missing", models.ActressSyncTaskPending, "selected")
		remove := forceQueryErrorOnCall(t, db, 3)
		defer remove()
		require.ErrorIs(t, migrateActiveActressSyncTasksTx(db.DB, 10, 20), errForcedActressCoverage)
	})
	t.Run("target job query", func(t *testing.T) {
		db, _, _ := migrationTaskFixture(t, true, models.ActressSyncTaskRunning, "missing", models.ActressSyncTaskPending, "selected")
		remove := forceQueryErrorOnCall(t, db, 4)
		defer remove()
		require.ErrorIs(t, migrateActiveActressSyncTasksTx(db.DB, 10, 20), errForcedActressCoverage)
	})

	tests := []struct {
		name         string
		sourceStatus string
		sourceScope  string
		targetStatus string
		targetScope  string
		updateCall   int
	}{
		{"move without target", models.ActressSyncTaskRunning, "missing", "", "", 1},
		{"skip stronger target", models.ActressSyncTaskRunning, "selected", models.ActressSyncTaskPending, "missing", 1},
		{"move after skipping target", models.ActressSyncTaskRunning, "selected", models.ActressSyncTaskPending, "missing", 0},
		{"skip merged source", models.ActressSyncTaskPending, "missing", models.ActressSyncTaskPending, "selected", 1},
		{"skip weaker target", models.ActressSyncTaskPending, "selected", models.ActressSyncTaskPending, "missing", 0},
		{"move deferred", models.ActressSyncTaskRunning, "missing", models.ActressSyncTaskRunning, "selected", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db, _, _ := migrationTaskFixture(t, tc.targetStatus != "", tc.sourceStatus, tc.sourceScope, tc.targetStatus, tc.targetScope)
			var remove func()
			if tc.updateCall == 0 {
				remove = forceMigrationTaskMoveError(t, db)
			} else {
				remove = forceMigrationUpdateError(t, db, tc.updateCall)
			}
			defer remove()
			require.ErrorIs(t, migrateActiveActressSyncTasksTx(db.DB, 10, 20), errForcedActressCoverage)
		})
	}

	t.Run("refresh job", func(t *testing.T) {
		db, _, _ := migrationTaskFixture(t, true, models.ActressSyncTaskPending, "missing", models.ActressSyncTaskRunning, "selected")
		remove := forceQueryErrorOnCall(t, db, 5)
		defer remove()
		require.ErrorIs(t, migrateActiveActressSyncTasksTx(db.DB, 10, 20), errForcedActressCoverage)
	})
}
