package actressnamesync_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/actresscache/artifactstore"
	"github.com/javinizer/javinizer-go/internal/actresscache/offline"
	"github.com/javinizer/javinizer-go/internal/actresscache/sources/r18dump"
	"github.com/javinizer/javinizer-go/internal/actressnamesync"
	"github.com/javinizer/javinizer-go/internal/actresssync"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

type periodicDiscovery struct {
	jobs []database.ActressSyncJob
	err  error
}
type periodicApply struct {
	task *database.ActressSyncTask
	err  error
}

// Only observe and synchronize real repository calls. In particular, do not
// synthesize queue results, signal the service wake channel, or change its clock.
type periodicRecoveryRepository struct {
	actressnamesync.Repository
	discovered chan periodicDiscovery
	release    chan struct{}
	applied    chan periodicApply
	first      bool // Only accessed by the serial Run worker.
}

func (r *periodicRecoveryRepository) DueOfflineNameSyncJobs(ctx context.Context, now time.Time, cursor string, limit int) ([]database.ActressSyncJob, error) {
	jobs, err := r.Repository.DueOfflineNameSyncJobs(ctx, now, cursor, limit)
	if !r.first {
		r.first = true
		r.discovered <- periodicDiscovery{jobs, err}
		select {
		case <-r.release:
		case <-ctx.Done():
		}
	}
	return jobs, err
}

func (r *periodicRecoveryRepository) ApplyOfflineNameSync(ctx context.Context, lease database.ActressSyncLease, snapshot *actresscache.Snapshot, now time.Time) (*database.ActressSyncTask, error) {
	task, err := r.Repository.ApplyOfflineNameSync(ctx, lease, snapshot, now)
	select {
	case r.applied <- periodicApply{task, err}:
	case <-ctx.Done():
	}
	return task, err
}

func awaitPeriodicRecovery[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(15 * time.Second):
		t.Fatal("periodic recovery did not complete")
		var zero T
		return zero
	}
}

