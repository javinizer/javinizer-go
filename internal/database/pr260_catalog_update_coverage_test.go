package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestCatalogUpdateGuardContractsPR260(t *testing.T) {
	t.Run("nil actress", func(t *testing.T) {
		db := newCreditTestDB(t)
		require.Error(t, NewActressRepository(db).Update(context.Background(), nil))
	})

	t.Run("create compatibility failure", func(t *testing.T) {
		db := newCreditTestDB(t)
		injectDatabaseCallbackError(t, db, "create", "actresses", 1)
		actress := &models.Actress{FirstName: "Create", LastName: "Failure"}
		require.Error(t, NewActressRepository(db).Update(context.Background(), actress))
		var count int64
		require.NoError(t, db.Model(&models.Actress{}).Count(&count).Error)
		require.Zero(t, count)
	})
}
