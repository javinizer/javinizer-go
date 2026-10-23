package actress

import (
	"fmt"
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

// Characterize, rather than hide, the remaining per-import candidate scan.
// Fixed-C catalogue scaling is not a claim of linearity when C also grows.
func TestCLIImportGrowingCandidatePopulation(t *testing.T) {
	const imports = 8
	for _, candidates := range []int{32, 256, 1024} {
		t.Run(fmt.Sprint(candidates), func(t *testing.T) {
			db, records, before := candidatePopulationWorkload(t, candidates, imports, false)
			expected := append([]models.Actress(nil), records...)
			var rows int64
			require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:candidate-population", func(tx *gorm.DB) {
				if tx.Statement.Table == "actresses" {
					rows += tx.RowsAffected
				}
			}))
			summary := importActressRecordsWithOptions(t.Context(), database.NewActressRepository(db), records, database.ActressImportOptions{})
			require.NoError(t, db.Callback().Query().Remove("test:candidate-population"))
			require.Zero(t, summary.Errors)
			require.Equal(t, imports, summary.Created)
			require.GreaterOrEqual(t, rows, int64(imports*candidates))
			require.LessOrEqual(t, rows, int64(imports*(candidates+3)))
			var after []models.Actress
			require.NoError(t, db.Where("verified = ?", false).Order("id").Find(&after).Error)
			require.Equal(t, before, after)
			assertImportedWorkload(t, db, "fresh", expected, records)
			t.Logf("%d imports with %d candidates: %d actress rows read", imports, candidates, rows)
		})
	}
}

func BenchmarkCLIImportGrowingCandidates(b *testing.B) {
	const imports = 100
	for _, candidates := range []int{32, 256, 1024} {
		b.Run(fmt.Sprint(candidates), func(b *testing.B) {
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
		})
	}
}
