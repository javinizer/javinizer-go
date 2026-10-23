package database

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestCanonicalIndexMigrationLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "canonical.db")
	db, err := New(&Config{Type: "sqlite", DSN: path, LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	provider := newMigrationProvider(t, sqlDB)
	_, err = provider.DownTo(t.Context(), 23)
	require.NoError(t, err)
	require.NoError(t, db.Exec("INSERT INTO actresses(id,first_name,last_name,japanese_name) VALUES(501,'Ａｌｉｃｅ','Name','ガ'),(502,NULL,NULL,NULL)").Error)
	for i := 0; i < 2; i++ {
		_, err = provider.Up(t.Context())
		require.NoError(t, err)
		require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
			matches, err := canonicalActressesByKeysTx(tx, []string{"alice name", "ガ"}, "")
			require.NoError(t, err)
			require.Len(t, matches, 1)
			require.EqualValues(t, 501, matches[0].ID)
			return nil
		}))
		var dirty int64
		require.NoError(t, db.Table("actress_canonical_keys_dirty").Count(&dirty).Error)
		require.Zero(t, dirty)
		if i == 0 {
			_, err = provider.DownTo(t.Context(), 23)
			require.NoError(t, err)
		}
	}
	require.NoError(t, db.Close())
	db, err = New(&Config{Type: "sqlite", DSN: path, LogLevel: "silent"})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	require.NoError(t, db.Exec("UPDATE actresses SET first_name='Bob' WHERE id=501").Error)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		matches, err := canonicalActressesByKeysTx(tx, []string{"bob name"}, "")
		require.NoError(t, err)
		require.Len(t, matches, 1)
		old, err := canonicalActressesByKeysTx(tx, []string{"alice name"}, "")
		require.NoError(t, err)
		require.Empty(t, old)
		return nil
	}))
}

func TestCanonicalIndexPlans(t *testing.T) {
	db := newCreditTestDB(t)
	for _, query := range []string{
		"SELECT * FROM actresses WHERE id IN (SELECT actress_id FROM actress_canonical_keys WHERE name_key IN ('test'))",
		"SELECT * FROM actresses WHERE id IN (SELECT actress_id FROM actress_canonical_keys WHERE name_key IN ('test')) AND dmm_id=0",
		"SELECT * FROM actresses WHERE id IN (SELECT actress_id FROM actress_canonical_keys WHERE name_key IN ('test')) AND verified=1 AND (dmm_id<=0 OR dmm_id IS NULL)",
	} {
		var plan []struct{ Detail string }
		require.NoError(t, db.Raw("EXPLAIN QUERY PLAN "+query).Scan(&plan).Error)
		details := ""
		for _, row := range plan {
			details += row.Detail + "\n"
		}
		require.Contains(t, details, "SEARCH actress_canonical_keys USING COVERING INDEX")
		require.False(t, strings.Contains(details, "SCAN actresses"), details)
	}
}

func TestCanonicalIndexMixedImportScaling(t *testing.T) {
	for _, size := range []int{32, 64, 128} {
		for _, overwrite := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/overwrite_%v", size, overwrite), func(t *testing.T) {
				db := newCreditTestDB(t)
				require.NoError(t, db.Create(&models.Actress{DMMID: 999999, JapaneseName: "Unrelated candidate"}).Error)
				var rows int64
				require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:canonical-scale", func(tx *gorm.DB) {
					if tx.Statement.Table == "actresses" {
						rows += tx.RowsAffected
					}
				}))
				repo := NewActressRepository(db)
				records := make([]models.Actress, size)
				for i := range records {
					records[i] = models.Actress{JapaneseName: fmt.Sprintf("Mixed %d", i)}
					if i%2 == 0 {
						records[i].DMMID = 10000 + i
					}
					a := records[i]
					_, err := repo.ImportUpsertWithOptions(t.Context(), &a, ActressImportOptions{OverwriteNames: overwrite})
					require.NoError(t, err)
					records[i].ID = a.ID
				}
				require.LessOrEqual(t, rows, int64(4*size+1))
				rows = 0
				for _, a := range records {
					id := a.ID
					a.ID = 0
					a.Aliases = "metadata only"
					_, err := repo.ImportUpsertWithOptions(t.Context(), &a, ActressImportOptions{OverwriteNames: overwrite})
					require.NoError(t, err)
					require.Equal(t, id, a.ID)
				}
				require.LessOrEqual(t, rows, int64(8*size))
				require.NoError(t, db.Callback().Query().Remove("test:canonical-scale"))
			})
		}
	}
}

func TestCanonicalIndexBatchedBackfillAndUnion(t *testing.T) {
	db := newCreditTestDB(t)
	records := make([]models.Actress, 300)
	keys := make([]string, 0, 303)
	for i := range records {
		records[i] = models.Actress{JapaneseName: fmt.Sprintf("batch %d", i), FirstName: fmt.Sprintf("pair %d", i)}
		keys = append(keys, records[i].JapaneseName)
	}
	require.NoError(t, db.CreateInBatches(&records, 100).Error)
	// Same identity occurs again in another SQL chunk through another representation.
	keys = append(keys, "pair 0", "batch 0", "missing")
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		found, err := canonicalActressesByKeysTx(tx, keys, "")
		require.NoError(t, err)
		require.Len(t, found, len(records))
		seen := make(map[uint]bool)
		for _, a := range found {
			require.False(t, seen[a.ID])
			seen[a.ID] = true
		}
		var dirty int64
		require.NoError(t, tx.Table("actress_canonical_keys_dirty").Count(&dirty).Error)
		require.Zero(t, dirty)
		return nil
	}))
}

func TestCanonicalIndexMigrationFailureIsAtomic(t *testing.T) {
	db := newCreditTestDB(t)
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	provider := newMigrationProvider(t, sqlDB)
	_, err = provider.DownTo(t.Context(), 23)
	require.NoError(t, err)
	require.NoError(t, db.Exec("CREATE TRIGGER actress_canonical_keys_insert AFTER INSERT ON actresses BEGIN SELECT 1; END").Error)
	_, err = provider.Up(t.Context())
	require.Error(t, err)
	var count int64
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE name IN ('actress_canonical_keys','actress_canonical_keys_dirty')").Scan(&count).Error)
	require.Zero(t, count)
	require.NoError(t, db.Exec("DROP TRIGGER actress_canonical_keys_insert").Error)
	_, err = provider.Up(t.Context())
	require.NoError(t, err)
}

func exactActressNamesMatch(left, right *models.Actress) bool {
	leftKeys := canonicalActressRepresentationKeys(left)
	if len(leftKeys) == 0 {
		return false
	}
	for key := range canonicalActressRepresentationKeys(right) {
		if _, ok := leftKeys[key]; ok {
			return true
		}
	}
	return false
}
