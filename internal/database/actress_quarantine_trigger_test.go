package database

import (
	"fmt"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestQuarantineGateInsertVictimQueryPlans(t *testing.T) {
	db := newCreditTestDB(t)
	var sql string
	require.NoError(t, db.Raw("SELECT sql FROM sqlite_master WHERE type='trigger' AND name='actress_quarantine_insert'").Scan(&sql).Error)
	predicate := strings.Split(strings.Split(sql, "\nWHEN ")[1], "\nBEGIN")[0]
	predicate = strings.NewReplacer("NEW.id", "99999", "NEW.dmm_id", "99999", "NEW.verified", "1", "NEW.name_key", "'new-key'").Replace(predicate)
	var plan []struct{ Detail string }
	require.NoError(t, db.Raw("EXPLAIN QUERY PLAN SELECT 1 WHERE "+predicate).Scan(&plan).Error)
	var details []string
	for _, step := range plan {
		details = append(details, step.Detail)
	}
	joined := strings.Join(details, "\n")
	require.NotContains(t, joined, "SCAN victim")
	require.Contains(t, joined, "SEARCH victim USING INTEGER PRIMARY KEY")
	require.Contains(t, joined, "idx_actresses_dmm_id_positive")
	require.Contains(t, joined, "idx_actresses_name_key_candidate")
}

func TestQuarantineGateRawRoleInvalidation(t *testing.T) {
	for _, verified := range []interface{}{nil, 0, 1, 2} {
		for _, dmm := range []interface{}{nil, -1, 0, 10} {
			t.Run(fmt.Sprintf("verified_%v/dmm_%v", verified, dmm), func(t *testing.T) {
				db := newCreditTestDB(t)
				relevant := verified == 0 || (verified == 1 && dmm != 10)
				before := readQuarantineRevision(t, db.DB)
				insertErr := db.Exec("INSERT INTO actresses(id,verified,dmm_id,japanese_name) VALUES(100,?,?, 'Before')", verified, dmm).Error
				if verified == nil {
					require.ErrorContains(t, insertErr, "NOT NULL constraint failed: actresses.verified")
					require.Equal(t, before, readQuarantineRevision(t, db.DB), "rejected NULL role must roll back invalidation")
					return
				}
				require.NoError(t, insertErr)
				after := readQuarantineRevision(t, db.DB)
				require.Equal(t, relevant, after.Revision > before.Revision, "insert role")
				before = after
				require.NoError(t, db.Exec("UPDATE actresses SET japanese_name=NULL WHERE id=100").Error)
				after = readQuarantineRevision(t, db.DB)
				require.Equal(t, relevant, after.Revision > before.Revision, "NULL-safe name change")
				before = after
				require.NoError(t, db.Exec("UPDATE actresses SET japanese_name=NULL WHERE id=100").Error)
				require.Equal(t, before, readQuarantineRevision(t, db.DB), "unchanged name is not evidence mutation")
				for _, column := range []string{"id", "dmm_id", "verified", "name_key"} {
					before = readQuarantineRevision(t, db.DB)
					require.NoError(t, db.Exec("UPDATE actresses SET "+column+"="+column+" WHERE id=100").Error)
					require.Greater(t, readQuarantineRevision(t, db.DB).Revision, before.Revision, "all conflict-bearing updates conservatively invalidate")
				}
				before = readQuarantineRevision(t, db.DB)
				require.NoError(t, db.Exec("UPDATE actresses SET ambiguity_quarantined=1 WHERE id=100").Error)
				require.Equal(t, relevant, readQuarantineRevision(t, db.DB).Revision > before.Revision, "raw quarantine tampering")
				before = readQuarantineRevision(t, db.DB)
				require.NoError(t, db.Exec("DELETE FROM actresses WHERE id=100").Error)
				require.Equal(t, relevant, readQuarantineRevision(t, db.DB).Revision > before.Revision, "delete role")
			})
		}
	}
}

func TestQuarantineGateReplacementVictims(t *testing.T) {
	for _, statement := range []string{
		"INSERT OR REPLACE INTO actresses(id,dmm_id,verified,japanese_name) VALUES(201,88,1,'Replacement')",
		"INSERT OR REPLACE INTO actresses(id,dmm_id,verified,japanese_name) VALUES(203,77,1,'Replacement')",
		"UPDATE OR REPLACE actresses SET dmm_id=77 WHERE id=202",
		"UPDATE OR REPLACE actresses SET id=201 WHERE id=202",
		"INSERT OR REPLACE INTO actresses(id,dmm_id,verified,japanese_name,name_key) VALUES(203,0,0,'Replacement','candidate')",
		"UPDATE OR REPLACE actresses SET verified=0,name_key='candidate' WHERE id=202",
	} {
		t.Run(statement, func(t *testing.T) {
			db := newCreditTestDB(t)
			require.NoError(t, db.Exec("PRAGMA recursive_triggers=OFF").Error)
			require.NoError(t, db.Exec("INSERT INTO actresses(id,dmm_id,verified,japanese_name,name_key) VALUES(201,77,0,'Candidate','candidate'),(202,88,1,'Verified',NULL)").Error)
			before := readQuarantineRevision(t, db.DB)
			require.NoError(t, db.Exec(statement).Error)
			require.Greater(t, readQuarantineRevision(t, db.DB).Revision, before.Revision)
			var original int64
			require.NoError(t, db.Model(&models.Actress{}).Where("id=201 AND japanese_name='Candidate'").Count(&original).Error)
			require.Zero(t, original, "replacement victim was actually removed")
		})
	}
}

func TestQuarantineGateUniqueConstraintContract(t *testing.T) {
	db := newCreditTestDB(t)
	var indexes []struct {
		Name   string
		Unique int
	}
	require.NoError(t, db.Raw("PRAGMA index_list('actresses')").Scan(&indexes).Error)
	var unique []string
	for _, index := range indexes {
		if index.Unique != 0 {
			unique = append(unique, index.Name)
		}
	}
	require.ElementsMatch(t, []string{"idx_actresses_dmm_id_positive", "idx_actresses_name_key_candidate"}, unique, "new uniqueness constraints require revisiting REPLACE victim invalidation")
	var columns []struct {
		Name string
		PK   int
		Type string
	}
	require.NoError(t, db.Raw("PRAGMA table_info('actresses')").Scan(&columns).Error)
	var primary []string
	for _, column := range columns {
		if column.PK != 0 {
			primary = append(primary, column.Name)
			require.Equal(t, "INTEGER", column.Type)
		}
	}
	require.Equal(t, []string{"id"}, primary)
}

func TestQuarantineGateRevisionOverflowConflictPolicies(t *testing.T) {
	for _, policy := range []string{"", " OR IGNORE", " OR REPLACE"} {
		for _, mutation := range []string{"INSERT" + policy + " INTO actresses(id,dmm_id,verified,japanese_name) VALUES(101,88,0,'New')", "UPDATE" + policy + " actresses SET japanese_name='Changed' WHERE id=100", "UPDATE" + policy + " actresses SET dmm_id=88,japanese_name='Changed' WHERE id=100"} {
			t.Run(mutation, func(t *testing.T) {
				db := newCreditTestDB(t)
				require.NoError(t, db.Exec("INSERT INTO actresses(id,dmm_id,verified,japanese_name,origin) VALUES(100,77,0,'Owner','import')").Error)
				require.NoError(t, db.Exec("INSERT INTO actress_import_name_provenance VALUES(100,1)").Error)
				require.NoError(t, db.Transaction(syncActressCanonicalKeysTx))
				require.NoError(t, db.Exec("UPDATE actress_quarantine_state SET revision=9223372036854775807,consumed_revision=9223372036854775807").Error)
				before := quarantineGateSnapshot(t, db)
				state := readQuarantineRevision(t, db.DB)
				var oldKeys []struct {
					NameKey   string
					ActressID uint
				}
				require.NoError(t, db.Table("actress_canonical_keys").Order("name_key,actress_id").Find(&oldKeys).Error)
				require.ErrorContains(t, db.Exec(mutation).Error, "invalid actress quarantine revision")
				require.Equal(t, before, quarantineGateSnapshot(t, db), "authoritative rows and provenance must roll back even under OR IGNORE")
				require.Equal(t, state, readQuarantineRevision(t, db.DB))
				var newKeys []struct {
					NameKey   string
					ActressID uint
				}
				require.NoError(t, db.Table("actress_canonical_keys").Order("name_key,actress_id").Find(&newKeys).Error)
				require.Equal(t, oldKeys, newKeys)
				var dirty int64
				require.NoError(t, db.Table("actress_canonical_keys_dirty").Count(&dirty).Error)
				require.Zero(t, dirty, "derived queue changes roll back with the failed statement")
				// Demonstrate that the attempted authoritative statement is otherwise valid.
				require.NoError(t, db.Exec("UPDATE actress_quarantine_state SET revision=1,consumed_revision=0").Error)
				require.NoError(t, db.Exec(mutation).Error)
				require.NotEqual(t, before, quarantineGateSnapshot(t, db))
			})
		}
	}
}

func TestQuarantineGateReplaceNullVerifiedDefault(t *testing.T) {
	db := newCreditTestDB(t)
	require.NoError(t, db.Exec("INSERT INTO actresses(id,dmm_id,verified,japanese_name) VALUES(101,77,0,'Stage')").Error)
	require.NoError(t, db.Transaction(recomputeActressCandidateQuarantineTx))
	before := readQuarantineRevision(t, db.DB)
	require.Equal(t, before.Revision, before.ConsumedRevision)
	require.NoError(t, db.Exec("INSERT OR REPLACE INTO actresses(id,dmm_id,verified,japanese_name) VALUES(102,0,NULL,'Stage')").Error)
	after := readQuarantineRevision(t, db.DB)
	require.Greater(t, after.Revision, before.Revision, "BEFORE INSERT must invalidate before REPLACE substitutes verified's default")
	var owner models.Actress
	require.NoError(t, db.First(&owner, 102).Error)
	require.True(t, owner.Verified)
	require.NoError(t, db.Transaction(recomputeActressCandidateQuarantineTx))
	var gated models.Actress
	require.NoError(t, db.First(&gated, 101).Error)
	require.True(t, gated.AmbiguityQuarantined)
	require.NoError(t, db.Transaction(recomputeActressCandidateQuarantineUnconditionallyTx))
	var oracle models.Actress
	require.NoError(t, db.First(&oracle, 101).Error)
	require.Equal(t, oracle, gated, "gate and single-pass oracle must preserve identical candidate state")
}
