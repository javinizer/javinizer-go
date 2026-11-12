package nfo_test

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/stretchr/testify/require"
)

func TestMergeMovieMetadataConsumesNameCandidatesInPriorityOrder(t *testing.T) {
	japanese := models.Actress{ID: 1, DMMID: 1001, JapaneseName: "同一", FirstName: "First", LastName: "Person"}
	roman := models.Actress{ID: 2, DMMID: 1002, JapaneseName: "別人", FirstName: "Second", LastName: "Person"}
	reversed := models.Actress{ID: 3, DMMID: 1003, JapaneseName: "異名", FirstName: "Person", LastName: "Second"}
	first := models.Actress{JapaneseName: " 同一 ", FirstName: "Second", LastName: "Person", ThumbURL: "https://fixture.invalid/first.jpg"}
	second := first
	second.ThumbURL = "https://fixture.invalid/second.jpg"
	third := first
	third.ThumbURL = "https://fixture.invalid/third.jpg"

	withThumb := func(a models.Actress, thumb string) models.Actress {
		a.ThumbURL = thumb
		return a
	}
	for _, tc := range []struct {
		name     string
		scraped  []models.Actress
		existing []models.Actress
		want     []models.Actress
	}{
		{
			name:     "consumed Japanese match falls back to romanized name",
			scraped:  []models.Actress{japanese, roman},
			existing: []models.Actress{first, second, third},
			want:     []models.Actress{withThumb(japanese, first.ThumbURL), withThumb(roman, second.ThumbURL), third},
		},
		{
			name:     "exact romanized tuple precedes earlier reversed tuple",
			scraped:  []models.Actress{reversed, roman},
			existing: []models.Actress{first, second, third},
			want:     []models.Actress{withThumb(reversed, second.ThumbURL), withThumb(roman, first.ThumbURL), third},
		},
	} {
		for _, strategy := range []nfo.MergeStrategy{nfo.PreferScraper, nfo.PreferNFO} {
			t.Run(tc.name+"/"+string(strategy), func(t *testing.T) {
				scraped := &models.Movie{ID: "IPX-535", Actresses: tc.scraped}
				existing := &models.Movie{ID: "IPX-535", Actresses: tc.existing}
				beforeScraped, beforeExisting := scraped.Clone(), existing.Clone()
				result, err := nfo.MergeMovieMetadataWithOptions(scraped, existing, strategy, true)
				require.NoError(t, err)
				require.Equal(t, tc.want, result.Merged.Actresses, "candidate priority, one-use matching, identity and output order must stay stable")
				require.Equal(t, beforeScraped, scraped)
				require.Equal(t, beforeExisting, existing)
			})
		}
	}
}
