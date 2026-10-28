package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/actresscache/sources/r18dump"
	"github.com/javinizer/javinizer-go/internal/actresssync"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func previewFixture(t *testing.T) (*actresscache.Snapshot, actresssync.Options, actresscache.Limits) {
	t.Helper()
	now := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	limits := actresscache.Limits{MaxBytes: 1 << 20, MaxRecordBytes: 1 << 16, MaxRecords: 100, MaxStringBytes: 4096, MaxTotalStringBytes: 1 << 18, MaxAliases: 32, MaxExternalIDs: 16, MaxTokensPerRecord: 1024}
	s, err := actresscache.Build(t.Context(), []actresscache.Evidence{{Source: "fixture", SourceID: "independent-7", Provenance: actresscache.Provenance{SourceURL: "https://example.invalid/actors/7", DatasetSHA256: strings.Repeat("a", 64), ParserRevision: "fixture-v1", ObservedAt: now}, Metadata: actresscache.Metadata{JapaneseName: "Incoming", Aliases: []string{"Not trusted"}}, ExternalIDs: []actresscache.ExternalID{{Namespace: "dmm", Value: "7"}}}}, limits)
	require.NoError(t, err)
	return s, actresssync.Options{Now: now, MaxAge: time.Hour, MaxActors: 20}, limits
}

// Snapshot all application tables, including suppression, aliases, quarantine,
// translations, provenance and credit/collision/render state. A preview must not
// repair or mutate any of them, even when the database has a dirty revision.
func previewDatabaseState(t *testing.T, db *DB) map[string][]map[string]interface{} {
	t.Helper()
	var names []string
	require.NoError(t, db.Raw("SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name").Scan(&names).Error)
	result := make(map[string][]map[string]interface{})
	for _, name := range names {
		var rows []map[string]interface{}
		require.NoError(t, db.Table(name).Order("rowid").Find(&rows).Error)
		result[name] = rows
	}
	return result
}

