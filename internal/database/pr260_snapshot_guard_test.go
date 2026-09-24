package database

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestRefreshIdentitySnapshotCreditsTxGuards(t *testing.T) {
	db := newCreditTestDB(t)

	// No previous identity snapshot means there is nothing to re-derive.
	require.NoError(t, refreshIdentitySnapshotCreditsTx(db.DB, 1, nil, "First", "Last", ""))

	// A previous identity without any canonical name has no snapshot to match.
	require.NoError(t, refreshIdentitySnapshotCreditsTx(db.DB, 1, &models.Actress{ThumbURL: "https://only-thumb"}, "First", "Last", ""))

	// A blank new canonical name leaves the credits untouched as well.
	previous := &models.Actress{FirstName: "Old", LastName: "Name"}
	require.NoError(t, refreshIdentitySnapshotCreditsTx(db.DB, 1, previous, "", "", ""))
}
