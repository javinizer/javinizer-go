package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/actresscache/artifactstore"
	"github.com/javinizer/javinizer-go/internal/actresscache/offline"
	"github.com/javinizer/javinizer-go/internal/actresssync"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func testRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.Chmod(root, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".javinizer-sync-e2e"), []byte(disposableMarker), 0600))
	return root
}
func testFixture(t *testing.T) (string, *database.DB, fixtureDescription) {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("native artifact publication unsupported")
	}
	root := testRoot(t)
	for _, v := range []string{"ready", "stale", "future", "missing"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, "artifacts", v), 0700))
	}
	db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(root, "names.db"), LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	require.NoError(t, provision(root, db))
	b, err := os.ReadFile(filepath.Join(root, "fixture.json"))
	require.NoError(t, err)
	var f fixtureDescription
	require.NoError(t, json.Unmarshal(b, &f))
	return root, db, f
}
func snapshot(t *testing.T, root, variant string) *actresscache.Snapshot {
	t.Helper()
	store, err := artifactstore.Open(filepath.Join(root, "artifacts", variant), offline.DefaultLimits().Store)
	require.NoError(t, err)
	defer func() { require.NoError(t, store.Close()) }()
	v, err := store.Load(t.Context())
	require.NoError(t, err)
	require.False(t, v.Recovered)
	require.Equal(t, "supplied-file-only", v.Manifest.Scope)
	return v.Snapshot
}
func TestFixtureEvidenceAndProtection(t *testing.T) {
	root, db, f := testFixture(t)
	require.Len(t, f.Actors, 7)
	s := snapshot(t, root, "ready")
	require.Len(t, s.Records(), 105)
	require.Equal(t, f.Digests["ready"], s.Digest())
	repo := database.NewActressRepository(db)
	options := actresssync.Options{Now: time.Now(), MaxAge: 24 * time.Hour, MaxActors: 8}
	for name, a := range f.Actors {
		e, ok := s.Get(a.SourceKey)
		require.True(t, ok)
		require.Equal(t, a.AfterName, e.Metadata.JapaneseName)
		require.Empty(t, e.ExternalIDs)
		require.Equal(t, "sync-browser-fixture-v1", e.Provenance.ParserRevision)
		review, err := repo.ReviewOfflineNameSync(t.Context(), []database.ActressSyncChoice{{ActressID: a.ID, SourceKey: a.SourceKey}}, s, options)
		if name == "manual" || name == "guarded" {
			require.ErrorIs(t, err, database.ErrActressSyncProtected)
			continue
		}
		require.NoError(t, err)
		require.Equal(t, a.BeforeName, review.Proposals[0].BeforeName)
		require.Equal(t, a.AfterName, review.Proposals[0].AfterName)
	}
	a := f.Actors["managed"]
	choice := []database.ActressSyncChoice{{ActressID: a.ID, SourceKey: a.SourceKey}}
	for _, v := range []string{"stale", "future"} {
		_, err := repo.ReviewOfflineNameSync(t.Context(), choice, snapshot(t, root, v), options)
		require.ErrorIs(t, err, database.ErrActressSyncStale)
	}
	var count int64
	require.NoError(t, db.Table("actress_sync_jobs").Count(&count).Error)
	require.Zero(t, count)
}
func TestGateOnlyReleasesRealApplyAndRestartPreservesReceipt(t *testing.T) {
	root, db, f := testFixture(t)
	s := snapshot(t, root, "ready")
	repo := database.NewActressRepository(db)
	a := f.Actors["managed"]
	review, err := repo.ReviewOfflineNameSync(t.Context(), []database.ActressSyncChoice{{ActressID: a.ID, SourceKey: a.SourceKey}}, s, actresssync.Options{Now: time.Now(), MaxAge: 24 * time.Hour, MaxActors: 8})
	require.NoError(t, err)
	job, err := repo.SubmitConfirmedOfflineNameSync(t.Context(), "fixture-confirm", *review, s, time.Now())
	require.NoError(t, err)
	gate := &gatedRepository{ActressRepository: repo, root: root, pauseAfter: 0}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	lease, err := gate.ClaimOfflineNameSync(ctx, job.ID, "worker", time.Now(), time.Minute)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, lease)
	require.FileExists(t, filepath.Join(root, "worker.paused"))
	var before models.MovieCredit
	require.NoError(t, db.First(&before).Error)
	unchanged, err := repo.FindByID(t.Context(), a.ID)
	require.NoError(t, err)
	require.Equal(t, a.BeforeName, unchanged.JapaneseName)
	require.NoError(t, os.WriteFile(filepath.Join(root, "worker.release"), nil, 0600))
	lease, err = gate.ClaimOfflineNameSync(t.Context(), job.ID, "worker", time.Now(), time.Minute)
	require.NoError(t, err)
	receipt, err := gate.ApplyOfflineNameSync(t.Context(), *lease, s, time.Now())
	require.NoError(t, err)
	require.Equal(t, "applied", receipt.Status)
	require.Equal(t, 1, gate.applied)
	var after models.MovieCredit
	require.NoError(t, db.First(&after).Error)
	require.Equal(t, before, after)
	var translations int64
	require.NoError(t, db.Table("actress_translations").Where("actress_id = ?", a.ID).Count(&translations).Error)
	require.Zero(t, translations)
	require.NoError(t, db.Close())
	db2, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(root, "names.db"), LogLevel: "silent"})
	require.NoError(t, err)
	defer func() { _ = db2.Close() }()
	repo2 := database.NewActressRepository(db2)
	duplicate, err := repo2.SubmitConfirmedOfflineNameSync(t.Context(), "fixture-confirm", *review, nil, time.Now())
	require.NoError(t, err)
	require.Equal(t, job.ID, duplicate.ID)
	require.Equal(t, "applied", duplicate.Tasks[0].Status)
	stored, err := repo2.FindByID(t.Context(), a.ID)
	require.NoError(t, err)
	require.Equal(t, a.AfterName, stored.JapaneseName)
}
func TestInspectorReadOnlyAllowlist(t *testing.T) {
	root, db, _ := testFixture(t)
	var before int64
	require.NoError(t, db.Table("actress_sync_jobs").Count(&before).Error)
	out, err := os.CreateTemp(t.TempDir(), "inspect")
	require.NoError(t, err)
	defer func() { _ = out.Close() }()
	original := os.Stdout
	os.Stdout = out
	defer func() { os.Stdout = original }()
	require.NoError(t, inspect(root))
	b, err := os.ReadFile(out.Name())
	require.NoError(t, err)
	var rows map[string][]map[string]any
	require.NoError(t, json.Unmarshal(b, &rows))
	require.Len(t, rows, 8)
	require.Len(t, rows["actors"], 7)
	require.Empty(t, rows["jobs"])
	require.Len(t, rows["credits"], 1)
	require.Equal(t, "Manual display", rows["credits"][0]["override_name"])
	var after int64
	require.NoError(t, db.Table("actress_sync_jobs").Count(&after).Error)
	require.Equal(t, before, after)
}
func TestRootRejectsUnsafeInputs(t *testing.T) {
	require.Error(t, validateRoot("relative"))
	root := testRoot(t)
	require.NoError(t, validateRoot(root))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".javinizer-sync-e2e"), []byte("wrong"), 0600))
	require.Error(t, validateRoot(root))
	root = testRoot(t)
	target := filepath.Join(t.TempDir(), "external")
	require.NoError(t, os.WriteFile(target, nil, 0600))
	if runtime.GOOS != "windows" {
		require.NoError(t, os.Symlink(target, filepath.Join(root, "names.db")))
		require.Error(t, validateRoot(root))
	}
}
func TestRunRejectsInvalidOptionsBeforeDatabaseCreation(t *testing.T) {
	old := os.Args
	os.Args = []string{"fixture"}
	defer func() { os.Args = old }()
	for _, tc := range []struct{ key, value string }{{"JAVINIZER_SYNC_E2E_MODE", "invalid"}, {"JAVINIZER_SYNC_E2E_PORT", "80"}, {"JAVINIZER_SYNC_E2E_PAUSE_AFTER", "-2"}} {
		t.Run(tc.key, func(t *testing.T) {
			root := testRoot(t)
			t.Setenv("JAVINIZER_SYNC_E2E_ROOT", root)
			t.Setenv(tc.key, tc.value)
			require.Error(t, run())
			require.NoFileExists(t, filepath.Join(root, "names.db"))
		})
	}
}

func TestInspectorAndCommandRejectMissingStateAndUnsupportedArguments(t *testing.T) {
	root := testRoot(t)
	require.Error(t, inspect(root))
	old := os.Args
	defer func() { os.Args = old }()
	t.Setenv("JAVINIZER_SYNC_E2E_ROOT", root)
	os.Args = []string{"fixture", "unknown"}
	require.Error(t, run())
	os.Args = []string{"fixture", "inspect"}
	require.Error(t, run())
	os.Args = []string{"fixture"}
	require.NoError(t, os.WriteFile(filepath.Join(root, "fixture.json"), []byte("{}"), 0600))
	require.ErrorContains(t, run(), "incomplete fixture initialization")
	require.NoFileExists(t, filepath.Join(root, "names.db"))
}
