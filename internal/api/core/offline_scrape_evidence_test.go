package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/actresscache/artifactstore"
	"github.com/javinizer/javinizer-go/internal/actresscache/offline"
	"github.com/javinizer/javinizer-go/internal/actresscache/sources/r18dump"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrapeevidence"
	"github.com/stretchr/testify/require"
)

func installRuntimeEvidence(t *testing.T, directory, name string) string {
	t.Helper()
	// This installs portable synthetic fixture bytes, not a production publishing
	// fallback. The real reader runs unchanged on Linux and native Windows.
	snapshot, err := actresscache.Build(t.Context(), []actresscache.Evidence{{Source: "r18dev", SourceID: "007", Metadata: actresscache.Metadata{JapaneseName: name}, Provenance: actresscache.Provenance{ObservedAt: time.Now().UTC(), SourceURL: "https://example.invalid/local", DatasetSHA256: strings.Repeat("a", 64), ParserRevision: "test"}}}, offline.DefaultLimits().Store.Artifact)
	require.NoError(t, err)
	manifest, err := json.Marshal(artifactstore.Manifest{Version: 1, Scope: "supplied-file-only", ArtifactSHA256: snapshot.Digest(), ArtifactBytes: int64(len(snapshot.Bytes())), Source: r18dump.Receipt{SHA256: strings.Repeat("a", 64), Bytes: 1, Rows: 1, ActressRows: 1}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(directory, snapshot.Digest()+".ndjson"), snapshot.Bytes(), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "current.json"), append(manifest, '\n'), 0600))
	store, err := artifactstore.Open(directory, offline.DefaultLimits().Store)
	require.NoError(t, err)
	view, err := store.Load(t.Context())
	require.NoError(t, err)
	require.Equal(t, snapshot.Digest(), view.Snapshot.Digest())
	require.NoError(t, store.Close())
	return snapshot.Digest()
}

func TestOfflineEvidenceRuntimeStartupPinnedNoIOAndIndependentConsent(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.Metadata.OfflineScrapeEvidence.Enabled = true
	cfg.Metadata.OfflineScrapeEvidence.ArtifactDirectory = filepath.Join(t.TempDir(), "unprovisioned")
	rt := newOfflineRuntime(t, cfg, nil)
	reader := rt.OfflineScrapeEvidence()
	require.NotNil(t, reader)
	_, err := os.Stat(cfg.Metadata.OfflineScrapeEvidence.ArtifactDirectory)
	require.ErrorIs(t, err, os.ErrNotExist)
	replacement := cfg.Clone()
	replacement.Metadata.OfflineScrapeEvidence.Enabled = false
	rt.SetConfig(replacement)
	require.Same(t, reader, rt.OfflineScrapeEvidence())
	require.Nil(t, rt.OfflineNameSync(), "no mutation-capable repository is required by the independent reader")
	ctx, release, err := rt.BeginOfflineScrapeEvidence(t.Context(), true)
	require.NoError(t, err)
	require.Equal(t, scrapeevidence.Unavailable, scrapeevidence.Resolve(ctx, nil, false).Outcome)
	release()
	release()
	off, releaseOff, err := rt.BeginOfflineScrapeEvidence(ctx, false)
	require.NoError(t, err)
	defer releaseOff()
	require.Nil(t, scrapeevidence.Resolve(off, nil, false), "opt-out strips an inherited private session")
	require.True(t, rt.WaitBackgroundTasks(time.Second))
}

func TestOfflineEvidenceRuntimeShutdownJoinsAndRejectsClosedAdmission(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rt := NewAPIRuntime(nil)
		ctx, release, err := rt.BeginOfflineScrapeEvidence(t.Context(), true)
		require.NoError(t, err)
		done := make(chan struct{})
		go func() { rt.Shutdown(); close(done) }()
		synctest.Wait()
		require.ErrorIs(t, ctx.Err(), context.Canceled)
		select {
		case <-done:
			t.Fatal("shutdown escaped admitted operation")
		default:
		}
		release()
		release()
		synctest.Wait()
		<-done
		_, release, err = rt.BeginOfflineScrapeEvidence(t.Context(), true)
		require.ErrorIs(t, err, ErrOfflineEvidenceShuttingDown)
		require.Nil(t, release)
		admitted, err := rt.AdmitOfflineEvidenceOperation(true)
		require.ErrorIs(t, err, ErrOfflineEvidenceShuttingDown)
		require.Nil(t, admitted)
		// Default-off retains the original no-admission behavior after shutdown.
		off, release, err := rt.BeginOfflineScrapeEvidence(t.Context(), false)
		require.NoError(t, err)
		defer release()
		require.Nil(t, scrapeevidence.Resolve(off, nil, false))
	})
}

func TestOfflineEvidenceRuntimeReadGenerationSeparateFromNameSyncReload(t *testing.T) {
	directory := t.TempDir()
	digestA := installRuntimeEvidence(t, directory, "A")
	db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "isolation.db"), LogLevel: "silent"})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	cfg := config.DefaultConfig(nil, nil)
	cfg.Metadata.OfflineScrapeEvidence.Enabled = true
	cfg.Metadata.OfflineScrapeEvidence.ArtifactDirectory = directory
	cfg.Metadata.ActressDatabase.OfflineNameSync.Enabled = true
	cfg.Metadata.ActressDatabase.OfflineNameSync.ArtifactDirectory = directory
	rt := newOfflineRuntime(t, cfg, database.NewActressRepository(db))
	defer rt.Shutdown()
	rt.StartOfflineNameSync()
	select {
	case <-rt.OfflineNameSync().Ready():
	case <-time.After(5 * time.Second):
		t.Fatal("name sync startup did not finish")
	}
	ctx, release, err := rt.BeginOfflineScrapeEvidenceForFiles(t.Context(), true, 32)
	require.NoError(t, err)
	defer release()
	digestB := installRuntimeEvidence(t, directory, "B")
	require.NotEqual(t, digestA, digestB)
	_, err = rt.OfflineNameSync().Reload(t.Context())
	require.NoError(t, err)
	raw := &models.ScraperResult{Source: "r18dev"}
	raw.SetR18DumpActorReferences([]string{"007"})
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			report := scrapeevidence.Resolve(ctx, []*models.ScraperResult{raw}, false)
			if report.SnapshotDigest != digestA || len(report.Observations) != 1 || report.Observations[0].JapaneseName != "A" {
				t.Errorf("pinned report changed: %+v", report)
			}
		})
	}
	wg.Wait()
	fresh, releaseFresh, err := rt.BeginOfflineScrapeEvidence(t.Context(), true)
	require.NoError(t, err)
	defer releaseFresh()
	require.Equal(t, digestB, scrapeevidence.Resolve(fresh, []*models.ScraperResult{raw}, false).SnapshotDigest)
	encoded, err := json.Marshal(raw)
	require.NoError(t, err)
	var forged models.ScraperResult
	require.NoError(t, json.Unmarshal(encoded, &forged))
	untrusted, releaseUntrusted, err := rt.BeginOfflineScrapeEvidence(t.Context(), true)
	require.NoError(t, err)
	defer releaseUntrusted()
	require.Equal(t, scrapeevidence.UnsupportedIdentity, scrapeevidence.Resolve(untrusted, []*models.ScraperResult{&forged}, false).Outcome)
	// Reload's public status/digest alone is not a private reader context.
	require.Nil(t, scrapeevidence.Resolve(t.Context(), []*models.ScraperResult{raw}, false))
}
