package database

import (
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type quarantineRevision struct {
	Revision         int64
	ConsumedRevision int64
}

func readQuarantineRevision(t *testing.T, db *gorm.DB) quarantineRevision {
	t.Helper()
	var state quarantineRevision
	require.NoError(t, db.Table("actress_quarantine_state").Where("id=1").Take(&state).Error)
	return state
}

func TestQuarantineGatePreservesSinglePassEvidence(t *testing.T) {
	db := newCreditTestDB(t)
	require.NoError(t, db.Exec("INSERT INTO actresses(id,dmm_id,verified,japanese_name,name_key) VALUES(101,77,0,'Fresh','legacy'),(102,0,1,'Legacy',NULL)").Error)
	before := readQuarantineRevision(t, db.DB)
	require.NoError(t, db.Transaction(recomputeActressCandidateQuarantineTx))
	var candidate models.Actress
	require.NoError(t, db.First(&candidate, 101).Error)
	require.True(t, candidate.AmbiguityQuarantined)
	require.Empty(t, candidate.NameKey)
	first := readQuarantineRevision(t, db.DB)
	require.Equal(t, before.Revision, first.ConsumedRevision)
	require.Greater(t, first.Revision, first.ConsumedRevision, "oracle-generated evidence changes stay pending")
	require.NoError(t, db.Transaction(recomputeActressCandidateQuarantineTx))
	require.NoError(t, db.First(&candidate, 101).Error)
	require.False(t, candidate.AmbiguityQuarantined, "second invocation must preserve old oracle's second-pass behavior")
	require.NoError(t, db.Transaction(recomputeActressCandidateQuarantineTx))
	stable := readQuarantineRevision(t, db.DB)
	require.Equal(t, stable.Revision, stable.ConsumedRevision)
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:no-graph", func(tx *gorm.DB) {
		if tx.Statement.Table == "actresses" {
			tx.AddError(errors.New("clean graph was read"))
		}
	}))
	require.NoError(t, db.Transaction(recomputeActressCandidateQuarantineTx))
	require.NoError(t, db.Callback().Query().Remove("test:no-graph"))
}

func TestQuarantineGateRevisionErrorsAndRollback(t *testing.T) {
	for _, stage := range []string{"read", "missing", "oracle", "consume", "outer rollback"} {
		t.Run(stage, func(t *testing.T) {
			db := newCreditTestDB(t)
			require.NoError(t, db.Create(&models.Actress{JapaneseName: "Pending"}).Error)
			before := readQuarantineRevision(t, db.DB)
			switch stage {
			case "missing":
				require.NoError(t, db.Exec("DELETE FROM actress_quarantine_state").Error)
			case "consume":
				require.NoError(t, db.Exec("CREATE TRIGGER fail_consume BEFORE UPDATE OF consumed_revision ON actress_quarantine_state BEGIN SELECT RAISE(ABORT,'consume failed'); END").Error)
			case "read", "oracle":
				require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:gate-error", func(tx *gorm.DB) {
					if (stage == "read" && tx.Statement.Table == "actress_quarantine_state") || (stage == "oracle" && tx.Statement.Table == "actresses") {
						tx.AddError(errors.New("read failed"))
					}
				}))
			}
			err := db.Transaction(func(tx *gorm.DB) error {
				if err := recomputeActressCandidateQuarantineTx(tx); err != nil {
					return err
				}
				return errors.New("outer rollback")
			})
			require.Error(t, err)
			if stage == "missing" {
				require.NoError(t, db.Exec("INSERT INTO actress_quarantine_state VALUES(1,?,?)", before.Revision, before.ConsumedRevision).Error)
			}
			if stage == "consume" {
				require.NoError(t, db.Exec("DROP TRIGGER fail_consume").Error)
			}
			if stage == "read" || stage == "oracle" {
				require.NoError(t, db.Callback().Query().Remove("test:gate-error"))
			}
			require.Equal(t, before, readQuarantineRevision(t, db.DB))
			require.NoError(t, db.Transaction(recomputeActressCandidateQuarantineTx))
			require.NoError(t, db.Transaction(recomputeActressCandidateQuarantineTx))
			stable := readQuarantineRevision(t, db.DB)
			require.Equal(t, stable.Revision, stable.ConsumedRevision)
		})
	}
}
