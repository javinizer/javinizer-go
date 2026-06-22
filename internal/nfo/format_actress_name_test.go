package nfo

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestFormatActressName(t *testing.T) {
	testCases := []struct {
		name           string
		actress        models.Actress
		japaneseNames  bool
		firstNameOrder bool
		unknownActress string
		want           string
	}{
		{
			name: "last name first order with both names",
			actress: models.Actress{
				FirstName: "Yui",
				LastName:  "Hatano",
			},
			japaneseNames:  false,
			firstNameOrder: false,
			unknownActress: "",
			want:           "Hatano Yui",
		},
		{
			name: "first name first order with both names",
			actress: models.Actress{
				FirstName: "Yui",
				LastName:  "Hatano",
			},
			japaneseNames:  false,
			firstNameOrder: true,
			unknownActress: "",
			want:           "Yui Hatano",
		},
		{
			name: "japanese name preferred when enabled",
			actress: models.Actress{
				FirstName:    "Yui",
				LastName:     "Hatano",
				JapaneseName: "波多野結衣",
			},
			japaneseNames:  true,
			firstNameOrder: false,
			unknownActress: "",
			want:           "波多野結衣",
		},
		{
			name: "falls back to english when japanese enabled but no japanese name",
			actress: models.Actress{
				FirstName: "Yui",
				LastName:  "Hatano",
			},
			japaneseNames:  true,
			firstNameOrder: true,
			unknownActress: "",
			want:           "Yui Hatano",
		},
		{
			name: "returns japanese name when english names empty even without japanese flag",
			actress: models.Actress{
				JapaneseName: "波多野結衣",
			},
			japaneseNames:  false,
			firstNameOrder: false,
			unknownActress: "",
			want:           "波多野結衣",
		},
		{
			name: "returns Unknown when all names empty and unknownActress empty",
			actress: models.Actress{
				FirstName: "",
				LastName:  "",
			},
			japaneseNames:  false,
			firstNameOrder: false,
			unknownActress: "",
			want:           "Unknown",
		},
		{
			name: "returns custom unknown when all names empty and unknownActress set",
			actress: models.Actress{
				FirstName: "",
				LastName:  "",
			},
			japaneseNames:  false,
			firstNameOrder: false,
			unknownActress: "N/A",
			want:           "N/A",
		},
		{
			name: "only first name in first-name order",
			actress: models.Actress{
				FirstName: "Yui",
			},
			japaneseNames:  false,
			firstNameOrder: true,
			unknownActress: "",
			want:           "Yui",
		},
		{
			name: "only last name in first-name order",
			actress: models.Actress{
				LastName: "Hatano",
			},
			japaneseNames:  false,
			firstNameOrder: true,
			unknownActress: "",
			want:           "Hatano",
		},
		{
			name: "only first name in last-name order",
			actress: models.Actress{
				FirstName: "Yui",
			},
			japaneseNames:  false,
			firstNameOrder: false,
			unknownActress: "",
			want:           "Yui",
		},
		{
			name: "only last name in last-name order",
			actress: models.Actress{
				LastName: "Hatano",
			},
			japaneseNames:  false,
			firstNameOrder: false,
			unknownActress: "",
			want:           "Hatano",
		},
		{
			name: "japanese name with empty english names and japanese flag off returns japanese",
			actress: models.Actress{
				JapaneseName: "波多野結衣",
			},
			japaneseNames:  true,
			firstNameOrder: false,
			unknownActress: "",
			want:           "波多野結衣",
		},
		{
			name: "skip mode returns empty string for unknown actress",
			actress: models.Actress{
				FirstName: "",
				LastName:  "",
			},
			japaneseNames:  false,
			firstNameOrder: false,
			unknownActress: "",
			want:           "",
		},
		{
			name: "skip mode returns name when actress has name",
			actress: models.Actress{
				FirstName: "Yui",
				LastName:  "Hatano",
			},
			japaneseNames:  false,
			firstNameOrder: true,
			unknownActress: "",
			want:           "Yui Hatano",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			opts := models.FormatActressNameOptions{
				JapaneseNames:  tc.japaneseNames,
				FirstNameOrder: tc.firstNameOrder,
				UnknownActress: tc.unknownActress,
			}
			// The last two test cases use skip mode
			if tc.name == "skip mode returns empty string for unknown actress" ||
				tc.name == "skip mode returns name when actress has name" {
				opts.UnknownActressMode = models.UnknownActressModeSkip
			}
			got := models.FormatActressName(tc.actress, opts)
			assert.Equal(t, tc.want, got)
		})
	}
}
