package nfo_test

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/stretchr/testify/require"
)

func TestMergeMovieMetadataKeepsDelimiterContainingNamesDistinct(t *testing.T) {
	for _, strategy := range []nfo.MergeStrategy{nfo.PreferScraper, nfo.PreferNFO} {
		for _, collision := range []struct{ name, first, last string }{
			{"reverse-key collision", "C|A", "B"},
			{"direct-key collision", "A", "B|C"},
		} {
			for _, reverse := range []bool{false, true} {
				name := string(strategy) + "/" + collision.name
				if reverse {
					name += "/reversed sources"
				} else {
					name += "/original sources"
				}
				t.Run(name, func(t *testing.T) {
					left := models.Actress{ID: 101, DMMID: 1001, FirstName: "A|B", LastName: "C"}
					right := models.Actress{ID: 202, DMMID: 2002, FirstName: collision.first, LastName: collision.last}
					if reverse {
						left, right = right, left
					}
					scraped := &models.Movie{ID: "IPX-535", ContentID: "ipx00535", Actresses: []models.Actress{left}}
					existing := &models.Movie{ID: "IPX-535", ContentID: "ipx00535", Actresses: []models.Actress{right}}
					beforeScraped, beforeExisting := scraped.Clone(), existing.Clone()
					result, err := nfo.MergeMovieMetadataWithOptions(scraped, existing, strategy, true)
					require.NoError(t, err)
					require.NotNil(t, result)
					require.Equal(t, beforeScraped, scraped)
					require.Equal(t, beforeExisting, existing)
					require.Equal(t, []models.Actress{left, right}, result.Merged.Actresses, "a separator inside a name must not make distinct name tuples collapse or discard identity")
				})
			}
		}
	}
}

func TestMergeMovieMetadataRetainsNameTupleMatchingPolicy(t *testing.T) {
	for _, tc := range []struct {
		name        string
		left, right models.Actress
	}{
		{"literal pipe exact tuple", models.Actress{FirstName: "A|B", LastName: "C"}, models.Actress{FirstName: "A|B", LastName: "C"}},
		{"literal pipe swapped tuple", models.Actress{FirstName: "A|B", LastName: "C"}, models.Actress{FirstName: "C", LastName: "A|B"}},
		{"ordinary swapped names", models.Actress{FirstName: "Yui", LastName: "Hatano"}, models.Actress{FirstName: "Hatano", LastName: "Yui"}},
		{"case and whitespace", models.Actress{FirstName: " Alice ", LastName: "B"}, models.Actress{FirstName: "alice", LastName: " b "}},
		{"empty last component", models.Actress{FirstName: "A|B"}, models.Actress{LastName: "A|B"}},
		{"Japanese match remains authoritative", models.Actress{JapaneseName: " 同一 ", FirstName: "Original"}, models.Actress{JapaneseName: "同一", FirstName: "Display"}},
	} {
		for _, strategy := range []nfo.MergeStrategy{nfo.PreferScraper, nfo.PreferNFO} {
			t.Run(tc.name+"/"+string(strategy), func(t *testing.T) {
				left, right := tc.left, tc.right
				left.ID, left.DMMID = 101, 1001
				right.ID, right.DMMID = 202, 2002
				scraped := &models.Movie{ID: "IPX-535", Actresses: []models.Actress{left}}
				existing := &models.Movie{ID: "IPX-535", Actresses: []models.Actress{right}}
				beforeScraped, beforeExisting := scraped.Clone(), existing.Clone()
				result, err := nfo.MergeMovieMetadataWithOptions(scraped, existing, strategy, true)
				require.NoError(t, err)
				require.Len(t, result.Merged.Actresses, 1, "literal pipes remain accepted; true name tuples still match")
				want := left
				if want.FirstName == "" {
					want.FirstName = right.FirstName
				}
				if want.LastName == "" {
					want.LastName = right.LastName
				}
				if want.JapaneseName == "" {
					want.JapaneseName = right.JapaneseName
				}
				require.Equal(t, want, result.Merged.Actresses[0], "existing scraped identity/name authority is preserved")
				require.Equal(t, beforeScraped, scraped)
				require.Equal(t, beforeExisting, existing)
			})
		}
	}
}

func TestMergeMovieMetadataPreservesDuplicateCandidateOrder(t *testing.T) {
	for _, strategy := range []nfo.MergeStrategy{nfo.PreferScraper, nfo.PreferNFO} {
		t.Run(string(strategy), func(t *testing.T) {
			a := models.Actress{ID: 1, DMMID: 1001, FirstName: "Shared", LastName: "Name"}
			b := a
			b.ID = 2
			b.DMMID = 1002
			unmatched := models.Actress{ID: 3, DMMID: 1003, FirstName: "Scraped", LastName: "Only"}
			first, second, leftover := a, a, a
			first.ID, second.ID, leftover.ID = 4, 5, 6
			first.DMMID, second.DMMID, leftover.DMMID = 2001, 2002, 2003
			first.ThumbURL, second.ThumbURL, leftover.ThumbURL = "https://fixture.invalid/first.jpg", "https://fixture.invalid/second.jpg", "https://fixture.invalid/leftover.jpg"
			added := models.Actress{ID: 7, DMMID: 2004, FirstName: "NFO", LastName: "Only"}
			scraped := &models.Movie{ID: "IPX-535", Actresses: []models.Actress{a, b, unmatched}}
			existing := &models.Movie{ID: "IPX-535", Actresses: []models.Actress{first, second, leftover, added}}
			beforeScraped, beforeExisting := scraped.Clone(), existing.Clone()
			result, err := nfo.MergeMovieMetadataWithOptions(scraped, existing, strategy, true)
			require.NoError(t, err)
			wantA, wantB := a, b
			wantA.ThumbURL, wantB.ThumbURL = first.ThumbURL, second.ThumbURL
			require.Equal(t, []models.Actress{wantA, wantB, unmatched, leftover, added}, result.Merged.Actresses, "each candidate is consumed once; duplicate and unmatched order stays stable")
			require.Equal(t, beforeScraped, scraped)
			require.Equal(t, beforeExisting, existing)
		})
	}
}

func TestMergeMovieMetadataJapaneseNamePrecedesRomanTuple(t *testing.T) {
	for _, strategy := range []nfo.MergeStrategy{nfo.PreferScraper, nfo.PreferNFO} {
		t.Run(string(strategy), func(t *testing.T) {
			japanese := models.Actress{ID: 1, DMMID: 1001, JapaneseName: "同一", FirstName: "Japanese", LastName: "Match"}
			roman := models.Actress{ID: 2, DMMID: 1002, JapaneseName: "別人", FirstName: "Roman", LastName: "Match"}
			incoming := models.Actress{ID: 3, DMMID: 2001, JapaneseName: "同一", FirstName: "Roman", LastName: "Match", ThumbURL: "https://fixture.invalid/preferred.jpg"}
			scraped := &models.Movie{ID: "IPX-535", Actresses: []models.Actress{roman, japanese}}
			existing := &models.Movie{ID: "IPX-535", Actresses: []models.Actress{incoming}}
			beforeScraped, beforeExisting := scraped.Clone(), existing.Clone()
			result, err := nfo.MergeMovieMetadataWithOptions(scraped, existing, strategy, true)
			require.NoError(t, err)
			japanese.ThumbURL = incoming.ThumbURL
			require.Equal(t, []models.Actress{roman, japanese}, result.Merged.Actresses, "Japanese-name candidate wins even when roman-name candidate is earlier")
			require.Equal(t, beforeScraped, scraped)
			require.Equal(t, beforeExisting, existing)
		})
	}
}
