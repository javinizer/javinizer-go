package actress

import (
	"fmt"
	"os"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func candidatePopulationWorkload(tb testing.TB, candidates, imports int, disk bool) (*database.DB, []models.Actress, []models.Actress) {
	db, records := importWorkload(tb, "fresh", imports, disk)
	population := make([]models.Actress, candidates)
	for i := range population {
		population[i] = models.Actress{DMMID: 1000000 + i, JapaneseName: fmt.Sprintf("Background candidate %d", i)}
	}
	require.NoError(tb, db.CreateInBatches(&population, 64).Error)
	seed := models.Actress{DMMID: 800000, JapaneseName: "Population seed"}
	require.NoError(tb, database.NewActressRepository(db).ImportUpsert(tb.Context(), &seed))
	var before []models.Actress
	require.NoError(tb, db.Where("verified = ?", false).Order("id").Find(&before).Error)
	require.Len(tb, before, candidates)
	return db, records, before
}

// A settled, unchanged candidate graph must not be reloaded for unrelated
// positive imports, even when the background candidate population grows.
func TestCLIImportGrowingCandidatePopulation(t *testing.T) {
	for _, imports := range []int{8, 32, 128} {
		for _, candidates := range []int{32, 256, 1024} {
			t.Run(fmt.Sprintf("imports_%d/candidates_%d", imports, candidates), func(t *testing.T) {
				db, records, before := candidatePopulationWorkload(t, candidates, imports, false)
				expected := append([]models.Actress(nil), records...)
				var rows, queries int64
				require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:candidate-population", func(tx *gorm.DB) {
					queries++
					if tx.Statement.Table == "actresses" {
						rows += tx.RowsAffected
					}
				}))
				require.NoError(t, db.Callback().Row().After("gorm:row").Register("test:candidate-population-rows", func(*gorm.DB) { queries++ }))
				summary := importActressRecordsWithOptions(t.Context(), database.NewActressRepository(db), records, database.ActressImportOptions{})
				require.NoError(t, db.Callback().Query().Remove("test:candidate-population"))
				require.NoError(t, db.Callback().Row().Remove("test:candidate-population-rows"))
				require.LessOrEqual(t, queries, int64(6*imports), "read queries must remain bounded per import")
				require.Zero(t, summary.Errors)
				require.Equal(t, imports, summary.Created)
				require.Zero(t, rows, "clean imports must not hydrate candidates or unrelated canonical identities")
				var after []models.Actress
				require.NoError(t, db.Where("verified = ?", false).Order("id").Find(&after).Error)
				require.Equal(t, before, after)
				assertImportedWorkload(t, db, "fresh", expected, records)
				t.Logf("%d imports with %d candidates: %d actress rows read", imports, candidates, rows)
			})
		}
	}

}

func BenchmarkCLIImportGrowingCandidates(b *testing.B) {
	for _, candidates := range []int{32, 256, 1024} {
		b.Run(fmt.Sprint(candidates), func(b *testing.B) { benchmarkCLIImportCandidatePopulation(b, candidates, 100) })
	}
}

// Opt-in proof for a large positive import into a fixed, settled candidate graph.
// This is not a mass-promotion or missing-DMM throughput claim.
func BenchmarkCLIImportStaticCandidates85000(b *testing.B) {
	if os.Getenv("JAVINIZER_BENCH_LARGE_IMPORT") != "1" {
		b.Skip("set JAVINIZER_BENCH_LARGE_IMPORT=1")
	}
	benchmarkCLIImportCandidatePopulation(b, 1024, 85000)
}

func benchmarkCLIImportCandidatePopulation(b *testing.B, candidates, imports int) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		db, records, before := candidatePopulationWorkload(b, candidates, imports, true)
		expected := append([]models.Actress(nil), records...)
		b.StartTimer()
		summary := importActressRecordsWithOptions(b.Context(), database.NewActressRepository(db), records, database.ActressImportOptions{})
		b.StopTimer()
		require.Zero(b, summary.Errors)
		require.Equal(b, imports, summary.Created)
		var after []models.Actress
		require.NoError(b, db.Where("verified = ?", false).Order("id").Find(&after).Error)
		require.Equal(b, before, after)
		assertImportedWorkload(b, db, "fresh", expected, records)
		require.NoError(b, db.Close())
	}
	b.ReportMetric(float64(imports), "rows/op")
}

// Promotion mutates the graph each time. Do not misrepresent clean-graph gating
// as batch recomputation or as a solution for mass candidate promotion.
func TestCLIImportCandidatePromotionStillReconciles(t *testing.T) {
	const candidates, imports = 32, 8
	db, _, _ := candidatePopulationWorkload(t, candidates, imports, false)
	records := make([]models.Actress, imports)
	for i := range records {
		records[i] = models.Actress{DMMID: 1000000 + i, JapaneseName: fmt.Sprintf("Background candidate %d", i)}
	}
	var rows int64
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:promotion-graph", func(tx *gorm.DB) {
		if tx.Statement.Table == "actresses" {
			rows += tx.RowsAffected
		}
	}))
	summary := importActressRecordsWithOptions(t.Context(), database.NewActressRepository(db), records, database.ActressImportOptions{})
	require.NoError(t, db.Callback().Query().Remove("test:promotion-graph"))
	require.Zero(t, summary.Errors)
	require.GreaterOrEqual(t, rows, int64(imports*candidates-imports*(imports+1)/2), "changing candidate graph still receives a full pass")
	for _, record := range records {
		var stored models.Actress
		require.NoError(t, db.First(&stored, record.ID).Error)
		require.True(t, stored.Verified)
		require.Equal(t, record.DMMID, stored.DMMID)
	}
}
