package actress

import (
	"fmt"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Exercise the CLI's preflight lookups as well as ImportUpsert. A missing-row
// lookup returns zero rows even if it scans the entire growing catalogue, so
// returned-row bounds alone are insufficient: inspect the actual query plans.
func TestCLIImportLookupPlansAndScaling(t *testing.T) {
	for _, size := range []int{32, 64, 128} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
			type query struct {
				sql  string
				args []interface{}
			}
			preflight := map[string]query{}
			var queries, identityRows int64
			require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:cli-import-plans", func(tx *gorm.DB) {
				queries++
				if tx.Statement.Table != "actresses" {
					return
				}
				identityRows += tx.RowsAffected
				sql := tx.Statement.SQL.String()
				// Only First lookups have LIMIT 1. Capture the real SQL and arguments,
				// including the CLI-only Japanese and English preflight lookups.
				if !strings.Contains(sql, "LIMIT 1") {
					return
				}
				kind := "id"
				switch {
				case strings.Contains(sql, "first_name = ? AND last_name = ?"):
					kind = "english"
				case strings.Contains(sql, "japanese_name = ?"):
					kind = "japanese"
				case strings.Contains(sql, "dmm_id = ?"):
					kind = "dmm"
				}
				preflight[kind] = query{sql, append([]interface{}(nil), tx.Statement.Vars...)}
			}))
			records := make([]models.Actress, size)
			for i := range records {
				records[i] = models.Actress{ID: uint(1000000 + i), DMMID: 2000000 + i, FirstName: fmt.Sprintf("Synthetic%06d", i), LastName: "Benchmark", JapaneseName: fmt.Sprintf("合成%06d", i)}
			}
			imported, skipped, failures := importActressRecords(t.Context(), database.NewActressRepository(db), records)
			require.NoError(t, db.Callback().Query().Remove("test:cli-import-plans"))
			require.Equal(t, size, imported)
			require.Zero(t, skipped)
			require.Zero(t, failures)
			require.Zero(t, identityRows)
			require.LessOrEqual(t, queries, int64(8*size))
			require.Len(t, preflight, 4, "all CLI preflight paths must be exercised")
			for kind, q := range preflight {
				var plan []struct{ Detail string }
				require.NoError(t, db.Raw("EXPLAIN QUERY PLAN "+q.sql, q.args...).Scan(&plan).Error)
				var details []string
				for _, step := range plan {
					details = append(details, step.Detail)
				}
				detail := strings.Join(details, "\n")
				require.Contains(t, detail, "SEARCH actresses", kind)
				require.NotContains(t, detail, "SCAN actresses", kind)
				if kind == "english" {
					require.Contains(t, detail, "idx_actresses_name_pair_lookup")
					require.NotContains(t, detail, "TEMP B-TREE", "English ordering must also be indexed")
				}
			}
			t.Logf("%d CLI rows: %d queries, %d identity rows", size, queries, identityRows)
		})
	}
}

func TestCLIEnglishPreflightIndexPreservesDuplicateOrdering(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	// Duplicate names remain legal. Positive DMM wins first; among DMM-less
	// ties the smaller row ID still wins, regardless of insertion order.
	rows := []models.Actress{
		{ID: 30, DMMID: 100, FirstName: "Shared", LastName: "Name"},
		{ID: 20, DMMID: 200, FirstName: "Shared", LastName: "Name"},
		{ID: 10, DMMID: 0, FirstName: "Shared", LastName: "Name"},
		{ID: 50, DMMID: 0, FirstName: "Tied", LastName: "Name"},
		{ID: 40, DMMID: 0, FirstName: "Tied", LastName: "Name"},
	}
	require.NoError(t, db.Create(&rows).Error)
	for _, tc := range []struct {
		name string
		want uint
	}{{"Shared", 20}, {"Tied", 40}} {
		found, err := findActressImportMatch(t.Context(), database.NewActressRepository(db), &models.Actress{FirstName: tc.name, LastName: "Name"})
		require.NoError(t, err)
		require.Equal(t, tc.want, found.ID)
	}
}