func TestPeriodicRecoveryDiscoversDurableJobWithoutWake(t *testing.T) {
	root := t.TempDir()
	db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(root, "names.db"), LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	repo := database.NewActressRepository(db)
	actor := models.Actress{DMMID: 7, FirstName: "Managed", LastName: "Actor", JapaneseName: "旧名", Aliases: "Keep metadata", ThumbURL: "https://example.invalid/keep.jpg"}
	require.NoError(t, repo.ImportUpsert(t.Context(), &actor))
	before, err := repo.FindByID(t.Context(), actor.ID)
	require.NoError(t, err)

	directory := filepath.Join(root, "artifacts")
	require.NoError(t, os.Mkdir(directory, 0700))
	limits := offline.DefaultLimits()
	now := time.Now().UTC()
	dump := "COPY public.derived_actress (id, name_kanji, name_romaji) FROM stdin;\nr18-periodic-900\t新名\tUntrusted source alias\n\\.\n"
	snapshot, receipt, err := r18dump.Convert(t.Context(), strings.NewReader(dump), actresscache.Provenance{SourceURL: "https://example.invalid/offline-dump", ParserRevision: "periodic-fixture-v1", ObservedAt: now}, limits.Source)
	require.NoError(t, err)
	store, err := artifactstore.Open(directory, limits.Store)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		published, err := store.Publish(t.Context(), bytes.NewReader(snapshot.Bytes()), receipt)
		require.NoError(t, err)
		require.True(t, published.Published)
	} else {
		// Publication fails closed on unsupported writers. Install valid synthetic
		// fixture bytes, as other service tests do, then use the real native reader.
		manifest := artifactstore.Manifest{Version: 1, Scope: "supplied-file-only", ArtifactSHA256: snapshot.Digest(), ArtifactBytes: int64(len(snapshot.Bytes())), Source: receipt}
		data, err := json.Marshal(manifest)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(directory, snapshot.Digest()+".ndjson"), snapshot.Bytes(), 0600))
		require.NoError(t, os.WriteFile(filepath.Join(directory, "current.json"), append(data, '\n'), 0600))
	}
	manifestBefore, err := os.ReadFile(filepath.Join(directory, "current.json"))
	require.NoError(t, err)

	observer := &periodicRecoveryRepository{Repository: repo, discovered: make(chan periodicDiscovery, 1), release: make(chan struct{}), applied: make(chan periodicApply, 1)}
	lifetime, cancel := context.WithCancel(t.Context())
	defer cancel()
	service, err := actressnamesync.New(lifetime, observer, actressnamesync.Options{Enabled: true, ArtifactDirectory: directory, MaxAge: time.Hour})
	require.NoError(t, err)
	done := make(chan struct{})
	go func() { service.Run(); close(done) }()
	// Registered last: cancel and join before closing DB/store or removing files,
	// including assertion failures while the observation barrier is held.
	t.Cleanup(func() { cancel(); awaitPeriodicRecovery(t, done) })
	awaitPeriodicRecovery(t, service.Ready())
	initial := awaitPeriodicRecovery(t, observer.discovered)
	require.NoError(t, initial.err)
	require.Empty(t, initial.jobs)
	info, err := service.Info(t.Context())
	require.NoError(t, err)
	require.True(t, info.Available)
	require.Equal(t, snapshot.Digest(), info.ArtifactDigest)

	key, err := actresscache.SourceKey("r18dev", "r18-periodic-900")
	require.NoError(t, err)
	review, err := repo.ReviewOfflineNameSync(t.Context(), []database.ActressSyncChoice{{ActressID: actor.ID, SourceKey: key}}, snapshot, actresssync.Options{Now: time.Now().UTC(), MaxAge: time.Hour, MaxActors: 1})
	require.NoError(t, err)
	require.Len(t, review.Proposals, 1)
	// A durable enqueue outside this worker has no in-process wake notification.
	job, err := repo.SubmitConfirmedOfflineNameSync(t.Context(), "periodic-confirmed", *review, snapshot, time.Now().UTC())
	require.NoError(t, err)
	require.Len(t, job.Tasks, 1)
	require.Equal(t, "pending", job.Tasks[0].Status)
	unchanged, err := repo.FindByID(t.Context(), actor.ID)
	require.NoError(t, err)
	require.Equal(t, before, unchanged)
	close(observer.release)

	applied := awaitPeriodicRecovery(t, observer.applied)
	require.NoError(t, applied.err)
	require.NotNil(t, applied.task)
	require.Equal(t, "applied", applied.task.Status)
	cancel()
	awaitPeriodicRecovery(t, done)
	durable, err := repo.OfflineNameSyncJob(t.Context(), job.ID)
	require.NoError(t, err)
	require.Len(t, durable.Tasks, 1)
	require.Equal(t, "applied", durable.Tasks[0].Status)
	require.Equal(t, "旧名", durable.Tasks[0].BeforeName)
	require.Equal(t, "新名", durable.Tasks[0].AfterName)
	require.EqualValues(t, 1, durable.Tasks[0].LeaseGeneration)
	require.Empty(t, durable.Tasks[0].ProblemCode)
	after, err := repo.FindByID(t.Context(), actor.ID)
	require.NoError(t, err)
	require.Equal(t, "新名", after.JapaneseName)
	require.Equal(t, before.ID, after.ID)
	require.Equal(t, before.DMMID, after.DMMID)
	require.Equal(t, before.FirstName, after.FirstName)
	require.Equal(t, before.LastName, after.LastName)
	require.Equal(t, before.Aliases, after.Aliases)
	require.Equal(t, before.ThumbURL, after.ThumbURL)
	manifestAfter, err := os.ReadFile(filepath.Join(directory, "current.json"))
	require.NoError(t, err)
	require.Equal(t, manifestBefore, manifestAfter)
	artifactAfter, err := os.ReadFile(filepath.Join(directory, snapshot.Digest()+".ndjson"))
	require.NoError(t, err)
	require.Equal(t, snapshot.Bytes(), artifactAfter)
}
