package actress

import (
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Seed import-owned identities and a curated alias for each. Half have no DMM
// identifier and use explicit local references, the only safe way to rename a
// name-only identity. Every expected ID is captured before the measured input.
func renameImportWorkload(tb testing.TB, size int, disk bool) (*database.DB, []models.Actress, []uint) {
	db, records := importWorkload(tb, "fresh", size, disk)
	for i := range records {
		if i%2 == 0 {
			records[i].DMMID = 0
		}
	}
	summary := importActressRecordsWithOptions(tb.Context(), database.NewActressRepository(db), records, database.ActressImportOptions{})
	require.Equal(tb, size, summary.Created)
	require.Zero(tb, summary.Errors)
	ids := make([]uint, size)
	for i := range records {
		ids[i] = records[i].ID
		require.NoError(tb, db.Create(&models.ActressAlias{AliasName: fmt.Sprintf("Curated stage %d", i), CanonicalName: records[i].JapaneseName}).Error)
		records[i].JapaneseName = fmt.Sprintf("Renamed %06d", i)
		// Only canonical Japanese names change; English representations stay stable.
		if records[i].DMMID > 0 {
			records[i].ID = 0
		}
	}
	return db, records, ids
}

func assertRenameImportWorkload(tb testing.TB, db *database.DB, expected, records []models.Actress, ids []uint) {
	var all []models.Actress
	require.NoError(tb, db.Find(&all).Error)
	require.Len(tb, all, len(records))
	byID := make(map[uint]models.Actress)
	for _, a := range all {
		byID[a.ID] = a
	}
	var aliases []models.ActressAlias
	require.NoError(tb, db.Find(&aliases).Error)
	byAlias := make(map[string]string)
	for _, a := range aliases {
		byAlias[a.AliasName] = a.CanonicalName
	}
	for i, a := range expected {
		stored, ok := byID[ids[i]]
		require.True(tb, ok)
		require.Equal(tb, ids[i], records[i].ID)
		require.Equal(tb, a.JapaneseName, stored.JapaneseName)
		require.Equal(tb, a.DMMID, stored.DMMID)
		require.Equal(tb, a.FirstName, stored.FirstName)
		require.Equal(tb, a.LastName, stored.LastName)
		require.Equal(tb, a.JapaneseName, byAlias[fmt.Sprintf("Curated stage %d", i)])
		require.Equal(tb, a.JapaneseName, byAlias[fmt.Sprintf("合成%06d", i)])
	}
}

func TestCLICanonicalRenameImportScaling(t *testing.T) {
	for _, size := range []int{24, 48, 96} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			db, records, ids := renameImportWorkload(t, size, false)
			expected := append([]models.Actress(nil), records...)
			var rows int64
			require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:rename-scaling", func(tx *gorm.DB) {
				if tx.Statement.Table == "actresses" {
					rows += tx.RowsAffected
				}
			}))
			summary := importActressRecordsWithOptions(t.Context(), database.NewActressRepository(db), records, database.ActressImportOptions{OverwriteNames: true})
			require.NoError(t, db.Callback().Query().Remove("test:rename-scaling"))
			require.Zero(t, summary.Errors)
			require.Equal(t, size, summary.Updated)
			require.LessOrEqual(t, rows, int64(8*size))
			assertRenameImportWorkload(t, db, expected, records, ids)
		})
	}
}

func BenchmarkCLICanonicalRenameImport(b *testing.B) {
	for _, size := range []int{250, 500, 1000} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				db, records, ids := renameImportWorkload(b, size, true)
				expected := append([]models.Actress(nil), records...)
				b.StartTimer()
				summary := importActressRecordsWithOptions(b.Context(), database.NewActressRepository(db), records, database.ActressImportOptions{OverwriteNames: true})
				b.StopTimer()
				require.Zero(b, summary.Errors)
				require.Equal(b, size, summary.Updated)
				assertRenameImportWorkload(b, db, expected, records, ids)
				require.NoError(b, db.Close())
			}
			b.ReportMetric(float64(size), "rows/op")
		})
	}
}
