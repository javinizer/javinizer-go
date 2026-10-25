package actressnamesync

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// The subtests share one disposable file-backed database. Read-only defensive
// contracts run first; only the final partial-cancellation case creates jobs.
func TestServicePublicBoundaryContracts(t *testing.T) {
	f := newServiceFixture(t)
	missingJob := "name-" + strings.Repeat("0", 64)
	choices := []database.ActressSyncChoice{{ActressID: f.actor.ID, SourceKey: f.key}}

	t.Run("constructor", func(t *testing.T) {
		s, err := New(nil, f.repo, Options{})
		require.ErrorIs(t, err, ErrInvalid)
		require.Nil(t, s)
		s, err = New(t.Context(), nil, Options{})
		require.ErrorIs(t, err, ErrInvalid)
		require.Nil(t, s)
		uncleanPath := f.directory + string(filepath.Separator) + ".." + string(filepath.Separator) + "unused-artifact"
		for name, opts := range map[string]Options{
			"negative_age":         {MaxAge: -time.Hour},
			"short_age":            {MaxAge: time.Hour - time.Nanosecond},
			"long_age":             {MaxAge: 8760*time.Hour + time.Nanosecond},
			"negative_jobs":        {QueueLimits: database.ActressSyncQueueLimits{MaxQueuedJobs: -1}},
			"too_many_jobs":        {QueueLimits: database.ActressSyncQueueLimits{MaxQueuedJobs: database.MaxActressSyncQueuedJobs + 1}},
			"negative_tasks":       {QueueLimits: database.ActressSyncQueueLimits{MaxQueuedTasks: -1}},
			"too_many_tasks":       {QueueLimits: database.ActressSyncQueueLimits{MaxQueuedTasks: database.MaxActressSyncQueuedTasks + 1}},
			"missing_enabled_path": {Enabled: true},
			"relative_path":        {Enabled: true, ArtifactDirectory: "offline-evidence"},
			"unclean_path":         {Enabled: true, ArtifactDirectory: uncleanPath},
		} {
			t.Run(name, func(t *testing.T) {
				s, err := New(t.Context(), f.repo, opts)
				require.ErrorIs(t, err, ErrInvalid)
				require.Nil(t, s)
			})
		}
		for _, age := range []time.Duration{time.Hour, 8760 * time.Hour} {
			s, err := New(t.Context(), f.repo, Options{MaxAge: age})
			require.NoError(t, err)
			info, err := s.Info(t.Context())
			require.NoError(t, err)
			require.Equal(t, int64(age/time.Second), info.MaxAgeSeconds)
		}
		absent := filepath.Join(f.directory, "not-created-by-constructor")
		s, err = New(t.Context(), f.repo, Options{Enabled: true, ArtifactDirectory: absent})
		require.NoError(t, err, "New validates settings without probing or creating the artifact directory")
		_, err = os.Stat(absent)
		require.ErrorIs(t, err, os.ErrNotExist)
		info, err := s.Info(t.Context())
		require.NoError(t, err)
		require.Equal(t, "starting", info.Code)
		require.False(t, info.Available)
		require.Equal(t, int64((168*time.Hour)/time.Second), info.MaxAgeSeconds)
		require.Equal(t, database.DefaultActressSyncQueuedJobs, s.opts.QueueLimits.MaxQueuedJobs)
		require.Equal(t, database.DefaultActressSyncQueuedTasks, s.opts.QueueLimits.MaxQueuedTasks)
	})

	t.Run("disabled_and_unavailable", func(t *testing.T) {
		cases := []struct {
			name string
			opts Options
			code string
			err  error
		}{
			{name: "disabled", opts: Options{}, code: "disabled", err: ErrDisabled},
			{name: "missing_directory", opts: Options{Enabled: true, ArtifactDirectory: filepath.Join(f.directory, "missing")}, code: "snapshot_unavailable", err: ErrUnavailable},
			{name: "empty_directory", opts: Options{Enabled: true, ArtifactDirectory: t.TempDir()}, code: "snapshot_unavailable", err: ErrUnavailable},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				s, _ := f.service(t, f.repo, tc.opts)
				if !tc.opts.Enabled {
					s.Run() // Disabled startup must close Ready and return, without polling.
				} else {
					s.initialize()
				}
				awaitServiceSignal(t, s.Ready())
				info, err := s.Info(t.Context())
				require.NoError(t, err)
				require.Equal(t, tc.opts.Enabled, info.Enabled)
				require.False(t, info.Available)
				require.Empty(t, info.ArtifactDigest)
				require.Equal(t, tc.code, info.Code)
				_, err = s.Sources(t.Context(), "", 0, 1)
				require.ErrorIs(t, err, tc.err)
				_, err = s.Review(t.Context(), fixturePrincipal, choices)
				require.ErrorIs(t, err, tc.err)
				_, err = s.Confirm(t.Context(), fixturePrincipal, strings.Repeat("0", 64), true)
				require.ErrorIs(t, err, ErrReviewExpired, "unavailable evidence does not authorize an unknown nonce")
				history, err := s.ListJobs(t.Context(), "", 1)
				require.NoError(t, err, "history remains readable without current evidence")
				require.Empty(t, history.Items)
				_, err = s.Job(t.Context(), missingJob)
				require.ErrorIs(t, err, gorm.ErrRecordNotFound)
				_, err = s.Tasks(t.Context(), missingJob, 0, 1)
				require.ErrorIs(t, err, gorm.ErrRecordNotFound)
			})
		}
	})

	t.Run("request_and_lifetime_cancellation", func(t *testing.T) {
		s, stop := f.service(t, f.repo)
		s.initialize()
		methods := map[string]func(context.Context) error{
			"info":    func(ctx context.Context) error { _, err := s.Info(ctx); return err },
			"sources": func(ctx context.Context) error { _, err := s.Sources(ctx, "", 0, 1); return err },
			"review":  func(ctx context.Context) error { _, err := s.Review(ctx, fixturePrincipal, choices); return err },
			"confirm": func(ctx context.Context) error {
				_, err := s.Confirm(ctx, fixturePrincipal, strings.Repeat("0", 64), true)
				return err
			},
			"job":     func(ctx context.Context) error { _, err := s.Job(ctx, missingJob); return err },
			"tasks":   func(ctx context.Context) error { _, err := s.Tasks(ctx, missingJob, 0, 1); return err },
			"history": func(ctx context.Context) error { _, err := s.ListJobs(ctx, "", 1); return err },
			"cancel":  func(ctx context.Context) error { _, err := s.Cancel(ctx, missingJob); return err },
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		for name, call := range methods {
			t.Run("pre_cancelled_"+name, func(t *testing.T) {
				require.ErrorIs(t, call(ctx), context.Canceled)
			})
		}
		stop()
		for name, call := range methods {
			t.Run("stopping_"+name, func(t *testing.T) {
				require.ErrorIs(t, call(context.Background()), ErrStopping)
			})
		}
		requireServiceCounts(t, f.db, 0, 0)
	})

	t.Run("history_validation_and_missing_jobs", func(t *testing.T) {
		s, _ := f.service(t, f.repo)
		s.initialize()
		for _, id := range []string{"", "../current.json", "name-" + strings.Repeat("a", 63), "name-" + strings.Repeat("A", 64), "other" + strings.Repeat("a", 64)} {
			_, err := s.Job(t.Context(), id)
			require.ErrorIs(t, err, ErrInvalid)
			_, err = s.Tasks(t.Context(), id, 0, 1)
			require.ErrorIs(t, err, ErrInvalid)
			_, err = s.Cancel(t.Context(), id)
			require.ErrorIs(t, err, ErrInvalid)
			if id != "" {
				_, err = s.ListJobs(t.Context(), id, 1)
				require.ErrorIs(t, err, ErrInvalid)
			}
		}
		for _, bounds := range [][2]int{{-1, 1}, {0, 0}, {0, 101}} {
			_, err := s.Tasks(t.Context(), missingJob, bounds[0], bounds[1])
			require.ErrorIs(t, err, ErrInvalid)
		}
		for _, limit := range []int{-1, 0, database.MaxActressSyncHistoryJobs + 1} {
			_, err := s.ListJobs(t.Context(), "", limit)
			require.ErrorIs(t, err, ErrInvalid)
		}
		_, err := s.Job(t.Context(), missingJob)
		require.ErrorIs(t, err, gorm.ErrRecordNotFound)
		_, err = s.Tasks(t.Context(), missingJob, 0, 1)
		require.ErrorIs(t, err, gorm.ErrRecordNotFound)
		_, err = s.Cancel(t.Context(), missingJob)
		require.ErrorIs(t, err, gorm.ErrRecordNotFound)
		_, err = s.ListJobs(t.Context(), missingJob, 1)
		require.ErrorIs(t, err, gorm.ErrRecordNotFound)
		history, err := s.ListJobs(t.Context(), "", database.MaxActressSyncHistoryJobs)
		require.NoError(t, err)
		require.NotNil(t, history.Items)
		require.Empty(t, history.Items)
		require.Empty(t, history.NextCursor)
		requireServiceCounts(t, f.db, 0, 0)
	})

	t.Run("partial_apply_and_cancellation_receipts", func(t *testing.T) {
		other := models.Actress{DMMID: 8, FirstName: "Other", LastName: "Actor", JapaneseName: "別旧名"}
		require.NoError(t, f.repo.ImportUpsert(t.Context(), &other))
		key, err := actresscache.SourceKey("r18dev", "r18-only-901")
		require.NoError(t, err)
		s, _ := f.service(t, f.repo)
		s.initialize()
		review, err := s.Review(t.Context(), fixturePrincipal, []database.ActressSyncChoice{choices[0], {ActressID: other.ID, SourceKey: key}})
		require.NoError(t, err)
		job, err := s.Confirm(t.Context(), fixturePrincipal, review.ReviewID, true)
		require.NoError(t, err)
		require.Equal(t, map[string]int{"pending": 2}, job.Counts)
		s.workPass(t.Context(), "", nil)
		partial, err := s.Job(t.Context(), job.ID)
		require.NoError(t, err)
		require.Equal(t, "pending", partial.Status)
		require.Equal(t, map[string]int{"applied": 1, "pending": 1}, partial.Counts)
		first, err := s.Tasks(t.Context(), job.ID, 0, 1)
		require.NoError(t, err)
		require.Len(t, first.Items, 1)
		require.Equal(t, "applied", first.Items[0].Status)
		require.NotNil(t, first.NextOffset)
		require.Equal(t, 1, *first.NextOffset)
		cancelled, err := s.Cancel(t.Context(), job.ID)
		require.NoError(t, err)
		require.True(t, cancelled.Cancelled)
		require.Equal(t, "cancelled", cancelled.Status)
		require.Equal(t, map[string]int{"applied": 1, "cancelled": 1}, cancelled.Counts)
		second, err := s.Tasks(t.Context(), job.ID, *first.NextOffset, 1)
		require.NoError(t, err)
		require.Len(t, second.Items, 1)
		require.Nil(t, second.NextOffset)
		require.Equal(t, "cancelled", second.Items[0].Status)
		require.Equal(t, "job_cancelled", second.Items[0].Reason)
		require.Equal(t, other.ID, second.Items[0].ActressID)
		again, err := s.Tasks(t.Context(), job.ID, 0, 1)
		require.NoError(t, err)
		require.Equal(t, first.Items, again.Items, "cancellation preserves committed receipts")
		end, err := s.Tasks(t.Context(), job.ID, 2, 1)
		require.NoError(t, err)
		require.NotNil(t, end.Items)
		require.Empty(t, end.Items)
		require.Nil(t, end.NextOffset)
		_, err = s.Tasks(t.Context(), job.ID, 3, 1)
		require.ErrorIs(t, err, ErrInvalid)
		replayed, err := s.Confirm(t.Context(), fixturePrincipal, review.ReviewID, true)
		require.NoError(t, err)
		require.Equal(t, cancelled, replayed)
		history, err := s.ListJobs(t.Context(), "", 1)
		require.NoError(t, err)
		require.Len(t, history.Items, 1)
		require.Equal(t, *cancelled, history.Items[0])
		require.Equal(t, job.ID, history.NextCursor)
		last, err := s.ListJobs(t.Context(), history.NextCursor, 1)
		require.NoError(t, err)
		require.Empty(t, last.Items)
		require.Empty(t, last.NextCursor)
		actor, err := f.repo.FindByID(t.Context(), f.actor.ID)
		require.NoError(t, err)
		require.Equal(t, "新名", actor.JapaneseName)
		otherActor, err := f.repo.FindByID(t.Context(), other.ID)
		require.NoError(t, err)
		require.Equal(t, "別旧名", otherActor.JapaneseName)
	})
}
