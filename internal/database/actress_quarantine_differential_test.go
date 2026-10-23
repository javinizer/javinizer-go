package database

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func quarantineGateFixture(t *testing.T, db *DB) {
	t.Helper()
	require.NoError(t, db.Exec("INSERT INTO actresses(id,dmm_id,verified,japanese_name,name_key) VALUES(1,77,0,'Stage',NULL),(2,0,1,'Owner',NULL),(3,0,0,'Other',NULL),(4,88,1,'Positive',NULL)").Error)
	for _, id := range []string{"one", "two"} {
		require.NoError(t, db.Create(&models.Movie{ID: id, ContentID: id, CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(1, 0)}).Error)
	}
	require.NoError(t, db.Exec("INSERT INTO movie_credits(id,movie_content_id,actress_id) VALUES(10,'one',1),(11,'two',3)").Error)
	require.NoError(t, db.Exec("INSERT INTO movie_actresses(movie_content_id,actress_id) VALUES('one',1),('two',3)").Error)
	require.NoError(t, db.Exec("INSERT INTO actress_aliases(id,alias_name,canonical_name) VALUES(20,'Stage','Owner')").Error)
	require.NoError(t, db.Exec("INSERT INTO credit_collisions(id,credit_id,movie_content_id,field,status) VALUES(30,10,'one','identity_link','open')").Error)
}

func quarantineGateSnapshot(t *testing.T, db *DB) map[string][]map[string]interface{} {
	t.Helper()
	result := make(map[string][]map[string]interface{})
	for _, table := range []string{"actresses", "actress_aliases", "movie_credits", "credit_collisions", "movies", "movie_actresses", "actress_import_name_provenance"} {
		var rows []map[string]interface{}
		order := "id"
		if table == "actress_import_name_provenance" {
			order = "actress_id"
		}
		if table == "movie_actresses" {
			order = "movie_content_id,actress_id"
		}
		require.NoError(t, db.Table(table).Order(order).Find(&rows).Error)
		for _, row := range rows {
			delete(row, "updated_at")
			delete(row, "created_at")
		}
		result[table] = rows
	}
	return result
}

func TestQuarantineGateDifferentialMutationSequences(t *testing.T) {
	for _, foreignKeys := range []bool{false, true} {
		for seed := int64(1); seed <= 4; seed++ {
			t.Run(fmt.Sprintf("foreign_keys_%v/seed_%d", foreignKeys, seed), func(t *testing.T) {
				gated, oracle := newCreditTestDB(t), newCreditTestDB(t)
				for _, db := range []*DB{gated, oracle} {
					require.NoError(t, db.Exec(fmt.Sprintf("PRAGMA foreign_keys=%v", foreignKeys)).Error)
					quarantineGateFixture(t, db)
				}
				compare := func() {
					require.NoError(t, gated.Transaction(recomputeActressCandidateQuarantineTx))
					require.NoError(t, oracle.Transaction(recomputeActressCandidateQuarantineUnconditionallyTx))
					require.Equal(t, quarantineGateSnapshot(t, oracle), quarantineGateSnapshot(t, gated))
				}
				compare()
				random := rand.New(rand.NewSource(seed))
				statements := []string{
					"UPDATE actresses SET japanese_name='Changed' WHERE id=1",
					"UPDATE actresses SET japanese_name=NULL WHERE id=1",
					"UPDATE actresses SET first_name='Ａｌｉｃｅ',last_name='Name' WHERE id=2",
					"UPDATE actresses SET first_name=NULL,last_name=NULL,japanese_name='Owner' WHERE id=2",
					"UPDATE actresses SET dmm_id=NULL WHERE id=2",
					"UPDATE actresses SET dmm_id=-1 WHERE id=2",
					"UPDATE actresses SET dmm_id=999 WHERE id=2",
					"UPDATE actresses SET verified=1 WHERE id=3",
					"UPDATE actresses SET verified=0 WHERE id=3",
					"UPDATE actresses SET name_key='owner' WHERE id=1",
					"UPDATE actresses SET ambiguity_quarantined=NOT ambiguity_quarantined WHERE id=1",
					"UPDATE actress_aliases SET canonical_name='Changed' WHERE id=20",
					"UPDATE actress_aliases SET alias_name='Other',canonical_name='Owner' WHERE id=20",
					"UPDATE movie_credits SET suppressed=NOT suppressed WHERE id=10",
					"UPDATE movie_credits SET movie_content_id='two' WHERE id=10",
					"UPDATE movie_credits SET movie_content_id='one' WHERE id=10",
					"UPDATE movie_credits SET actress_id=4 WHERE id=10",
					"UPDATE movie_credits SET actress_id=1 WHERE id=10",
					"UPDATE credit_collisions SET status='resolved' WHERE id=30",
					"UPDATE credit_collisions SET status='open' WHERE id=30",
					"UPDATE credit_collisions SET field='name' WHERE id=30",
					"UPDATE credit_collisions SET field='identity_link' WHERE id=30",
					"UPDATE credit_collisions SET movie_content_id='two' WHERE id=30",
					"UPDATE credit_collisions SET movie_content_id='one' WHERE id=30",
					"UPDATE credit_collisions SET credit_id=11 WHERE id=30",
					"UPDATE credit_collisions SET credit_id=10 WHERE id=30",
					"INSERT OR REPLACE INTO actress_aliases(id,alias_name,canonical_name) VALUES(20,'Stage','Owner')",
					"INSERT INTO actresses(id,dmm_id,verified,japanese_name) VALUES(90,9000,1,'Unrelated') ON CONFLICT(id) DO UPDATE SET japanese_name='Unrelated again'",
					"DELETE FROM actresses WHERE id=90",
				}
				for i := 0; i < 60; i++ {
					statement := statements[random.Intn(len(statements))]
					for _, db := range []*DB{gated, oracle} {
						require.NoError(t, db.Exec(statement).Error, statement)
					}
					compare()
					if i%7 == 0 {
						compare()
					}
				}
				// Deleting the movie exercises child DELETE invalidation with FK enabled;
				// disabled FKs deliberately preserve the oracle's existing orphan semantics.
				for _, db := range []*DB{gated, oracle} {
					require.NoError(t, db.Exec("DELETE FROM movie_actresses WHERE movie_content_id='one'").Error)
					require.NoError(t, db.Exec("DELETE FROM movies WHERE content_id='one'").Error)
				}
				compare()
				for _, db := range []*DB{gated, oracle} {
					require.NoError(t, db.Exec("DELETE FROM movie_actresses WHERE actress_id=3").Error)
					require.NoError(t, db.Exec("DELETE FROM actresses WHERE id=3").Error)
				}
				compare()
			})
		}
	}
}

