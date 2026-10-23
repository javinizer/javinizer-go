package database

import (
	"errors"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Every failed derived-index stage must roll back as a unit, leave its queue
// retryable, and never turn an index failure into an empty identity match.
func TestCanonicalIndexStageFailuresRollback(t *testing.T) {
	for _, stage := range []string{"queue read", "owner read", "key delete", "key insert", "queue delete", "lookup"} {
		t.Run(stage, func(t *testing.T) {
			db := newCreditTestDB(t)
			owner := models.Actress{JapaneseName: "Owner", Verified: true}
			require.NoError(t, db.Create(&owner).Error)
			require.NoError(t, db.Transaction(syncActressCanonicalKeysTx))
			require.NoError(t, db.Exec("INSERT INTO actress_canonical_keys_dirty(actress_id) VALUES (?)", owner.ID).Error)
			failure := errors.New("injected canonical stage")
			fired := false
			require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:index-stage", func(tx *gorm.DB) {
				hit := (stage == "queue read" && tx.Statement.Table == "actress_canonical_keys_dirty") || (stage == "owner read" && tx.Statement.Table == "actresses")
				if stage == "lookup" && tx.Statement.Table == "actresses" {

					hit = strings.Contains(tx.Statement.SQL.String(), "SELECT actress_id FROM actress_canonical_keys")
				}
				if hit {
					fired = true
					tx.AddError(failure)
				}
			}))
			var trigger string
			switch stage {
			case "key delete":
				trigger = "BEFORE DELETE ON actress_canonical_keys"
			case "key insert":
				trigger = "BEFORE INSERT ON actress_canonical_keys"
			case "queue delete":
				trigger = "BEFORE DELETE ON actress_canonical_keys_dirty"
			}
			if trigger != "" {
				require.NoError(t, db.Exec("CREATE TRIGGER test_index_stage "+trigger+" BEGIN SELECT RAISE(ABORT, 'injected canonical stage'); END").Error)
			}
			err := db.Transaction(func(tx *gorm.DB) error { _, err := canonicalActressesByKeysTx(tx, []string{"owner"}, ""); return err })
			require.ErrorContains(t, err, "injected canonical stage")
			if trigger == "" {
				require.True(t, fired)
			} else {
				require.NoError(t, db.Exec("DROP TRIGGER test_index_stage").Error)
			}
			require.NoError(t, db.Callback().Query().Remove("test:index-stage"))
			var dirty, keys int64
			require.NoError(t, db.Table("actress_canonical_keys_dirty").Count(&dirty).Error)
			require.EqualValues(t, 1, dirty)
			require.NoError(t, db.Table("actress_canonical_keys").Count(&keys).Error)
			require.EqualValues(t, 1, keys)
			require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
				matches, err := canonicalActressesByKeysTx(tx, []string{"owner"}, "")
				require.Len(t, matches, 1)
				return err
			}))
		})
	}
}

func TestCanonicalIndexPositiveEligibilityFailure(t *testing.T) {
	db := newCreditTestDB(t)
	require.NoError(t, db.Callback().Row().Before("gorm:row").Register("test:eligibility-failure", func(tx *gorm.DB) {
		if strings.Contains(tx.Statement.SQL.String(), "SELECT EXISTS") {
			tx.AddError(errors.New("eligibility unavailable"))
		}
	}))
	incoming := models.Actress{DMMID: 12, JapaneseName: "New"}
	require.ErrorContains(t, NewActressRepository(db).ImportUpsert(t.Context(), &incoming), "eligibility unavailable")
	require.NoError(t, db.Callback().Row().Remove("test:eligibility-failure"))
	var count int64
	require.NoError(t, db.Model(&models.Actress{}).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, NewActressRepository(db).ImportUpsert(t.Context(), &incoming))
}
