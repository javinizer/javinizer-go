package models

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMovieCloneDeepCopiesCredits(t *testing.T) {
	identity := &Actress{FirstName: "Original", Translations: []ActressTranslation{{FirstName: "Translated"}}}
	movie := &Movie{Credits: []MovieCredit{{ID: 1, CreditedName: "Credit", Actress: identity, Scraped: Actress{Translations: []ActressTranslation{{FirstName: "Scraped"}}}}, {ID: 2}}}
	clone := movie.Clone()
	require.NotSame(t, &movie.Credits[0], &clone.Credits[0])
	require.NotSame(t, movie.Credits[0].Actress, clone.Credits[0].Actress)
	clone.Credits[0].CreditedName = "Changed"
	clone.Credits[0].Actress.FirstName = "Changed"
	clone.Credits[0].Actress.Translations[0].FirstName = "Changed"
	clone.Credits[0].Scraped.Translations[0].FirstName = "Changed"
	require.Equal(t, "Credit", movie.Credits[0].CreditedName)
	require.Equal(t, "Original", movie.Credits[0].Actress.FirstName)
	require.Equal(t, "Translated", movie.Credits[0].Actress.Translations[0].FirstName)
	require.Equal(t, "Scraped", movie.Credits[0].Scraped.Translations[0].FirstName)
	require.Nil(t, clone.Credits[1].Actress)
}
