package actress

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// These fixtures are deliberately synthetic. They exercise the entire CLI import
// loop, not JSON decoding, network storage, or any user's catalogue.
func importWorkload(tb testing.TB, kind string, size int, disk bool) (*database.DB, []models.Actress) {
	tb.Helper()
	dsn := ":memory:"
	if disk {
		dsn = filepath.Join(tb.TempDir(), "import.db")
	}
	db, err := database.New(&database.Config{Type: "sqlite", DSN: dsn, LogLevel: "silent"})
	require.NoError(tb, err)
	tb.Cleanup(func() { _ = db.Close() })
	require.NoError(tb, db.RunMigrationsOnStartup(tb.Context()))
	if kind == "candidate-heavy" || kind == "alias-heavy" {
		owner := models.Actress{Verified: true, JapaneseName: "Fixture canonical"}
		require.NoError(tb, db.Create(&owner).Error)
		for i := 0; i < 32; i++ {
			candidate := models.Actress{DMMID: 900000 + i, JapaneseName: fmt.Sprintf("Fixture stage %d", i)}
			require.NoError(tb, db.Create(&candidate).Error)
			if kind == "alias-heavy" {
				for j := 0; j < 4; j++ {
					name := fmt.Sprintf("Fixture alias %d %d", i, j)
					if j == 0 {
						name = candidate.JapaneseName
					}
					require.NoError(tb, db.Create(&models.ActressAlias{AliasName: name, CanonicalName: owner.JapaneseName}).Error)
				}
			}
			movie := models.Movie{ID: fmt.Sprintf("fixture-%d", i), ContentID: fmt.Sprintf("fixture-%d", i)}
			require.NoError(tb, db.Create(&movie).Error)
			require.NoError(tb, db.Create(&models.MovieCredit{MovieContentID: movie.ContentID, ActressID: candidate.ID, CreditedName: candidate.JapaneseName}).Error)
		}
		// Establish quarantine through the same public path before taking snapshots.
		seed := models.Actress{DMMID: 800000, JapaneseName: "Fixture seed"}
		require.NoError(tb, database.NewActressRepository(db).ImportUpsert(tb.Context(), &seed))
	}
	records := make([]models.Actress, size)
	for i := range records {
		records[i] = models.Actress{DMMID: 10000 + i, FirstName: fmt.Sprintf("Synthetic%06d", i), LastName: "Workload", JapaneseName: fmt.Sprintf("合成%06d", i)}
		if kind == "dmmless" {
			records[i].DMMID = 0
		}
	}
	if kind == "reimport" || kind == "update" {
		imported, skipped, failures := importActressRecords(tb.Context(), database.NewActressRepository(db), records)
		require.Equal(tb, size, imported)
		require.Zero(tb, skipped)
		require.Zero(tb, failures)
		if kind == "update" {
			for i := range records {
				records[i].Aliases = "Metadata only"
			}
		}
	}
	return db, records
}

