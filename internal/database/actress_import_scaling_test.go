package database

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Bound returned identity rows as the positive-DMM catalogue grows, including
// the canonical-name fallback. Query-plan checks below also guard empty-result
// scans, which a returned-row counter alone cannot detect.
func TestImportPositiveDMMEvidenceScalesLinearly(t *testing.T) {
	for _, candidates := range []int{0, 1} {
		for _, size := range []int{32, 64, 128} {
			t.Run(fmt.Sprintf("candidates_%d/rows_%d", candidates, size), func(t *testing.T) {
				db := newCreditTestDB(t)
				if candidates > 0 {
					require.NoError(t, db.Create(&models.Actress{DMMID: 900001, JapaneseName: "Unrelated candidate"}).Error)
					require.NoError(t, db.Create(&models.Actress{Verified: true, JapaneseName: "DMM-less owner"}).Error)
				}
				var rows, queries int64
				require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:import-scaling", func(tx *gorm.DB) {
					queries++
					if tx.Statement.Table == "actresses" {
						rows += tx.RowsAffected
					}
				}))
				for i := 0; i < size; i++ {
					incoming := models.Actress{DMMID: 10000 + i, JapaneseName: fmt.Sprintf("Imported %d", i)}
					require.NoError(t, NewActressRepository(db).ImportUpsert(t.Context(), &incoming))
				}
				require.NoError(t, db.Callback().Query().Remove("test:import-scaling"))
				if candidates == 0 {
					require.Zero(t, rows, "positive-only imports must not hydrate unrelated catalogue rows")
				}
				require.LessOrEqual(t, rows, int64((3*candidates+1)*size), "read each changed identity once plus bounded candidate evidence")
				require.LessOrEqual(t, queries, int64(11*size), "bounded per-row query count")
				t.Logf("%d imports: %d queries, %d identity rows", size, queries, rows)
			})
		}
	}
}

func TestImportEvidenceQueryPlans(t *testing.T) {
	db := newCreditTestDB(t)
	tests := []struct {
		query, index string
		args         []interface{}
	}{
		{"SELECT * FROM actresses WHERE verified = ? ORDER BY id", "idx_actresses_candidate_scan", []interface{}{false}},
		{"SELECT * FROM actresses WHERE verified = ? AND (dmm_id <= 0 OR dmm_id IS NULL) ORDER BY id", "idx_actresses_verified_dmmless_scan", []interface{}{true}},
		{"SELECT EXISTS(SELECT 1 FROM actresses WHERE dmm_id = 0)", "idx_actresses_dmm_lookup", nil},
		{"SELECT * FROM actresses WHERE dmm_id = ?", "idx_actresses_dmm_lookup", []interface{}{0}},
	}
	for _, tt := range tests {
		var plan []struct{ Detail string }
		require.NoError(t, db.Raw("EXPLAIN QUERY PLAN "+tt.query, tt.args...).Scan(&plan).Error)
		var details []string
		for _, step := range plan {
			details = append(details, step.Detail)
		}
		require.Contains(t, strings.Join(details, "\n"), tt.index)
	}
}

func TestQuarantineVerifiedDMMlessEligibility(t *testing.T) {
	for _, dmm := range []interface{}{nil, -1, 0, 100} {
		t.Run(fmt.Sprint(dmm), func(t *testing.T) {
			db := newCreditTestDB(t)
			owner := models.Actress{Verified: true, JapaneseName: "Canonical"}
			require.NoError(t, db.Create(&owner).Error)
			require.NoError(t, db.Model(&owner).UpdateColumn("dmm_id", dmm).Error)
			require.NoError(t, db.Create(&models.ActressAlias{AliasName: "Stage", CanonicalName: "Canonical"}).Error)
			candidate := models.Actress{DMMID: 200, JapaneseName: "Stage"}
			require.NoError(t, db.Create(&candidate).Error)
			require.NoError(t, db.Transaction(recomputeActressCandidateQuarantineTx))
			require.NoError(t, db.First(&candidate, candidate.ID).Error)
			require.Equal(t, dmm != 100, candidate.AmbiguityQuarantined)
			require.NoError(t, db.Model(&owner).UpdateColumn("dmm_id", 101).Error)
			require.NoError(t, db.Transaction(recomputeActressCandidateQuarantineTx))
			require.NoError(t, db.First(&candidate, candidate.ID).Error)
			require.False(t, candidate.AmbiguityQuarantined, "removing eligible alias ownership clears quarantine")
		})
	}
}

