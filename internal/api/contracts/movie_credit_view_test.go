package contracts

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMovieCreditViewFromModel(t *testing.T) {
	assert.Nil(t, MovieCreditViewFromModel(nil))
	assert.Empty(t, MovieCreditViewSliceFromModels(nil))

	view := MovieCreditViewFromModel(&models.MovieCredit{
		ID: 3, ActressID: 4, CreditedName: "Credited", CreditedJapaneseName: "名",
		ReportedThumbURL: "https://thumb", OverrideName: "Override", UserOverride: true, Suppressed: true, OrderIndex: 2,
	})
	require.NotNil(t, view)
	assert.Equal(t, uint(3), view.ID)
	assert.Equal(t, uint(4), view.ActressID)
	assert.Equal(t, "Credited", view.CreditedName)
	assert.Equal(t, "名", view.CreditedJapaneseName)
	assert.Equal(t, "https://thumb", view.ReportedThumbURL)
	assert.Equal(t, "Override", view.OverrideName)
	assert.True(t, view.UserOverride)
	assert.True(t, view.Suppressed)
	assert.Equal(t, 2, view.OrderIndex)

	views := MovieCreditViewSliceFromModels([]models.MovieCredit{{ID: 1}, {ID: 2}})
	require.Len(t, views, 2)
	assert.Equal(t, uint(1), views[0].ID)
}