func TestPreviewOfflineSyncSelectedIdentityAndProtection(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	s, o, _ := previewFixture(t)
	managed := models.Actress{DMMID: 7, FirstName: "Canonical", Aliases: "Local metadata"}
	require.NoError(t, repo.ImportUpsert(t.Context(), &managed))
	manual := models.Actress{DMMID: 8, FirstName: "Manual"}
	require.NoError(t, repo.Create(t.Context(), &manual))
	legacy := models.Actress{DMMID: 9, FirstName: "Legacy", Origin: ActressOriginImport, Verified: true}
	require.NoError(t, db.Create(&legacy).Error)
	revoked := models.Actress{DMMID: 10, FirstName: "Revoked"}
	require.NoError(t, repo.ImportUpsert(t.Context(), &revoked))
	require.NoError(t, db.Model(&revoked).Update("first_name", "Manual edit").Error)
	candidate := models.Actress{DMMID: 11, FirstName: "Candidate", Origin: "scrape", Verified: false}
	require.NoError(t, db.Create(&candidate).Error)
	quarantine := models.Actress{DMMID: 12, FirstName: "Quarantined", Origin: ActressOriginImport, Verified: true, AmbiguityQuarantined: true}
	require.NoError(t, db.Create(&quarantine).Error)
	noID := models.Actress{FirstName: "No ID"}
	require.NoError(t, repo.ImportUpsert(t.Context(), &noID))
	// Import reconciliation may change unrelated candidate projections; set the
	// selected quarantined state explicitly immediately before the read-only call.
	require.NoError(t, db.Model(&quarantine).Update("ambiguity_quarantined", true).Error)
	// Seed existing credit overrides/suppression and alias/translation data, not
	// merely empty tables, before checking the no-write guarantee.
	require.NoError(t, db.Create(&models.Movie{ContentID: "preview-movie", ID: "preview-movie", Title: "Existing"}).Error)
	require.NoError(t, db.Create(&models.MovieCredit{MovieContentID: "preview-movie", ActressID: managed.ID, CreditedName: "Screen credit", OverrideName: "Manual override", UserOverride: true, Suppressed: true, Origin: string(models.CreditOriginUser)}).Error)
	require.NoError(t, db.Create(&models.ActressAlias{AliasName: "Trusted alias", CanonicalName: "Canonical"}).Error)
	require.NoError(t, db.Create(&models.ActressTranslation{ActressID: managed.ID, Language: "en", FirstName: "Translated", SourceName: "Canonical"}).Error)
	ids := []uint{99999, managed.ID, manual.ID, legacy.ID, revoked.ID, candidate.ID, quarantine.ID, noID.ID}
	before := previewDatabaseState(t, db)
	p, err := repo.PreviewOfflineSync(t.Context(), ids, s, o)
	require.NoError(t, err)
	want := []actresssync.Outcome{actresssync.Missing, actresssync.EvidenceAvailable, actresssync.Protected, actresssync.Protected, actresssync.Protected, actresssync.Unverified, actresssync.Quarantined, actresssync.MissingIdentity}
	require.Len(t, p.Items, len(ids))
	for i, item := range p.Items {
		require.Equal(t, ids[i], item.Target.ID)
		require.Equal(t, want[i], item.Outcome)
		if i != 1 {
			require.Nil(t, item.Evidence)
		}
	}
	require.Equal(t, s.Digest(), p.SnapshotDigest)
	require.Equal(t, "Incoming", p.Items[1].Evidence.Metadata.JapaneseName)
	require.Equal(t, before, previewDatabaseState(t, db))
	// An explicit user correction after preview must be read afresh. The first
	// returned plan remains diagnostic data and grants no write authority.
	require.NoError(t, repo.UpdateCanonicalFields(t.Context(), managed.ID, "User corrected", "", "", ""))
	p, err = repo.PreviewOfflineSync(t.Context(), []uint{managed.ID}, s, o)
	require.NoError(t, err)
	require.Equal(t, actresssync.Protected, p.Items[0].Outcome)
	require.NoError(t, repo.Delete(t.Context(), managed.ID))
	p, err = repo.PreviewOfflineSync(t.Context(), []uint{managed.ID}, s, o)
	require.NoError(t, err)
	require.Equal(t, actresssync.Missing, p.Items[0].Outcome)
}

func TestPreviewOfflineSyncDumpIDsAreNotDMMIdentity(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	_, o, limits := previewFixture(t)
	actor := models.Actress{DMMID: 7, JapaneseName: "Same name"}
	require.NoError(t, repo.ImportUpsert(t.Context(), &actor))
	dump := "COPY public.derived_actress (id, name_kanji) FROM stdin;\n7\tSame name\n\\.\n"
	snapshot, _, err := r18dump.Convert(t.Context(), strings.NewReader(dump), actresscache.Provenance{SourceURL: "https://example.invalid/dump", ParserRevision: "fixture-v1", ObservedAt: o.Now}, r18dump.Limits{MaxInputBytes: 4096, MaxLineBytes: 1024, MaxRows: 10, Output: limits})
	require.NoError(t, err)
	before := previewDatabaseState(t, db)
	p, err := repo.PreviewOfflineSync(t.Context(), []uint{actor.ID}, snapshot, o)
	require.NoError(t, err)
	require.Equal(t, actresssync.Unmatched, p.Items[0].Outcome)
	require.Equal(t, before, previewDatabaseState(t, db))
}

func TestPreviewOfflineSyncFailsClosed(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	s, o, _ := previewFixture(t)
	for _, ids := range [][]uint{nil, {0}, {1, 1}, make([]uint, 501)} {
		p, err := repo.PreviewOfflineSync(t.Context(), ids, s, o)
		require.Error(t, err)
		require.Nil(t, p)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	p, err := repo.PreviewOfflineSync(ctx, []uint{1}, s, o)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, p)
	require.NoError(t, db.Exec("DROP TABLE actress_import_name_provenance").Error)
	p, err = repo.PreviewOfflineSync(t.Context(), []uint{1}, s, o)
	require.Error(t, err)
	require.Nil(t, p)
}

