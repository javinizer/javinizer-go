package database

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func BenchmarkFindAuthoritativeProjections(b *testing.B) {
	db, err := New(&Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	require.NoError(b, err)
	require.NoError(b, db.RunMigrationsOnStartup(context.Background()))
	b.Cleanup(func() { _ = db.Close() })
	actress := models.Actress{JapaneseName: "Benchmark", Verified: true, Origin: ActressOriginUser}
	require.NoError(b, db.Create(&actress).Error)
	movies := make([]models.Movie, 500)
	credits := make([]models.MovieCredit, 500)
	for i := range movies {
		movies[i] = models.Movie{ContentID: fmt.Sprintf("benchmark-content-%03d", i), ID: fmt.Sprintf("BENCHMARK-%03d", i)}
		credits[i] = models.MovieCredit{MovieContentID: movies[i].ContentID, ActressID: actress.ID}
	}
	require.NoError(b, db.CreateInBatches(&movies, 100).Error)
	require.NoError(b, db.CreateInBatches(&credits, 100).Error)
	repo := NewMovieRepository(db)
	for _, size := range []int{1, 100, 500} {
		contentIDs := make([]string, size)
		canonicalIDs := make([]string, size)
		for i := 0; i < size; i++ {
			contentIDs[i], canonicalIDs[i] = movies[i].ContentID, movies[i].ID
		}
		b.Run(fmt.Sprintf("unique-%d", size), func(b *testing.B) {
			b.ReportMetric(3, "queries/op")
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := repo.FindAuthoritativeProjections(context.Background(), contentIDs, canonicalIDs); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
	sharedContent := make([]string, 500)
	sharedCanonical := make([]string, 500)
	for i := range sharedContent {
		sharedContent[i], sharedCanonical[i] = movies[0].ContentID, movies[0].ID
	}
	b.Run("multipart-500-to-1", func(b *testing.B) {
		b.ReportMetric(3, "queries/op")
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := repo.FindAuthoritativeProjections(context.Background(), sharedContent, sharedCanonical); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func TestFindAuthoritativeProjectionsBoundedQueriesAndSourceOfTruth(t *testing.T) {
	db := newCreditTestDB(t)
	verified := models.Actress{JapaneseName: "Verified", Verified: true, Origin: ActressOriginUser, Aliases: "Alias A|Alias B"}
	candidate := models.Actress{JapaneseName: "Candidate", Origin: ActressOriginScrape}
	require.NoError(t, db.Create(&verified).Error)
	require.NoError(t, db.Create(&candidate).Error)
	movies := make([]models.Movie, 500)
	credits := make([]models.MovieCredit, 0, 502)
	for i := range movies {
		movies[i] = models.Movie{ContentID: fmt.Sprintf("projection-content-%03d", i), ID: fmt.Sprintf("PROJECTION-%03d", i), Title: fmt.Sprintf("Movie %d", i)}
		credits = append(credits, models.MovieCredit{MovieContentID: movies[i].ContentID, ActressID: verified.ID, CreditedName: "Verified Credit", OrderIndex: 2})
	}
	credits = append(credits,
		models.MovieCredit{MovieContentID: movies[0].ContentID, ActressID: candidate.ID, CreditedName: "Candidate Credit", OrderIndex: 1},
		models.MovieCredit{MovieContentID: movies[0].ContentID, ActressID: candidate.ID + 100000, CreditedName: "Suppressed", OrderIndex: 0, Suppressed: true},
	)
	require.NoError(t, db.CreateInBatches(&movies, 100).Error)
	require.NoError(t, db.CreateInBatches(&credits, 100).Error)

	repo := NewMovieRepository(db)
	countQueries := func(run func()) int64 {
		var queries atomic.Int64
		name := fmt.Sprintf("projection_query_count_%p", &queries)
		require.NoError(t, db.Callback().Query().After("gorm:query").Register(name, func(*gorm.DB) { queries.Add(1) }))
		run()
		require.NoError(t, db.Callback().Query().Remove(name))
		return queries.Load()
	}
	oneQueries := countQueries(func() {
		projection, err := repo.FindAuthoritativeProjections(context.Background(), []string{movies[0].ContentID}, []string{movies[0].ID})
		require.NoError(t, err)
		movie := projection.ByContentID[movies[0].ContentID]
		require.Len(t, movie.Credits, 2)
		require.Equal(t, "Candidate Credit", movie.Credits[0].CreditedName)
		require.NotNil(t, movie.Credits[0].Actress)
		require.Len(t, movie.Actresses, 2)
		require.Equal(t, candidate.ID, movie.Actresses[0].ID)
		require.Equal(t, verified.ID, movie.Actresses[1].ID)
	})
	allContent, allCanonical := make([]string, 0, 1000), make([]string, 0, 1000)
	for i := range movies {
		allContent = append(allContent, movies[i].ContentID, movies[i].ContentID)
		allCanonical = append(allCanonical, movies[i].ID, movies[i].ID)
	}
	allQueries := countQueries(func() {
		projection, err := repo.FindAuthoritativeProjections(context.Background(), allContent, allCanonical)
		require.NoError(t, err)
		require.Len(t, projection.ByContentID, 500)
		require.Len(t, projection.ByCanonicalID, 500)
	})
	require.EqualValues(t, 3, oneQueries)
	require.Equal(t, oneQueries, allQueries)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := repo.FindAuthoritativeProjections(ctx, []string{movies[0].ContentID}, nil)
	require.ErrorIs(t, err, context.Canceled)
}

func TestFindAuthoritativeProjectionsAliasDuplicationStaysBounded(t *testing.T) {
	db := newCreditTestDB(t)
	actress := models.Actress{JapaneseName: "Bounded", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&actress).Error)
	movies := make([]models.Movie, 500)
	credits := make([]models.MovieCredit, 0, len(movies))
	for i := range movies {
		movies[i] = models.Movie{ContentID: fmt.Sprintf("bounded-content-%03d", i), ID: fmt.Sprintf("BOUNDED-%03d", i)}
		credits = append(credits, models.MovieCredit{MovieContentID: movies[i].ContentID, ActressID: actress.ID})
	}
	require.NoError(t, db.CreateInBatches(&movies, 100).Error)
	require.NoError(t, db.CreateInBatches(&credits, 100).Error)

	// Mirror movieProjectionLookupIDs: every result contributes its matcher alias
	// to BOTH the content and canonical lookup dimensions. The alias is duplicated
	// 2*len(movies) times per list and also overlaps a canonical movie ID.
	alias := movies[0].ID
	contentIDs := make([]string, 0, len(movies)*2)
	canonicalIDs := make([]string, 0, len(movies)*2)
	for i := range movies {
		contentIDs = append(contentIDs, movies[i].ContentID, alias)
		canonicalIDs = append(canonicalIDs, movies[i].ID, alias)
	}

	repo := NewMovieRepository(db)
	var queries atomic.Int64
	name := "projection_alias_duplication_query_count"
	require.NoError(t, db.Callback().Query().After("gorm:query").Register(name, func(*gorm.DB) { queries.Add(1) }))
	projection, err := repo.FindAuthoritativeProjections(context.Background(), contentIDs, canonicalIDs)
	require.NoError(t, db.Callback().Query().Remove(name))
	require.NoError(t, err)
	require.EqualValues(t, 3, queries.Load(), "dedup must keep the batched projection at one query per phase")
	require.Len(t, projection.ByContentID, 500)
	require.Len(t, projection.ByCanonicalID, 500)
	require.Empty(t, projection.AmbiguousCanonicalIDs)
	require.Equal(t, movies[0].ContentID, projection.ByCanonicalID[alias].ContentID)

	empty, err := repo.FindAuthoritativeProjections(context.Background(), nil, nil)
	require.NoError(t, err)
	require.Empty(t, empty.ByContentID)
	require.Empty(t, empty.ByCanonicalID)
	require.Empty(t, empty.AmbiguousCanonicalIDs)
}

func TestFindAuthoritativeProjectionsAmbiguousCanonicalIDsFailClosed(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(fmt.Sprintf("reverse-%t", reverse), func(t *testing.T) {
			db := newCreditTestDB(t)
			actresses := []models.Actress{{JapaneseName: "First", Verified: true}, {JapaneseName: "Second", Verified: true}, {JapaneseName: "Unique", Verified: true}}
			require.NoError(t, db.Create(&actresses).Error)
			movies := []models.Movie{
				{ContentID: "duplicate-a", ID: "DUPLICATE"},
				{ContentID: "duplicate-b", ID: "DUPLICATE"},
				{ContentID: "unique", ID: "Unique"},
				{ContentID: "case", ID: "unique"},
				{ContentID: "empty", ID: ""},
			}
			if reverse {
				for left, right := 0, len(movies)-1; left < right; left, right = left+1, right-1 {
					movies[left], movies[right] = movies[right], movies[left]
				}
			}
			require.NoError(t, db.Create(&movies).Error)
			require.NoError(t, db.Create(&[]models.MovieCredit{
				{MovieContentID: "duplicate-a", ActressID: actresses[0].ID},
				{MovieContentID: "duplicate-b", ActressID: actresses[1].ID},
				{MovieContentID: "unique", ActressID: actresses[2].ID},
			}).Error)

			projection, err := NewMovieRepository(db).FindAuthoritativeProjections(t.Context(),
				[]string{"duplicate-a", "duplicate-b", "empty"},
				[]string{"DUPLICATE", "Unique", "unique", "", "   "},
			)
			require.NoError(t, err)
			require.Contains(t, projection.ByContentID, "duplicate-a")
			require.Contains(t, projection.ByContentID, "duplicate-b")
			require.NotContains(t, projection.ByCanonicalID, "DUPLICATE")
			require.Contains(t, projection.AmbiguousCanonicalIDs, "DUPLICATE")
			require.NotContains(t, projection.ByCanonicalID, "")
			require.Equal(t, "unique", projection.ByCanonicalID["Unique"].ContentID)
			require.Equal(t, "case", projection.ByCanonicalID["unique"].ContentID)

			canonicalOnly, err := NewMovieRepository(db).FindAuthoritativeProjections(t.Context(), nil, []string{"Unique"})
			require.NoError(t, err)
			require.Equal(t, "unique", canonicalOnly.ByCanonicalID["Unique"].ContentID)
		})
	}
}

func projectionPhaseFixture(t *testing.T) (*DB, *MovieRepository, models.Movie) {
	t.Helper()
	db := newCreditTestDB(t)
	actress := models.Actress{JapaneseName: "Projection Phase", Verified: true, Origin: ActressOriginUser}
	movie := models.Movie{ContentID: "projection-phase", ID: "PROJECTION-PHASE"}
	require.NoError(t, db.Create(&actress).Error)
	require.NoError(t, db.Create(&movie).Error)
	require.NoError(t, db.Create(&models.MovieCredit{MovieContentID: movie.ContentID, ActressID: actress.ID}).Error)
	return db, NewMovieRepository(db), movie
}

func TestAuthoritativeProjectionPhaseCancellationReturnsNoPartialResult(t *testing.T) {
	for _, table := range []string{"movies", "movie_credits", "actresses"} {
		t.Run(table, func(t *testing.T) {
			db, repo, movie := projectionPhaseFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			name := "cancel-projection-after-" + table
			require.NoError(t, db.Callback().Query().After("gorm:query").Register(name, func(tx *gorm.DB) {
				if tx.Statement != nil && tx.Statement.Table == table {
					cancel()
				}
			}))
			t.Cleanup(func() { _ = db.Callback().Query().Remove(name) })
			projection, err := repo.FindAuthoritativeProjections(ctx, []string{movie.ContentID}, []string{movie.ID})
			require.ErrorIs(t, err, context.Canceled)
			require.Nil(t, projection)
		})
	}
}

func TestAuthoritativeProjectionQueryFailuresReturnNoPartialResult(t *testing.T) {
	for _, table := range []string{"movie_credits", "actresses"} {
		t.Run(table, func(t *testing.T) {
			db, repo, movie := projectionPhaseFixture(t)
			injectDatabaseCallbackError(t, db, "query", table, 1)
			projection, err := repo.FindAuthoritativeProjections(context.Background(), []string{movie.ContentID}, []string{movie.ID})
			require.Error(t, err)
			require.Nil(t, projection)
		})
	}
}
