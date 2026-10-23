package database

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestCanonicalIndexAuditPositiveFastPathEligibility(t *testing.T) {
	db := newCreditTestDB(t)
	require.NoError(t, db.Exec("INSERT INTO actresses(id,dmm_id,japanese_name,verified) VALUES(100,1,'Shared',1),(101,NULL,'Shared',1),(102,-1,'Shared',0)").Error)
	require.NoError(t, db.Exec("CREATE TRIGGER audit_fastpath_sync_fail BEFORE INSERT ON actress_canonical_keys BEGIN SELECT RAISE(ABORT, 'audit fastpath index failure'); END").Error)
	lookup := func(dmm int) ([]models.Actress, error) {
		var result []models.Actress
		err := db.Transaction(func(tx *gorm.DB) error {
			var err error
			result, err = NewActressRepository(&DB{DB: tx}).findImportMatchesByCanonicalUnion(t.Context(), &models.Actress{DMMID: dmm, JapaneseName: "Shared"})
			return err
		})
		return result, err
	}
	rows, err := lookup(999)
	require.NoError(t, err)
	require.Empty(t, rows, "NULL, negative and other positive IDs are ineligible for positive canonical fallback")
	var dirty int64
	require.NoError(t, db.Table("actress_canonical_keys_dirty").Count(&dirty).Error)
	require.EqualValues(t, 3, dirty, "skip must defer index maintenance")
	require.NoError(t, db.Exec("UPDATE actresses SET dmm_id=0 WHERE id=101").Error)
	_, err = lookup(999)
	require.ErrorContains(t, err, "audit fastpath index failure", "metadata-only eligibility transition must disable fast path")
	require.NoError(t, db.Table("actress_canonical_keys_dirty").Count(&dirty).Error)
	require.EqualValues(t, 3, dirty, "failed hydration must preserve queue")
	require.NoError(t, db.Exec("DROP TRIGGER audit_fastpath_sync_fail").Error)
	rows, err = lookup(999)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.EqualValues(t, 101, rows[0].ID)
	require.NoError(t, db.Exec("UPDATE actresses SET dmm_id=-2 WHERE id=101").Error)
	rows, err = lookup(999)
	require.NoError(t, err)
	require.Empty(t, rows)
	rows, err = lookup(0)
	require.NoError(t, err)
	require.Len(t, rows, 3, "name-only union still includes every role and DMM class")
}

func TestCanonicalIndexAuditDeferredPositiveHydration(t *testing.T) {
	db := newCreditTestDB(t)
	rows := make([]models.Actress, 600)
	for i := range rows {
		rows[i] = models.Actress{DMMID: 1000 + i, JapaneseName: fmt.Sprintf("Deferred %d", i), Verified: true}
	}
	require.NoError(t, db.CreateInBatches(&rows, 100).Error)
	var dirty int64
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		matches, err := NewActressRepository(&DB{DB: tx}).findImportMatchesByCanonicalUnion(t.Context(), &models.Actress{DMMID: 99999, JapaneseName: "Deferred 599"})
		require.NoError(t, err)
		require.Empty(t, matches)
		return nil
	}))
	require.NoError(t, db.Table("actress_canonical_keys_dirty").Count(&dirty).Error)
	require.EqualValues(t, 600, dirty)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		matches, err := NewActressRepository(&DB{DB: tx}).findImportMatchesByCanonicalUnion(t.Context(), &models.Actress{JapaneseName: "Deferred 599"})
		require.NoError(t, err)
		require.Len(t, matches, 1)
		require.Equal(t, rows[599].ID, matches[0].ID)
		return nil
	}))
	require.NoError(t, db.Table("actress_canonical_keys_dirty").Count(&dirty).Error)
	require.Zero(t, dirty)
	require.NoError(t, db.Create(&models.Actress{DMMID: 99998, JapaneseName: "Deferred 599", Verified: true}).Error)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		_, err := NewActressRepository(&DB{DB: tx}).findImportMatch(t.Context(), &models.Actress{JapaneseName: "Deferred 599"})
		require.ErrorIs(t, err, ErrActressImportIdentityConflict, "deferred hydration must retain union ambiguity")
		return nil
	}))

}

func TestCanonicalIndexAuditFastPathConcurrentZeroArrival(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fastpath-retry.db")
	db, err := New(&Config{Type: "sqlite", DSN: path, LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	raw, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	_, err = raw.Exec("INSERT INTO actresses(id,dmm_id,japanese_name,verified,origin) VALUES(99,1000,'Other',1,'user')")
	require.NoError(t, err)
	inserted := false
	require.NoError(t, db.Callback().Row().After("gorm:row").Register("audit:zero-arrival", func(tx *gorm.DB) {
		statement := strings.ToUpper(tx.Statement.SQL.String())
		if inserted || !strings.Contains(statement, "EXISTS") || !strings.Contains(statement, "DMM_ID") {
			return
		}
		inserted = true
		// Import's earlier exact-positive lookup already established the transaction
		// snapshot. A committed zero owner now forces the write to retry that snapshot.
		_, err := raw.Exec("INSERT INTO actresses(id,dmm_id,japanese_name,verified,origin) VALUES(100,0,'Arrival',1,'user')")
		require.NoError(t, err)
	}))
	incoming := models.Actress{DMMID: 99999, JapaneseName: "Arrival"}
	require.NoError(t, NewActressRepository(db).ImportUpsert(t.Context(), &incoming))
	require.NoError(t, db.Callback().Row().Remove("audit:zero-arrival"))
	require.True(t, inserted)
	require.EqualValues(t, 100, incoming.ID, "retry must select newly committed zero owner instead of creating a duplicate")
	var count int64
	require.NoError(t, db.Model(&models.Actress{}).Count(&count).Error)
	require.EqualValues(t, 2, count)
}
