package database

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestQuarantineGateMigrationLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gate.db")
	db, err := New(&Config{Type: "sqlite", DSN: path, LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	provider := newMigrationProvider(t, sqlDB)
	_, err = provider.DownTo(t.Context(), 24)
	require.NoError(t, err)
	require.NoError(t, db.Exec("INSERT INTO actresses(id,dmm_id,verified,japanese_name,name_key) VALUES(101,77,0,'Fresh','legacy'),(102,0,1,'Legacy',NULL)").Error)
	require.Error(t, db.Transaction(recomputeActressCandidateQuarantineTx), "missing migration must not appear clean")
	_, err = provider.Up(t.Context())
	require.NoError(t, err)
	initial := readQuarantineRevision(t, db.DB)
	require.Greater(t, initial.Revision, initial.ConsumedRevision)
	require.NoError(t, db.Transaction(recomputeActressCandidateQuarantineTx))
	var candidate models.Actress
	require.NoError(t, db.First(&candidate, 101).Error)
	require.True(t, candidate.AmbiguityQuarantined)
	// Down removes only derived revision state/triggers, leaving the one-pass
	// domain result intact and the unconditional oracle usable for repair.
	_, err = provider.DownTo(t.Context(), 24)
	require.NoError(t, err)
	require.NoError(t, db.Transaction(recomputeActressCandidateQuarantineUnconditionallyTx))
	require.NoError(t, db.First(&candidate, 101).Error)
	require.False(t, candidate.AmbiguityQuarantined)
	require.NoError(t, db.Exec("CREATE TRIGGER actress_quarantine_insert AFTER INSERT ON actresses BEGIN SELECT 1; END").Error)
	_, err = provider.Up(t.Context())
	require.Error(t, err)
	var tables int64
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE name='actress_quarantine_state'").Scan(&tables).Error)
	require.Zero(t, tables)
	require.NoError(t, db.Exec("DROP TRIGGER actress_quarantine_insert").Error)
	_, err = provider.Up(t.Context())
	require.NoError(t, err)
	require.NoError(t, db.Close())
	db, err = New(&Config{Type: "sqlite", DSN: path, LogLevel: "silent"})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	state := readQuarantineRevision(t, db.DB)
	require.Equal(t, state.Revision, state.ConsumedRevision)
	// Startup is forced even when normal transaction gates can skip a clean state.
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:startup-force", func(tx *gorm.DB) {
		if tx.Statement.Table == "actresses" {
			tx.AddError(sql.ErrConnDone)
		}
	}))
	require.NoError(t, db.Transaction(recomputeActressCandidateQuarantineTx))
	require.Error(t, backfillActressCandidateNameKeys(t.Context(), db.DB))
	require.NoError(t, db.Callback().Query().Remove("test:startup-force"))
	require.Equal(t, state, readQuarantineRevision(t, db.DB))
}

func TestQuarantineGateExternalWriterImportRetry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "concurrent.db")
	db, err := New(&Config{Type: "sqlite", DSN: path, LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	require.NoError(t, db.Exec("INSERT INTO actresses(id,dmm_id,verified,japanese_name) VALUES(100,0,1,'Shared')").Error)
	require.NoError(t, db.Transaction(recomputeActressCandidateQuarantineTx))
	raw, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	inserted := false
	require.NoError(t, db.Callback().Row().After("gorm:row").Register("test:external-graph", func(tx *gorm.DB) {
		if inserted || !strings.Contains(strings.ToUpper(tx.Statement.SQL.String()), "SELECT EXISTS") {
			return
		}
		inserted = true
		_, err := raw.Exec("INSERT INTO actresses(id,dmm_id,verified,japanese_name) VALUES(101,77,0,'Shared')")
		require.NoError(t, err)
	}))
	incoming := models.Actress{DMMID: 500, JapaneseName: "Unrelated"}
	require.NoError(t, NewActressRepository(db).ImportUpsert(t.Context(), &incoming))
	require.NoError(t, db.Callback().Row().Remove("test:external-graph"))
	require.True(t, inserted)
	var candidate models.Actress
	require.NoError(t, db.First(&candidate, 101).Error)
	require.True(t, candidate.AmbiguityQuarantined, "snapshot retry must reload the committed graph invalidation")
	_, err = raw.Exec("UPDATE actresses SET japanese_name='No longer shared' WHERE id=100")
	require.NoError(t, err)
	require.NoError(t, db.Transaction(recomputeActressCandidateQuarantineTx))
	require.NoError(t, db.First(&candidate, 101).Error)
	require.False(t, candidate.AmbiguityQuarantined)
}

func TestQuarantineGateCleanSnapshotRetryReloadsRevision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clean-snapshot.db")
	db, err := New(&Config{Type: "sqlite", DSN: path, LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	require.NoError(t, db.Exec("INSERT INTO actresses(id,dmm_id,verified,japanese_name) VALUES(100,0,1,'Owner'),(101,77,0,'Unique')").Error)
	require.NoError(t, db.Transaction(recomputeActressCandidateQuarantineTx))
	raw, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })
	changed := false
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:clean-snapshot", func(tx *gorm.DB) {
		if changed || tx.Statement.Table != "actress_quarantine_state" {
			return
		}
		changed = true
		_, err := raw.Exec("UPDATE actresses SET japanese_name='Owner' WHERE id=101")
		require.NoError(t, err)
	}))
	attempts := 0
	require.NoError(t, retryOnLocked(func() error {
		attempts++
		return db.Transaction(func(tx *gorm.DB) error {
			if err := recomputeActressCandidateQuarantineTx(tx); err != nil {
				return err
			}
			return tx.Exec("INSERT INTO actresses(id,dmm_id,verified,japanese_name) VALUES(102,500,1,'Unrelated')").Error
		})
	}))
	require.NoError(t, db.Callback().Query().Remove("test:clean-snapshot"))
	require.GreaterOrEqual(t, attempts, 2, "write after stale clean snapshot must retry")
	var candidate models.Actress
	require.NoError(t, db.First(&candidate, 101).Error)
	require.True(t, candidate.AmbiguityQuarantined)
}

func TestQuarantineGateSameTransactionMutations(t *testing.T) {
	db := newCreditTestDB(t)
	quarantineGateFixture(t, db)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		for i := 0; i < 2; i++ {
			require.NoError(t, recomputeActressCandidateQuarantineTx(tx))
		}
		// Remove both sources of ambiguity, reconcile, restore alias ownership,
		// reconcile again. A cached clean flag would miss one of these transitions.
		require.NoError(t, tx.Exec("UPDATE credit_collisions SET status='resolved'").Error)
		require.NoError(t, tx.Exec("UPDATE actress_aliases SET canonical_name='Missing'").Error)
		require.NoError(t, recomputeActressCandidateQuarantineTx(tx))
		var candidate models.Actress
		require.NoError(t, tx.First(&candidate, 1).Error)
		require.False(t, candidate.AmbiguityQuarantined)
		require.NoError(t, tx.Exec("UPDATE actress_aliases SET canonical_name='Owner'").Error)
		require.NoError(t, recomputeActressCandidateQuarantineTx(tx))
		require.NoError(t, tx.First(&candidate, 1).Error)
		require.True(t, candidate.AmbiguityQuarantined)
		return nil
	}))
}
