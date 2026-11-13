package database

import (
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestCanonicalIndexPreservesVerifiedDMMlessPredicateAcrossBatches(t *testing.T) {
	db := newCreditTestDB(t)
	var keys []string
	var wantIDs []uint
	var excludedIDs []uint
	for i := 0; i < 320; i++ {
		verified := i%8 < 4
		kind := i % 4
		owner := models.Actress{
			Verified:     verified,
			JapaneseName: fmt.Sprintf("canonical %03d", i),
			FirstName:    fmt.Sprintf("alternate %03d", i),
		}
		switch kind {
		case 1:
			owner.DMMID = -1
		case 3:
			owner.DMMID = 80000 + i
		}
		require.NoError(t, db.Create(&owner).Error)
		if kind == 0 {
			require.NoError(t, db.Model(&owner).UpdateColumn("dmm_id", nil).Error)
		}
		keys = append(keys, owner.JapaneseName)
		if verified && kind != 3 {
			wantIDs = append(wantIDs, owner.ID)
		} else {
			excludedIDs = append(excludedIDs, owner.ID)
		}
	}
	keys = append(keys, "canonical 000", "alternate 000", "canonical 319", "alternate 319", "missing")
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		for i := 0; i < 2; i++ {
			found, err := canonicalActressesByKeysTx(tx, keys, "verified = ? AND (dmm_id <= 0 OR dmm_id IS NULL)", true)
			require.NoError(t, err)
			var gotIDs []uint
			for _, owner := range found {
				require.True(t, owner.Verified)
				require.LessOrEqual(t, owner.DMMID, 0)
				gotIDs = append(gotIDs, owner.ID)
			}
			require.ElementsMatch(t, wantIDs, gotIDs)
			for _, id := range excludedIDs {
				require.NotContains(t, gotIDs, id)
			}
		}
		return nil
	}))
}