func TestImportQuarantineReadFailureRollsBackOnlyCurrentRow(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	first := models.Actress{DMMID: 100, JapaneseName: "First"}
	require.NoError(t, repo.ImportUpsert(t.Context(), &first))
	// Explicit pending repair keeps the oracle fault/cancellation path exercised.
	require.NoError(t, db.Exec("UPDATE actress_quarantine_state SET revision=revision+1").Error)
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:fail-candidates", func(tx *gorm.DB) {
		if tx.Statement.Table == "actresses" && strings.Contains(tx.Statement.SQL.String(), "verified") {
			tx.AddError(fmt.Errorf("injected candidate read failure"))
		}
	}))
	second := models.Actress{DMMID: 101, JapaneseName: "Second"}
	err := repo.ImportUpsert(t.Context(), &second)
	require.NoError(t, db.Callback().Query().Remove("test:fail-candidates"))
	require.ErrorContains(t, err, "injected candidate read failure")
	var count int64
	require.NoError(t, db.Model(&models.Actress{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, db.First(&models.Actress{}, first.ID).Error)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, repo.ImportUpsert(ctx, &models.Actress{DMMID: 102}), context.Canceled)
	require.NoError(t, db.Model(&models.Actress{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestImportEvidenceIndexesUpgradeReopenAndRollback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "import-indexes.db")
	db, err := New(&Config{Type: "sqlite", DSN: path, LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	provider := newMigrationProvider(t, sqlDB)
	_, err = provider.DownTo(t.Context(), 21)
	require.NoError(t, err)
	require.NoError(t, db.Exec(`INSERT INTO actresses (id,dmm_id,verified,first_name) VALUES
 (501,NULL,1,'Legacy null'),(502,-1,1,'Legacy negative'),(503,0,1,'Zero'),
 (504,0,0,'Candidate'),(505,12345,1,'Positive')`).Error)
	var before []models.Actress
	require.NoError(t, db.Order("id").Find(&before).Error)
	for cycle := 0; cycle < 2; cycle++ {
		_, err = provider.Up(t.Context())
		require.NoError(t, err)
		var after []models.Actress
		require.NoError(t, db.Order("id").Find(&after).Error)
		require.Equal(t, before, after, "indexes must not mutate legacy identities")
		var count int64
		require.NoError(t, db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name IN ('idx_actresses_dmm_lookup','idx_actresses_name_pair_lookup','idx_actresses_candidate_scan','idx_actresses_verified_dmmless_scan') AND sql NOT LIKE '%UNIQUE%'").Scan(&count).Error)
		require.EqualValues(t, 4, count)
		if cycle == 0 {
			_, err = provider.DownTo(t.Context(), 21)
			require.NoError(t, err)
		}
	}
	require.Error(t, db.Exec("INSERT INTO actresses(dmm_id) VALUES (12345)").Error, "existing positive-DMM uniqueness stays enforced")
	require.NoError(t, db.Exec("INSERT INTO actresses(dmm_id) VALUES (NULL),(-1),(0)").Error, "DMM-less duplicates remain legal")
	require.NoError(t, db.Close())
	db, err = New(&Config{Type: "sqlite", DSN: path, LogLevel: "silent"})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	var count int64
	require.NoError(t, db.Model(&models.Actress{}).Count(&count).Error)
	require.EqualValues(t, 8, count)
}

func TestImportEvidenceConcurrentRowsKeepQuarantine(t *testing.T) {
	db := newCreditTestDB(t)
	candidate := models.Actress{DMMID: 900001, JapaneseName: "Shared"}
	require.NoError(t, db.Create(&candidate).Error)
	require.NoError(t, db.Create(&models.Actress{Verified: true, JapaneseName: "Shared"}).Error)
	var wg sync.WaitGroup
	failures := make(chan error, 4)
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for row := 0; row < 8; row++ {
				err := retryOnLocked(func() error {
					incoming := models.Actress{DMMID: 1000 + worker*8 + row, JapaneseName: fmt.Sprintf("Imported %d %d", worker, row)}
					return NewActressRepository(db).ImportUpsert(t.Context(), &incoming)
				})
				if err != nil {
					failures <- err
					return
				}
			}
		}(worker)
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	var count int64
	require.NoError(t, db.Model(&models.Actress{}).Count(&count).Error)
	require.EqualValues(t, 34, count)
	require.NoError(t, db.First(&candidate, candidate.ID).Error)
	require.True(t, candidate.AmbiguityQuarantined)
}

func TestImportCancelAfterEmptyCandidateRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cancel.db")
	db, err := New(&Config{Type: "sqlite", DSN: path, LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	repo := NewActressRepository(db)
	first := models.Actress{DMMID: 100, JapaneseName: "First"}
	require.NoError(t, repo.ImportUpsert(t.Context(), &first))
	// Explicit pending repair keeps the oracle fault/cancellation path exercised.
	require.NoError(t, db.Exec("UPDATE actress_quarantine_state SET revision=revision+1").Error)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	seen := false
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:cancel", func(tx *gorm.DB) {
		if tx.Statement.Table == "actresses" && strings.Contains(tx.Statement.SQL.String(), "verified = ?") && tx.RowsAffected == 0 {
			seen = true
			cancel()
		}
	}))
	err = repo.ImportUpsert(ctx, &models.Actress{DMMID: 101, JapaneseName: "Second"})
	require.NoError(t, db.Callback().Query().Remove("test:cancel"))
	require.True(t, seen)
	require.Error(t, err)
	var rows []models.Actress
	require.NoError(t, db.Order("id").Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Equal(t, first.ID, rows[0].ID)
}
