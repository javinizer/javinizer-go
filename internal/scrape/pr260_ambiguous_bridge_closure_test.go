package scrape

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestBuildCreditsDropsAmbiguousNoDMMComponentsAcrossPermutations(t *testing.T) {
	base := []models.Actress{
		{DMMID: 701, FirstName: "Alias", LastName: "One", JapaneseName: "乙"},
		{DMMID: 701, FirstName: "Alpha", LastName: "Beta", JapaneseName: "甲"},
		{DMMID: 702, FirstName: "Alpha", LastName: "Beta", JapaneseName: "丙"},
		{FirstName: "Alpha", LastName: "Beta"},
	}
	permutations := ambiguousBridgePermutations(base)
	for i, actresses := range permutations {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			movie := &models.Movie{Actresses: actresses}
			BuildCreditsFromScrape(movie, nil, nil)
			require.Len(t, movie.Actresses, 2)
			require.Len(t, movie.Credits, 2)
			require.ElementsMatch(t, []int{701, 702}, []int{movie.Actresses[0].DMMID, movie.Actresses[1].DMMID})
			for _, actress := range movie.Actresses {
				require.NotZero(t, actress.DMMID)
			}
			fixedPoint := &models.Movie{Actresses: append([]models.Actress(nil), movie.Actresses...)}
			BuildCreditsFromScrape(fixedPoint, nil, nil)
			require.Equal(t, movie.Actresses, fixedPoint.Actresses)
			require.Len(t, fixedPoint.Credits, 2)
		})
	}
}

func TestBuildCreditsPreservesStandaloneAndSingleDMMNoDMMComponents(t *testing.T) {
	standalone := &models.Movie{Actresses: []models.Actress{{FirstName: "Standalone", LastName: "Person"}}}
	BuildCreditsFromScrape(standalone, nil, nil)
	require.Len(t, standalone.Actresses, 1)
	require.Zero(t, standalone.Actresses[0].DMMID)

	attached := &models.Movie{Actresses: []models.Actress{{FirstName: "Attached", LastName: "Person"}, {DMMID: 703, FirstName: "Attached", LastName: "Person", JapaneseName: "接続"}}}
	BuildCreditsFromScrape(attached, nil, nil)
	require.Len(t, attached.Actresses, 1)
	require.Len(t, attached.Credits, 1)
	require.Equal(t, 703, attached.Actresses[0].DMMID)
}

func TestBuildCreditsDropsAmbiguousNoDMMComponentBridgeVariants(t *testing.T) {
	tests := map[string][]models.Actress{
		"japanese": {
			{DMMID: 711, FirstName: "Alias", JapaneseName: "乙"},
			{DMMID: 711, FirstName: "One", JapaneseName: "共通"},
			{DMMID: 712, FirstName: "Two", JapaneseName: "共通"},
			{JapaneseName: "共通"},
		},
		"complete component keys": {
			{DMMID: 721, FirstName: "Alpha", LastName: "One"},
			{DMMID: 722, FirstName: "Beta", LastName: "Two"},
			{FirstName: "Alpha", LastName: "One", JapaneseName: "橋"},
			{FirstName: "Beta", LastName: "Two", JapaneseName: "橋"},
		},
	}
	for name, actresses := range tests {
		t.Run(name, func(t *testing.T) {
			for _, permutation := range ambiguousBridgePermutations(actresses) {
				movie := &models.Movie{Actresses: permutation}
				BuildCreditsFromScrape(movie, nil, nil)
				require.Len(t, movie.Actresses, 2)
				require.Len(t, movie.Credits, 2)
				for _, actress := range movie.Actresses {
					require.NotZero(t, actress.DMMID)
				}
			}
		})
	}
}

func TestAmbiguousBridgeReportStaysTwoCreditsThroughMovieUpsert(t *testing.T) {
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))

	movie := &models.Movie{
		ContentID: "ambiguous-bridge-closure",
		ID:        "AMBIGUOUS-BRIDGE-CLOSURE",
		Actresses: []models.Actress{
			{DMMID: 701, FirstName: "Alias", LastName: "One", JapaneseName: "乙"},
			{DMMID: 701, FirstName: "Alpha", LastName: "Beta", JapaneseName: "甲"},
			{DMMID: 702, FirstName: "Alpha", LastName: "Beta", JapaneseName: "丙"},
			{FirstName: "Alpha", LastName: "Beta"},
		},
	}
	BuildCreditsFromScrape(movie, map[string]string{"dmmid:701": "provider-701", "dmmid:702": "provider-702"}, []*models.ScraperResult{
		{Source: "provider-701", Actresses: []models.ActressInfo{{DMMID: 701, FirstName: "Reported", LastName: "One"}}},
		{Source: "provider-702", Actresses: []models.ActressInfo{{DMMID: 702, FirstName: "Reported", LastName: "Two"}}},
	})
	require.Len(t, movie.Actresses, 2)
	require.Len(t, movie.Credits, 2)
	require.Equal(t, []int{701, 702}, []int{movie.Actresses[0].DMMID, movie.Actresses[1].DMMID})
	require.Equal(t, []string{"provider-701", "provider-702"}, []string{movie.Credits[0].Source, movie.Credits[1].Source})
	require.Equal(t, []string{"One Reported", "Two Reported"}, []string{movie.Credits[0].CreditedName, movie.Credits[1].CreditedName})
	require.Equal(t, []int{0, 1}, []int{movie.Credits[0].OrderIndex, movie.Credits[1].OrderIndex})

	emitted := &models.Movie{Actresses: append([]models.Actress(nil), movie.Actresses...)}
	BuildCreditsFromScrape(emitted, nil, nil)
	require.Equal(t, movie.Actresses, emitted.Actresses)
	require.Len(t, emitted.Credits, 2)

	saved, err := database.NewMovieRepository(db).Upsert(t.Context(), movie)
	require.NoError(t, err)
	require.Len(t, saved.Credits, 2)
	require.NotEqual(t, saved.Credits[0].ActressID, saved.Credits[1].ActressID)
	require.Equal(t, []int{701, 702}, []int{saved.Credits[0].Actress.DMMID, saved.Credits[1].Actress.DMMID})
	require.Equal(t, []string{"provider-701", "provider-702"}, []string{saved.Credits[0].Source, saved.Credits[1].Source})
	require.Equal(t, []string{"One Reported", "Two Reported"}, []string{saved.Credits[0].CreditedName, saved.Credits[1].CreditedName})
	require.Equal(t, []int{0, 1}, []int{saved.Credits[0].OrderIndex, saved.Credits[1].OrderIndex})
}

func ambiguousBridgePermutations(values []models.Actress) [][]models.Actress {
	working := append([]models.Actress(nil), values...)
	out := make([][]models.Actress, 0)
	var visit func(int)
	visit = func(index int) {
		if index == len(working) {
			out = append(out, append([]models.Actress(nil), working...))
			return
		}
		for i := index; i < len(working); i++ {
			working[index], working[i] = working[i], working[index]
			visit(index + 1)
			working[index], working[i] = working[i], working[index]
		}
	}
	visit(0)
	return out
}
