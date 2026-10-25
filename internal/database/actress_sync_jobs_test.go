package database

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/actresscache/sources/r18dump"
	"github.com/javinizer/javinizer-go/internal/actresssync"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func durableNameFixture(t *testing.T) (*DB, *ActressRepository, *actresscache.Snapshot, ActressSyncReview, time.Time, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "names.db")
	db, err := New(&Config{Type: "sqlite", DSN: path, LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	repo := NewActressRepository(db)
	actor := models.Actress{DMMID: 7, FirstName: "Managed", LastName: "Actor", JapaneseName: "旧名", Aliases: "Keep metadata", ThumbURL: "https://example.invalid/keep.jpg"}
	require.NoError(t, repo.ImportUpsert(t.Context(), &actor))
	now := time.Date(2026, 10, 25, 2, 0, 0, 0, time.UTC)
	limits := actresscache.Limits{MaxBytes: 1 << 20, MaxRecordBytes: 1 << 16, MaxRecords: 100, MaxStringBytes: 4096, MaxTotalStringBytes: 1 << 18, MaxAliases: 32, MaxExternalIDs: 16, MaxTokensPerRecord: 1024}
	dump := "COPY public.derived_actress (id, name_kanji, name_romaji) FROM stdin;\nr18-only-900\t新名\tNot a trusted alias\n\\.\n"
	snapshot, _, err := r18dump.Convert(t.Context(), strings.NewReader(dump), actresscache.Provenance{SourceURL: "https://example.invalid/offline-dump", ParserRevision: "fixture-v1", ObservedAt: now}, r18dump.Limits{MaxInputBytes: 4096, MaxLineBytes: 1024, MaxRows: 10, Output: limits})
	require.NoError(t, err)
	// Persist only synthetic fixture bytes, then exercise the bounded artifact
	// reader rather than passing importer-owned mutable data to the job layer.
	artifactPath := filepath.Join(t.TempDir(), "evidence.ndjson")
	require.NoError(t, os.WriteFile(artifactPath, snapshot.Bytes(), 0600))
	data, err := os.ReadFile(artifactPath)
	require.NoError(t, err)
	snapshot, err = actresscache.Decode(t.Context(), bytes.NewReader(data), limits)
	require.NoError(t, err)
	key, err := actresscache.SourceKey("r18dev", "r18-only-900")
	require.NoError(t, err)
	review, err := repo.ReviewOfflineNameSync(t.Context(), []ActressSyncChoice{{actor.ID, key}}, snapshot, actresssync.Options{Now: now, MaxAge: time.Hour, MaxActors: 10})
	require.NoError(t, err)
	return db, repo, snapshot, *review, now, path
}

func TestConfirmedOfflineNameSyncEndToEndAndReopen(t *testing.T) {
	db, repo, snapshot, review, now, path := durableNameFixture(t)
	actorID := review.Proposals[0].ActressID
	before, err := repo.FindByID(t.Context(), actorID)
	require.NoError(t, err)
	require.NoError(t, db.Create(&models.Movie{ContentID: "name-job-film", ID: "name-job-film", Title: "Fixture"}).Error)
	credit := models.MovieCredit{MovieContentID: "name-job-film", ActressID: actorID, CreditedName: "Reported old name", CreditedJapaneseName: "Reported Japanese", Source: "dmm", Origin: string(models.CreditOriginScrape), UserOverride: true, OverrideName: "Manual display", Suppressed: true}
	require.NoError(t, db.Create(&credit).Error)
	require.NoError(t, db.Create(&models.ActressTranslation{ActressID: actorID, Language: "en", DisplayName: "Old translation"}).Error)
	job, err := repo.SubmitConfirmedOfflineNameSync(t.Context(), "confirm-first", review, snapshot, now)
	require.NoError(t, err)
	require.Len(t, job.Tasks, 1)
	require.Equal(t, "pending", job.Tasks[0].Status)
	unchanged, err := repo.FindByID(t.Context(), actorID)
	require.NoError(t, err)
	require.Equal(t, before, unchanged)
	require.NoError(t, db.Close())
	reopened, err := New(&Config{Type: "sqlite", DSN: path, LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = reopened.Close() })
	require.NoError(t, reopened.RunMigrationsOnStartup(t.Context()))
	repo = NewActressRepository(reopened)
	duplicate, err := repo.SubmitConfirmedOfflineNameSync(t.Context(), "confirm-first", review, snapshot, now)
	require.NoError(t, err)
	require.Equal(t, job.ID, duplicate.ID)
	require.Equal(t, job.Tasks[0].ID, duplicate.Tasks[0].ID)
	lease, err := repo.ClaimOfflineNameSync(t.Context(), job.ID, "worker-a", now, time.Minute)
	require.NoError(t, err)
	result, err := repo.ApplyOfflineNameSync(t.Context(), *lease, snapshot, now.Add(time.Second))
	require.NoError(t, err)
	require.Equal(t, "applied", result.Status)
	require.Equal(t, "旧名", result.BeforeName)
	require.Equal(t, "新名", result.AfterName)
	after, err := repo.FindByID(t.Context(), actorID)
	require.NoError(t, err)
	require.Equal(t, "新名", after.JapaneseName)
	require.Equal(t, before.DMMID, after.DMMID)
	require.Equal(t, before.Origin, after.Origin)
	require.Equal(t, before.FirstName, after.FirstName)
	require.Equal(t, before.LastName, after.LastName)
	require.Equal(t, before.ThumbURL, after.ThumbURL)
	require.Equal(t, before.Aliases, after.Aliases)
	var afterCredit models.MovieCredit
	require.NoError(t, reopened.First(&afterCredit, credit.ID).Error)
	require.Equal(t, credit, afterCredit)
	var translations int64
	require.NoError(t, reopened.Model(&models.ActressTranslation{}).Where("actress_id = ?", actorID).Count(&translations).Error)
	require.Zero(t, translations)
	requireImportProvenance(t, reopened, actorID, importNamesManaged)
	var untrusted int64
	require.NoError(t, reopened.Model(&models.ActressAlias{}).Where("alias_name = ?", "Not a trusted alias").Count(&untrusted).Error)
	require.Zero(t, untrusted)
	_, err = repo.ApplyOfflineNameSync(t.Context(), *lease, snapshot, now.Add(2*time.Second))
	require.ErrorIs(t, err, ErrActressSyncLease)
	finished, err := repo.OfflineNameSyncJob(t.Context(), job.ID)
	require.NoError(t, err)
	require.Equal(t, "applied", finished.Tasks[0].Status)
	// Idempotency after completion returns the receipt rather than reauthorizing.
	duplicate, err = repo.SubmitConfirmedOfflineNameSync(t.Context(), "confirm-first", review, nil, now.Add(24*time.Hour))
	require.NoError(t, err)
	require.Equal(t, "applied", duplicate.Tasks[0].Status)
}

func TestConfirmedOfflineNameSyncDedupeConflictAndConcurrentSubmit(t *testing.T) {
	db, repo, snapshot, review, now, _ := durableNameFixture(t)
	var wg sync.WaitGroup
	results := make(chan *ActressSyncJob, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			j, e := repo.SubmitConfirmedOfflineNameSync(t.Context(), "same-request", review, snapshot, now)
			results <- j
			errs <- e
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var id string
	for j := range results {
		if id == "" {
			id = j.ID
		}
		require.Equal(t, id, j.ID)
	}
	var jobs, tasks int64
	require.NoError(t, db.Model(&ActressSyncJob{}).Count(&jobs).Error)
	require.NoError(t, db.Model(&ActressSyncTask{}).Count(&tasks).Error)
	require.EqualValues(t, 1, jobs)
	require.EqualValues(t, 1, tasks)
	changed := review
	changed.MaxAge = 2 * time.Hour
	_, err := repo.SubmitConfirmedOfflineNameSync(t.Context(), "same-request", changed, snapshot, now)
	require.ErrorIs(t, err, ErrActressSyncConflict)
}

func TestConfirmedOfflineNameSyncLeaseAndCancellation(t *testing.T) {
	_, repo, snapshot, review, now, _ := durableNameFixture(t)
	job, err := repo.SubmitConfirmedOfflineNameSync(t.Context(), "lease-case", review, snapshot, now)
	require.NoError(t, err)
	first, err := repo.ClaimOfflineNameSync(t.Context(), job.ID, "worker-a", now, time.Second)
	require.NoError(t, err)
	_, err = repo.ClaimOfflineNameSync(t.Context(), job.ID, "worker-b", now, time.Second)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	second, err := repo.ClaimOfflineNameSync(t.Context(), job.ID, "worker-b", now.Add(time.Second), time.Second)
	require.NoError(t, err)
	require.Greater(t, second.Generation, first.Generation)
	_, err = repo.ApplyOfflineNameSync(t.Context(), *first, snapshot, now.Add(time.Second))
	require.ErrorIs(t, err, ErrActressSyncLease)
	_, err = repo.ApplyOfflineNameSync(t.Context(), *second, nil, now.Add(time.Second))
	require.ErrorIs(t, err, ErrActressSyncStale)
	require.NoError(t, repo.CancelOfflineNameSync(t.Context(), job.ID))
	require.NoError(t, repo.CancelOfflineNameSync(t.Context(), job.ID))
	_, err = repo.ApplyOfflineNameSync(t.Context(), *second, snapshot, now.Add(time.Second))
	require.ErrorIs(t, err, ErrActressSyncLease)
	job, err = repo.OfflineNameSyncJob(t.Context(), job.ID)
	require.NoError(t, err)
	require.True(t, job.Cancelled)
	require.Equal(t, "cancelled", job.Tasks[0].Status)
	actor, err := repo.FindByID(t.Context(), review.Proposals[0].ActressID)
	require.NoError(t, err)
	require.Equal(t, "旧名", actor.JapaneseName)
}

func TestConfirmedOfflineNameSyncExpiredEvidenceAndUnchanged(t *testing.T) {
	for _, unchanged := range []bool{false, true} {
		t.Run(map[bool]string{false: "stale", true: "unchanged"}[unchanged], func(t *testing.T) {
			db, repo, snapshot, review, now, _ := durableNameFixture(t)
			if unchanged {
				// A fresh explicitly imported fixture can legitimately already have the
				// proposed value; don't clear protection by editing a confirmed identity.
				replacement := models.Actress{DMMID: 17, FirstName: "Second", JapaneseName: "新名"}
				require.NoError(t, repo.ImportUpsert(t.Context(), &replacement))
				next, e := repo.ReviewOfflineNameSync(t.Context(), []ActressSyncChoice{{replacement.ID, review.Proposals[0].SourceKey}}, snapshot, actresssync.Options{Now: now, MaxAge: time.Hour, MaxActors: 1})
				require.NoError(t, e)
				review = *next
			}
			job, e := repo.SubmitConfirmedOfflineNameSync(t.Context(), "terminal", review, snapshot, now)
			require.NoError(t, e)
			at := now
			if !unchanged {
				at = now.Add(2 * time.Hour)
			}
			lease, e := repo.ClaimOfflineNameSync(t.Context(), job.ID, "worker", at, time.Minute)
			require.NoError(t, e)
			result, e := repo.ApplyOfflineNameSync(t.Context(), *lease, snapshot, at)
			require.NoError(t, e)
			if unchanged {
				require.Equal(t, "unchanged", result.Status)
			} else {
				require.Equal(t, "skipped", result.Status)
				require.Equal(t, "stale_evidence", result.Reason)
			}
			var binding actressSyncBinding
			require.NoError(t, db.First(&binding, result.BindingID).Error)
			require.Equal(t, "consumed", binding.Status)
		})
	}
}

func TestConfirmedOfflineNameSyncAdmissionAndStaleReview(t *testing.T) {
	_, repo, snapshot, review, now, _ := durableNameFixture(t)
	for _, key := range []string{"", strings.Repeat("x", 129), " bad"} {
		j, e := repo.SubmitConfirmedOfflineNameSync(t.Context(), key, review, snapshot, now)
		require.Error(t, e)
		require.Nil(t, j)
	}
	for _, ttl := range []time.Duration{0, time.Nanosecond, 25 * time.Hour} {
		l, e := repo.ClaimOfflineNameSync(t.Context(), "none", "owner", now, ttl)
		require.ErrorIs(t, e, ErrActressSyncLease)
		require.Nil(t, l)
	}
	bad := review
	bad.Proposals = append([]ActressSyncProposal(nil), review.Proposals...)
	bad.Proposals[0].AfterName = "Not the reviewed evidence"
	_, err := repo.SubmitConfirmedOfflineNameSync(t.Context(), "tampered", bad, snapshot, now)
	require.ErrorIs(t, err, ErrActressSyncStale)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = repo.SubmitConfirmedOfflineNameSync(ctx, "cancel-before", review, snapshot, now)
	require.ErrorIs(t, err, context.Canceled)
	_, err = repo.ReviewOfflineNameSync(ctx, []ActressSyncChoice{{review.Proposals[0].ActressID, review.Proposals[0].SourceKey}}, snapshot, actresssync.Options{Now: now, MaxAge: time.Hour, MaxActors: 1})
	require.ErrorIs(t, err, context.Canceled)
	_, err = repo.SubmitConfirmedOfflineNameSync(t.Context(), "old-evidence", review, snapshot, now.Add(2*time.Hour))
	require.ErrorIs(t, err, ErrActressSyncStale)
}

func TestConfirmedOfflineNameSyncNoOpAssociationAndImportPreserveWork(t *testing.T) {
	db, repo, snapshot, review, now, _ := durableNameFixture(t)
	job, err := repo.SubmitConfirmedOfflineNameSync(t.Context(), "association", review, snapshot, now)
	require.NoError(t, err)
	actor, err := repo.FindByID(t.Context(), review.Proposals[0].ActressID)
	require.NoError(t, err)
	movie := models.Movie{ContentID: "associate-existing", ID: "associate-existing", Title: "Existing"}
	require.NoError(t, db.Create(&movie).Error)
	require.NoError(t, db.Model(&movie).Association("Actresses").Replace([]models.Actress{*actor}))
	require.NoError(t, repo.ImportUpsert(t.Context(), actor)) // Same values, new UpdatedAt only.
	still, err := repo.OfflineNameSyncJob(t.Context(), job.ID)
	require.NoError(t, err)
	require.Equal(t, syncTaskPending, still.Tasks[0].Status)
	lease, err := repo.ClaimOfflineNameSync(t.Context(), job.ID, "worker", now, time.Minute)
	require.NoError(t, err)
	receipt, err := repo.ApplyOfflineNameSync(t.Context(), *lease, snapshot, now)
	require.NoError(t, err)
	require.Equal(t, "applied", receipt.Status)
}

func TestConfirmedOfflineNameSyncNonAuthorityPreconditionChangeTerminates(t *testing.T) {
	db, repo, snapshot, review, now, _ := durableNameFixture(t)
	job, err := repo.SubmitConfirmedOfflineNameSync(t.Context(), "changed-creation", review, snapshot, now)
	require.NoError(t, err)
	require.NoError(t, db.Model(&models.Actress{}).Where("id = ?", review.Proposals[0].ActressID).Update("created_at", now.Add(-time.Hour)).Error)
	lease, err := repo.ClaimOfflineNameSync(t.Context(), job.ID, "worker", now, time.Minute)
	require.NoError(t, err)
	receipt, err := repo.ApplyOfflineNameSync(t.Context(), *lease, snapshot, now)
	require.NoError(t, err)
	require.Equal(t, "invalidated", receipt.Status)
	require.Equal(t, "identity_changed", receipt.Reason)
	_, err = repo.ClaimOfflineNameSync(t.Context(), job.ID, "worker", now.Add(time.Hour), time.Minute)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	actor, err := repo.FindByID(t.Context(), review.Proposals[0].ActressID)
	require.NoError(t, err)
	require.Equal(t, "旧名", actor.JapaneseName)
}

func TestConfirmedOfflineNameSyncReplaceBeforeFirstBindingCannotInheritOwnership(t *testing.T) {
	db, repo, snapshot, review, now, _ := durableNameFixture(t)
	require.NoError(t, db.Exec("PRAGMA foreign_keys = OFF").Error)
	require.NoError(t, db.Exec("PRAGMA recursive_triggers = OFF").Error)
	id := review.Proposals[0].ActressID
	require.NoError(t, db.Exec("INSERT OR REPLACE INTO actresses (id, dmm_id, first_name, japanese_name, verified, origin) VALUES (?, 7, 'Replacement', 'Other', 1, 'import')", id).Error)
	_, err := repo.ReviewOfflineNameSync(t.Context(), []ActressSyncChoice{{id, review.Proposals[0].SourceKey}}, snapshot, actresssync.Options{Now: now, MaxAge: time.Hour, MaxActors: 1})
	require.ErrorIs(t, err, ErrActressSyncProtected)
}

func TestConfirmedOfflineNameSyncPrimaryKeyMoveCannotInheritOwnership(t *testing.T) {
	db, repo, snapshot, review, now, _ := durableNameFixture(t)
	other := models.Actress{DMMID: 8, FirstName: "Other", JapaneseName: "Other"}
	require.NoError(t, repo.ImportUpsert(t.Context(), &other))
	require.NoError(t, db.Exec("PRAGMA foreign_keys = OFF").Error)
	require.NoError(t, db.Exec("PRAGMA recursive_triggers = OFF").Error)
	require.NoError(t, db.Exec("UPDATE OR REPLACE actresses SET id = ? WHERE id = ?", review.Proposals[0].ActressID, other.ID).Error)
	_, err := repo.ReviewOfflineNameSync(t.Context(), []ActressSyncChoice{{review.Proposals[0].ActressID, review.Proposals[0].SourceKey}}, snapshot, actresssync.Options{Now: now, MaxAge: time.Hour, MaxActors: 1})
	require.ErrorIs(t, err, ErrActressSyncProtected)
}