func TestCLIImportRepresentativeWorkloads(t *testing.T) {
	for _, kind := range []string{"fresh", "reimport", "update", "candidate-heavy", "alias-heavy", "dmmless"} {
		for _, size := range []int{24, 48} {
			t.Run(fmt.Sprintf("%s/%d", kind, size), func(t *testing.T) {
				db, records := importWorkload(t, kind, size, false)
				var before []models.Actress
				var creditsBefore []models.MovieCredit
				require.NoError(t, db.Where("dmm_id >= ? OR japanese_name = ?", 800000, "Fixture canonical").Order("id").Find(&before).Error)
				require.NoError(t, db.Order("id").Find(&creditsBefore).Error)
				if kind == "candidate-heavy" || kind == "alias-heavy" {
					require.Len(t, before, 34)
					require.Len(t, creditsBefore, 32)
					for _, candidate := range before {
						if !candidate.Verified {
							require.Equal(t, kind == "alias-heavy", candidate.AmbiguityQuarantined)
						}
					}
				}
				var queries, identityRows, aliasRows int64
				require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:workload", func(tx *gorm.DB) {
					queries++
					if tx.Statement.Table == "actresses" {
						identityRows += tx.RowsAffected
					}
					if tx.Statement.Table == "actress_aliases" {
						aliasRows += tx.RowsAffected
					}
				}))
				imported, skipped, failures := importActressRecords(t.Context(), database.NewActressRepository(db), records)
				require.NoError(t, db.Callback().Query().Remove("test:workload"))
				require.Zero(t, failures)
				if kind == "reimport" {
					require.Equal(t, size, skipped)
					require.Zero(t, imported)
				} else {
					require.Equal(t, size, imported)
					require.Zero(t, skipped)
				}
				require.LessOrEqual(t, queries, int64(20*size), "query count remains bounded per row")
				// Positive-DMM imports may inspect fixed candidate/DMM-less evidence, not
				// the growing positive-DMM catalogue. DMM-less canonical matching still
				// scans prior identities; explicitly characterize that quadratic limit.
				bound := int64(5 * size)
				if kind == "candidate-heavy" || kind == "alias-heavy" {
					bound = int64(36 * size)
				}
				if kind == "dmmless" {
					bound = int64(size * (size - 1) / 2)
				}
				require.LessOrEqual(t, identityRows, bound)
				require.LessOrEqual(t, aliasRows, int64(128*size))
				var after []models.Actress
				var creditsAfter []models.MovieCredit
				require.NoError(t, db.Where("dmm_id >= ? OR japanese_name = ?", 800000, "Fixture canonical").Order("id").Find(&after).Error)
				require.NoError(t, db.Order("id").Find(&creditsAfter).Error)
				require.Equal(t, before, after, "unrelated identities and quarantine must be unchanged")
				require.Equal(t, creditsBefore, creditsAfter, "credits must not migrate")
				for _, record := range records {
					var stored models.Actress
					require.NoError(t, db.First(&stored, record.ID).Error)
					require.Equal(t, record.DMMID, stored.DMMID)
					require.True(t, stored.Verified)
					require.Equal(t, record.Aliases, stored.Aliases)
				}
				t.Logf("%d rows: %d queries, %d identity rows, %d alias rows", size, queries, identityRows, aliasRows)
			})
		}
	}
}

// Run explicitly with -run '^$' -bench '^BenchmarkCLIImportWorkloads$'
// -benchtime=1x -count=3. Setup/migrations and assertions are outside the timer.
// Each operation imports a whole dataset into a fresh temporary on-disk SQLite DB.
func BenchmarkCLIImportWorkloads(b *testing.B) {
	for _, kind := range []string{"fresh", "reimport", "update", "candidate-heavy", "alias-heavy", "dmmless"} {
		b.Run(kind, func(b *testing.B) { benchmarkCLIImport(b, kind, 1000) })
	}
}

// The 85k case is doubly opt-in so ordinary CI and make bench stay moderate.
func BenchmarkCLIImportFresh85000(b *testing.B) {
	if os.Getenv("JAVINIZER_BENCH_LARGE_IMPORT") != "1" {
		b.Skip("set JAVINIZER_BENCH_LARGE_IMPORT=1 to run the synthetic 85k import")
	}
	benchmarkCLIImport(b, "fresh", 85000)
}

func benchmarkCLIImport(b *testing.B, kind string, size int) {
	b.ReportAllocs()
	for n := 0; n < b.N; n++ {
		b.StopTimer()
		db, records := importWorkload(b, kind, size, true)
		b.StartTimer()
		imported, skipped, failures := importActressRecords(b.Context(), database.NewActressRepository(db), records)
		b.StopTimer()
		require.Zero(b, failures)
		require.Equal(b, size, imported+skipped)
		require.NoError(b, db.Close())
	}
	b.ReportMetric(float64(size), "rows/op")
}