func TestPreviewOfflineSyncCancellationAfterRead(t *testing.T) {
	// Cancellation may let database/sql discard SQLite's last connection. A
	// shared-memory database then ceases to exist, which is fixture lifetime loss,
	// not a preview write. Use durable disposable storage so the no-mutation
	// assertion remains meaningful after a fresh connection is opened.
	db, err := New(&Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "cancel-preview.db"), LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	repo := NewActressRepository(db)
	s, o, _ := previewFixture(t)
	actor := models.Actress{DMMID: 7, FirstName: "Managed"}
	require.NoError(t, repo.ImportUpsert(t.Context(), &actor))
	before := previewDatabaseState(t, db)
	// Leave a second before the package watchdog to report a stuck acquisition,
	// with a bounded fallback for runs without -timeout. Keep the original
	// driver identity to verify retirement without calling driver methods.
	deadline, ok := t.Deadline()
	if ok {
		deadline = deadline.Add(-time.Second)
	} else {
		deadline = time.Now().Add(time.Minute)
	}
	waitCtx, stopWaiting := context.WithDeadline(t.Context(), deadline)
	defer stopWaiting()
	originalConn, err := sqlDB.Conn(waitCtx)
	require.NoError(t, err)
	var originalDriverConn any
	require.NoError(t, originalConn.Raw(func(driverConn any) error {
		originalDriverConn = driverConn
		return nil
	}))
	require.NoError(t, originalConn.Close())
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	require.NoError(t, db.Callback().Row().After("gorm:row").Register("cancel_offline_preview", func(tx *gorm.DB) {
		// Keep the application callback from returning until the cancellation
		// rollback wins and retires the only connection. Closing the returned rows
		// releases the query lock needed by database/sql's rollback goroutine.
		// This forces the Windows-observed ordering on every platform without
		// depending on a scheduler race or an arbitrary sleep.
		cancel()
		rows, ok := tx.Statement.Dest.(*sql.Rows)
		require.True(t, ok)
		require.NoError(t, rows.Close())
		// Acquiring the sole connection is a barrier for rollback and release,
		// without relying on a five-second polling window.
		conn, err := sqlDB.Conn(waitCtx)
		require.NoError(t, err)
		defer conn.Close()
		require.NoError(t, conn.Raw(func(driverConn any) error {
			require.NotSame(t, originalDriverConn, driverConn, "canceled transaction must retire its connection")
			return nil
		}))
		require.NoError(t, conn.Close())
	}))
	p, err := repo.PreviewOfflineSync(ctx, []uint{actor.ID}, s, o)
	require.NoError(t, db.Callback().Row().Remove("cancel_offline_preview"))
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, p)
	require.Equal(t, before, previewDatabaseState(t, db))
	// A new uncanceled request must still see the original managed identity and
	// evidence; cancellation must not poison the pool or return a partial plan.
	recovered, err := repo.PreviewOfflineSync(t.Context(), []uint{actor.ID}, s, o)
	require.NoError(t, err)
	require.Equal(t, actresssync.EvidenceAvailable, recovered.Items[0].Outcome)
	require.Equal(t, before, previewDatabaseState(t, db))
}

func TestPreviewOfflineSyncMaximumSelection(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	s, o, _ := previewFixture(t)
	ids := make([]uint, actresssync.MaxSelection)
	for i := range ids {
		ids[i] = uint(i + 1)
	}
	o.MaxActors = actresssync.MaxSelection
	p, err := repo.PreviewOfflineSync(t.Context(), ids, s, o)
	require.NoError(t, err)
	require.Len(t, p.Items, len(ids))
	for i, item := range p.Items {
		require.Equal(t, ids[i], item.Target.ID)
		require.Equal(t, actresssync.Missing, item.Outcome)
	}
}
