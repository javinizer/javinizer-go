package scrape

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func compactedActressSignatures(actresses []models.Actress) []string {
	out := make([]string, len(actresses))
	for i, actress := range actresses {
		out[i] = fmt.Sprintf("%d|%s|%s|%s", actress.DMMID, actress.FirstName, actress.LastName, actress.JapaneseName)
	}
	sort.Strings(out)
	return out
}

func permuteActresses(input []models.Actress, visit func([]models.Actress)) {
	var walk func(int)
	walk = func(index int) {
		if index == len(input) {
			visit(append([]models.Actress(nil), input...))
			return
		}
		for i := index; i < len(input); i++ {
			input[index], input[i] = input[i], input[index]
			walk(index + 1)
			input[index], input[i] = input[i], input[index]
		}
	}
	walk(0)
}

func TestBuildCreditsCompactionReachesNewFullNameClosure(t *testing.T) {
	input := []models.Actress{
		{FirstName: "Alpha", JapaneseName: "共有名"},
		{LastName: "Beta", JapaneseName: "共有名"},
		{FirstName: "Alpha", LastName: "Beta"},
	}
	permutations := 0
	permuteActresses(input, func(actresses []models.Actress) {
		permutations++
		movie := &models.Movie{Actresses: actresses}
		BuildCreditsFromScrape(movie, nil, nil)
		require.Len(t, movie.Actresses, 1)
		require.Len(t, movie.Credits, 1)
		require.Equal(t, "Alpha", movie.Actresses[0].FirstName)
		require.Equal(t, "Beta", movie.Actresses[0].LastName)
		require.Equal(t, "共有名", movie.Actresses[0].JapaneseName)
	})
	require.Equal(t, 6, permutations)
}

func TestBuildCreditsCompactionDropsMultiDMMBridge(t *testing.T) {
	input := []models.Actress{
		{DMMID: 985001, FirstName: "Shared", LastName: "Person"},
		{FirstName: "Shared", LastName: "Person"},
		{DMMID: 985002, FirstName: "Shared", LastName: "Person"},
	}
	permuteActresses(input, func(actresses []models.Actress) {
		movie := &models.Movie{Actresses: actresses}
		BuildCreditsFromScrape(movie, nil, nil)
		require.Len(t, movie.Actresses, 2)
		require.Equal(t, []string{
			"985001|Shared|Person|",
			"985002|Shared|Person|",
		}, compactedActressSignatures(movie.Actresses))
	})
}

func TestBuildCreditsCompactionSameDMMConflictingNamesIsPermutationStable(t *testing.T) {
	input := []models.Actress{
		{DMMID: 985011, FirstName: "Alpha", LastName: "One"},
		{DMMID: 985011, FirstName: "Beta", LastName: "Two", JapaneseName: "同一"},
		{FirstName: "Alpha", LastName: "Two"},
	}
	permuteActresses(input, func(actresses []models.Actress) {
		movie := &models.Movie{Actresses: actresses}
		BuildCreditsFromScrape(movie, nil, nil)
		require.Len(t, movie.Actresses, 2)
		require.Len(t, movie.Credits, 2)
		dmmIDs := []int{movie.Actresses[0].DMMID, movie.Actresses[1].DMMID}
		sort.Ints(dmmIDs)
		require.Equal(t, []int{0, 985011}, dmmIDs)
	})
}

func TestBuildCreditsCompactionJapaneseAndNameTransitivityIsPermutationStable(t *testing.T) {
	input := []models.Actress{
		{FirstName: "Alpha", JapaneseName: "共有"},
		{LastName: "Beta", JapaneseName: "共有"},
		{FirstName: "Alpha", LastName: "Beta", JapaneseName: "別名"},
		{JapaneseName: "別名", ThumbURL: "final.jpg"},
	}
	permuteActresses(input, func(actresses []models.Actress) {
		movie := &models.Movie{Actresses: actresses}
		BuildCreditsFromScrape(movie, nil, nil)
		require.Len(t, movie.Actresses, 1, strings.Join(compactedActressSignatures(movie.Actresses), ","))
		require.Len(t, movie.Credits, 1)
		require.Equal(t, "Alpha", movie.Actresses[0].FirstName)
		require.Equal(t, "Beta", movie.Actresses[0].LastName)
	})
}
