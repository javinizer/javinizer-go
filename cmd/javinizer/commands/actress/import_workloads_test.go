package actress

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	if kind == "candidate-heavy" || kind == "alias-heavy" || kind == "mixed-alias" {
		owner := models.Actress{Verified: true, JapaneseName: "Fixture canonical"}
		require.NoError(tb, db.Create(&owner).Error)
		for i := 0; i < 32; i++ {
			candidate := models.Actress{DMMID: 900000 + i, JapaneseName: fmt.Sprintf("Fixture stage %d", i)}
			require.NoError(tb, db.Create(&candidate).Error)
			if kind == "alias-heavy" || kind == "mixed-alias" {
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
		if strings.HasPrefix(kind, "dmmless") || (strings.HasPrefix(kind, "mixed") && i%2 == 0) {
			records[i].DMMID = 0
		}
	}
	if strings.HasSuffix(kind, "reimport") || strings.HasSuffix(kind, "update") {
		// Seed a separate slice: the measured input must still model ID-less JSON,
		// not reuse the database IDs written back by the initial import.
		seedRecords := append([]models.Actress(nil), records...)
		imported, skipped, failures := importActressRecords(tb.Context(), database.NewActressRepository(db), seedRecords)
		require.Equal(tb, size, imported)
		require.Zero(tb, skipped)
		require.Zero(tb, failures)
		if strings.HasSuffix(kind, "update") {
			for i := range records {
				records[i].Aliases = "Metadata only"
			}
		}
	}
	return db, records
}

// Snapshot seeded IDs from the database independently of the mutable import input.
func expectedImportWorkload(tb testing.TB, db *database.DB, kind string, records []models.Actress) []models.Actress {
	tb.Helper()
	expected := append([]models.Actress(nil), records...)
	var all []models.Actress
	require.NoError(tb, db.Find(&all).Error)
	byName := make(map[string][]models.Actress)
	for _, a := range all {
		byName[a.JapaneseName] = append(byName[a.JapaneseName], a)
	}
	if strings.HasSuffix(kind, "reimport") || strings.HasSuffix(kind, "update") {
		for i := range expected {
			require.Zero(tb, records[i].ID, "reimport and update must match ID-less inputs")
			matches := byName[expected[i].JapaneseName]
			require.Len(tb, matches, 1)
			require.NotZero(tb, matches[0].ID, "the fixture must already exist in the database")
			expected[i].ID = matches[0].ID
		}
	}
	return expected
}

func assertImportedWorkload(tb testing.TB, db *database.DB, kind string, expected, records []models.Actress) {
	tb.Helper()
	seenIDs := make(map[uint]bool, len(expected))
	var all []models.Actress
	require.NoError(tb, db.Find(&all).Error)
	byName := make(map[string][]models.Actress)
	for _, a := range all {
		byName[a.JapaneseName] = append(byName[a.JapaneseName], a)
	}
	for i, record := range expected {
		// Locate by an immutable fixture name, never an ID returned by import.
		matches := byName[record.JapaneseName]
		require.Len(tb, matches, 1)
		stored := matches[0]
		require.NotZero(tb, stored.ID)
		require.False(tb, seenIDs[stored.ID], "each fixture must retain a distinct identity")
		seenIDs[stored.ID] = true
		if strings.HasSuffix(kind, "reimport") || strings.HasSuffix(kind, "update") {
			require.NotZero(tb, record.ID)
			require.Equal(tb, record.ID, stored.ID, "existing identity IDs must be preserved")
		}
		require.Equal(tb, stored.ID, records[i].ID, "import must return the original fixture's identity")
		require.Equal(tb, record.DMMID, records[i].DMMID)
		require.Equal(tb, record.DMMID, stored.DMMID)
		require.Equal(tb, record.FirstName, stored.FirstName)
		require.Equal(tb, record.LastName, stored.LastName)
		require.Equal(tb, record.JapaneseName, stored.JapaneseName)
		require.True(tb, stored.Verified)
		require.Equal(tb, record.Aliases, stored.Aliases)
	}
}

func TestCLIImportRepresentativeWorkloads(t *testing.T) {
	for _, kind := range []string{"fresh", "reimport", "update", "candidate-heavy", "alias-heavy", "dmmless", "mixed-alias", "dmmless-reimport", "dmmless-update"} {
		for _, size := range []int{24, 48} {
			t.Run(fmt.Sprintf("%s/%d", kind, size), func(t *testing.T) {
				db, records := importWorkload(t, kind, size, false)
				// ImportUpsert mutates records, including their IDs and DMM IDs. Keep
				// the original identities (and seeded IDs) independent of its output.
				expected := expectedImportWorkload(t, db, kind, records)
				var before []models.Actress
				var creditsBefore []models.MovieCredit
				require.NoError(t, db.Where("dmm_id >= ? OR japanese_name = ?", 800000, "Fixture canonical").Order("id").Find(&before).Error)
				require.NoError(t, db.Order("id").Find(&creditsBefore).Error)
				if kind == "candidate-heavy" || kind == "alias-heavy" || kind == "mixed-alias" {
					require.Len(t, before, 34)
					require.Len(t, creditsBefore, 32)
					for _, candidate := range before {
						if !candidate.Verified {
							require.Equal(t, kind == "alias-heavy" || kind == "mixed-alias", candidate.AmbiguityQuarantined)
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
				if strings.HasSuffix(kind, "reimport") {
					require.Equal(t, size, skipped)
					require.Zero(t, imported)
				} else {
					require.Equal(t, size, imported)
					require.Zero(t, skipped)
				}
				require.LessOrEqual(t, queries, int64(20*size), "query count remains bounded per row")
				// Imports inspect fixed candidate evidence and changed identities,
				// never the growing positive-DMM or DMM-less catalogue.
				bound := int64(5 * size)
				if kind == "candidate-heavy" || kind == "alias-heavy" || kind == "mixed-alias" {
					bound = int64(36 * size)
				}
				require.LessOrEqual(t, identityRows, bound)
				require.LessOrEqual(t, aliasRows, int64(128*size))
				var after []models.Actress
				var creditsAfter []models.MovieCredit
				require.NoError(t, db.Where("dmm_id >= ? OR japanese_name = ?", 800000, "Fixture canonical").Order("id").Find(&after).Error)
				require.NoError(t, db.Order("id").Find(&creditsAfter).Error)
				require.Equal(t, before, after, "unrelated identities and quarantine must be unchanged")
				require.Equal(t, creditsBefore, creditsAfter, "credits must not migrate")
				assertImportedWorkload(t, db, kind, expected, records)
				t.Logf("%d rows: %d queries, %d identity rows, %d alias rows", size, queries, identityRows, aliasRows)
			})
		}
	}
}

// Run explicitly with -run '^$' -bench '^BenchmarkCLIImportWorkloads$'
// -benchtime=1x -count=3. Setup/migrations and assertions are outside the timer.
// Each operation imports a whole dataset into a fresh temporary on-disk SQLite DB.
func BenchmarkCLIImportWorkloads(b *testing.B) {
	for _, kind := range []string{"fresh", "reimport", "update", "candidate-heavy", "alias-heavy", "dmmless", "mixed-alias", "dmmless-reimport", "dmmless-update"} {
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
	var cold, warm time.Duration
	for n := 0; n < b.N; n++ {
		b.StopTimer()
		db, records := importWorkload(b, kind, size, true)
		expected := expectedImportWorkload(b, db, kind, records)
		var before []models.Actress
		var creditsBefore []models.MovieCredit
		require.NoError(b, db.Where("dmm_id >= ? OR japanese_name = ?", 800000, "Fixture canonical").Order("id").Find(&before).Error)
		require.NoError(b, db.Order("id").Find(&creditsBefore).Error)
		b.StartTimer()
		imported, skipped, failures := importActressRecords(b.Context(), database.NewActressRepository(db), records)
		b.StopTimer()
		require.Zero(b, failures)
		if strings.HasSuffix(kind, "reimport") {
			require.Equal(b, size, skipped)
			require.Zero(b, imported)
		} else {
			require.Equal(b, size, imported)
			require.Zero(b, skipped)
		}
		assertImportedWorkload(b, db, kind, expected, records)
		var after []models.Actress
		var creditsAfter []models.MovieCredit
		require.NoError(b, db.Where("dmm_id >= ? OR japanese_name = ?", 800000, "Fixture canonical").Order("id").Find(&after).Error)
		require.NoError(b, db.Order("id").Find(&creditsAfter).Error)
		require.Equal(b, before, after, "unrelated identities and quarantine must be unchanged")
		require.Equal(b, creditsBefore, creditsAfter, "credits must not migrate")
		if kind == "fresh" {
			// Deferred maintenance is not free: report the first name-only lookup
			// separately, then the identical warm lookup, outside import timing.
			var persistedBefore models.Actress
			require.NoError(b, db.First(&persistedBefore, records[0].ID).Error)
			for attempt := 0; attempt < 2; attempt++ {
				probe := models.Actress{JapaneseName: expected[0].JapaneseName}
				start := time.Now()
				err := database.NewActressRepository(db).ImportUpsert(b.Context(), &probe)
				elapsed := time.Since(start)
				require.NoError(b, err)
				require.Equal(b, records[0].ID, probe.ID)
				require.Equal(b, expected[0].DMMID, probe.DMMID)
				var persistedAfter models.Actress
				require.NoError(b, db.First(&persistedAfter, records[0].ID).Error)
				// Public import intentionally saves its matched row; only its
				// update timestamp may change during these name-only probes.
				require.False(b, persistedAfter.UpdatedAt.Before(persistedBefore.UpdatedAt))
				persistedAfter.UpdatedAt = persistedBefore.UpdatedAt
				require.Equal(b, persistedBefore, persistedAfter)
				if attempt == 0 {
					cold += elapsed
				} else {
					warm += elapsed
				}
			}
		}
		require.NoError(b, db.Close())
	}
	b.ReportMetric(float64(size), "rows/op")
	if kind == "fresh" {
		b.ReportMetric(float64(cold.Nanoseconds())/float64(b.N), "first-name-ns/op")
		b.ReportMetric(float64(warm.Nanoseconds())/float64(b.N), "warm-name-ns/op")
	}
}

// Compare bounded baseline sizes and linear scaling with identical fixtures.
func BenchmarkCLIImportDMMlessScaling(b *testing.B) {
	for _, size := range []int{250, 500, 1000} {
		b.Run(fmt.Sprint(size), func(b *testing.B) { benchmarkCLIImport(b, "dmmless", size) })
	}
}

func BenchmarkCLIImportDMMless85000(b *testing.B) {
	if os.Getenv("JAVINIZER_BENCH_LARGE_IMPORT") != "1" {
		b.Skip("set JAVINIZER_BENCH_LARGE_IMPORT=1")
	}
	benchmarkCLIImport(b, "dmmless", 85000)
}