func TestQuarantineGateAllEvidenceMutationKinds(t *testing.T) {
	statements := []string{
		"INSERT OR REPLACE INTO actress_aliases(id,alias_name,canonical_name) VALUES(20,'New','Owner')",
		"UPDATE actress_aliases SET canonical_name='Other' WHERE id=20",
		"DELETE FROM actress_aliases WHERE id=20",
		"INSERT OR REPLACE INTO movie_credits(id,movie_content_id,actress_id) VALUES(10,'one',1)",
		"UPDATE movie_credits SET suppressed=1 WHERE id=10",
		"DELETE FROM movie_credits WHERE id=10",
		"INSERT OR REPLACE INTO credit_collisions(id,credit_id,movie_content_id,field,status) VALUES(30,10,'one','identity_link','open')",
		"UPDATE credit_collisions SET status='resolved' WHERE id=30",
		"DELETE FROM credit_collisions WHERE id=30",
	}
	for _, statement := range statements {
		t.Run(statement, func(t *testing.T) {
			db := newCreditTestDB(t)
			quarantineGateFixture(t, db)
			before := readQuarantineRevision(t, db.DB)
			require.NoError(t, db.Exec(statement).Error)
			require.Greater(t, readQuarantineRevision(t, db.DB).Revision, before.Revision)
		})
	}
}

func TestQuarantineGatePublicImportsMatchAlwaysReconcile(t *testing.T) {
	gated, oracle := newCreditTestDB(t), newCreditTestDB(t)
	for _, db := range []*DB{gated, oracle} {
		quarantineGateFixture(t, db)
		for i := 0; i < 3; i++ {
			require.NoError(t, db.Transaction(recomputeActressCandidateQuarantineUnconditionallyTx))
		}
	}
	// Keep this test database dirty after every consumption, reproducing the
	// pre-gate public API's unconditional single oracle pass at every call site.
	require.NoError(t, oracle.Exec("UPDATE actress_quarantine_state SET revision=revision+1").Error)
	require.NoError(t, oracle.Exec("CREATE TRIGGER test_always_reconcile AFTER UPDATE OF consumed_revision ON actress_quarantine_state BEGIN UPDATE actress_quarantine_state SET revision=revision+1; END").Error)
	inputs := []models.Actress{
		{DMMID: 500, JapaneseName: "Fresh import"},
		{DMMID: 501, JapaneseName: "Another import"},
		{DMMID: 500, JapaneseName: "Fresh import"},
		{DMMID: 500, JapaneseName: "Fresh import", Aliases: "Metadata"},
		{DMMID: 77, JapaneseName: "Stage"},
		{ID: 3, JapaneseName: "Other"},
		{DMMID: 500, JapaneseName: "Renamed import", Aliases: "Metadata"},
		{ID: 2, JapaneseName: "Protected owner"},
	}
	for i, input := range inputs {
		options := ActressImportOptions{OverwriteNames: i >= 6}
		left, right := input, input
		leftResult, leftErr := NewActressRepository(gated).ImportUpsertWithOptions(t.Context(), &left, options)
		rightResult, rightErr := NewActressRepository(oracle).ImportUpsertWithOptions(t.Context(), &right, options)
		require.Equal(t, rightErr == nil, leftErr == nil)
		require.Equal(t, rightResult, leftResult)
		require.Equal(t, right.ID, left.ID)
		require.Equal(t, quarantineGateSnapshot(t, oracle), quarantineGateSnapshot(t, gated), "public import %d", i)
	}
}
