// Package actressnamesync runs explicitly confirmed offline Japanese-name jobs.
// It never derives actor identity from names or starts work from a scrape.
package actressnamesync

import (
	"context"
	"errors"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/actresssync"
	"github.com/javinizer/javinizer-go/internal/database"
)

// Service admission errors are safe categories; underlying storage/path errors
// must not be returned verbatim by an HTTP handler.
var (
	ErrReloadBusy      = errors.New("offline evidence reload already running")
	ErrReloadFailed    = errors.New("offline evidence reload failed")
	ErrStaleGeneration = errors.New("offline evidence changed; review again")
	ErrDisabled        = errors.New("offline name sync is disabled")
	ErrUnavailable     = errors.New("offline name evidence is unavailable")
	ErrInvalid         = errors.New("invalid offline name sync request")
	ErrReviewExpired   = errors.New("offline name review is unavailable or expired")
	ErrPrincipal       = errors.New("offline name review belongs to another principal")
	ErrReviewCapacity  = errors.New("offline name review capacity reached")
	ErrStopping        = errors.New("offline name sync is stopping")
)

// Repository is the capability used by the offline worker, not the broad actor API.
type Repository interface {
	ReviewOfflineNameSync(context.Context, []database.ActressSyncChoice, *actresscache.Snapshot, actresssync.Options) (*database.ActressSyncReview, error)
	SubmitConfirmedOfflineNameSyncWithLimits(context.Context, string, database.ActressSyncReview, *actresscache.Snapshot, time.Time, database.ActressSyncQueueLimits) (*database.ActressSyncJob, error)
	OfflineNameSyncJob(context.Context, string) (*database.ActressSyncJob, error)
	OfflineNameSyncJobByRequestKey(context.Context, string) (*database.ActressSyncJob, error)
	ListOfflineNameSyncJobs(context.Context, string, int) ([]database.ActressSyncJob, error)
	DueOfflineNameSyncJobs(context.Context, time.Time, string, int) ([]database.ActressSyncJob, error)
	ClaimOfflineNameSync(context.Context, string, string, time.Time, time.Duration) (*database.ActressSyncLease, error)
	ApplyOfflineNameSync(context.Context, database.ActressSyncLease, *actresscache.Snapshot, time.Time) (*database.ActressSyncTask, error)
	CancelOfflineNameSync(context.Context, string) error
	RecordOfflineNameSyncJobProblem(context.Context, string, string, time.Time) error
	RecordOfflineNameSyncLeaseProblem(context.Context, database.ActressSyncLease, string, time.Time) error
}

// Options are server-owned and pinned until restart. They are never HTTP inputs.
type Options struct {
	Enabled           bool
	ArtifactDirectory string
	MaxAge            time.Duration
	QueueLimits       database.ActressSyncQueueLimits
}

// SourceInfo describes a pinned, validated offline artifact, not source authenticity.
type SourceInfo struct {
	ReloadProblemCode               string `json:"reload_problem_code,omitempty"`
	WorkerProblemCode               string `json:"worker_problem_code,omitempty"`
	Enabled                         bool   `json:"enabled"`
	Available                       bool   `json:"available"`
	Code                            string `json:"code,omitempty"`
	ArtifactDigest                  string `json:"artifact_digest,omitempty"`
	RecordCount                     int    `json:"record_count"`
	Recovered                       bool   `json:"recovered"`
	MaxAgeSeconds                   int64  `json:"max_age_seconds"`
	RestartRequiredForConfigChanges bool   `json:"restart_required_for_config_changes"`
}

// SourceChoice is bounded discovery metadata. Searching names grants no binding.
type SourceChoice struct {
	SourceKey    string    `json:"source_key"`
	SourceID     string    `json:"source_id"`
	JapaneseName string    `json:"japanese_name"`
	Aliases      []string  `json:"aliases"`
	SourceURL    string    `json:"source_url"`
	ObservedAt   time.Time `json:"observed_at"`
}

// SourcePage owns its returned records; NextOffset is nil at the end.
type SourcePage struct {
	Items          []SourceChoice `json:"items"`
	NextOffset     *int           `json:"next_offset"`
	ArtifactDigest string         `json:"artifact_digest"`
}

// ReviewProposal displays exact selected identities and names for human review.
type ReviewProposal struct {
	ActressID  uint         `json:"actress_id"`
	Source     SourceChoice `json:"source"`
	BeforeName string       `json:"before_name"`
	AfterName  string       `json:"after_name"`
}

// Review is an expiring server-held confirmation reference, not a client patch.
type Review struct {
	ReviewID       string           `json:"review_id"`
	ExpiresAt      time.Time        `json:"expires_at"`
	ArtifactDigest string           `json:"artifact_digest"`
	MaxAgeSeconds  int64            `json:"max_age_seconds"`
	Proposals      []ReviewProposal `json:"proposals"`
}

// TaskView excludes internal actor tokens and lease authority from API receipts.
type TaskView struct {
	ID              uint       `json:"id"`
	ActressID       uint       `json:"actress_id"`
	SourceKey       string     `json:"source_key"`
	BeforeName      string     `json:"before_name"`
	AfterName       string     `json:"after_name"`
	Status          string     `json:"status"`
	Reason          string     `json:"reason,omitempty"`
	ProblemCode     string     `json:"problem_code,omitempty"`
	RetryAfterMS    int64      `json:"retry_after_ms,omitempty"`
	ProblemAttempts int        `json:"problem_attempts,omitempty"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
}

// JobView summarizes durable receipts, including partially applied cancellation.
type JobView struct {
	ID             string         `json:"id"`
	SnapshotDigest string         `json:"snapshot_digest"`
	CreatedAt      time.Time      `json:"created_at"`
	Cancelled      bool           `json:"cancelled"`
	Status         string         `json:"status"`
	Counts         map[string]int `json:"counts"`
}

// JobPage is a bounded history page in durable creation order.
type JobPage struct {
	Items      []JobView `json:"items"`
	NextCursor string    `json:"next_cursor,omitempty"`
}

// TaskPage is a bounded page of immutable result and current pending-task state.
type TaskPage struct {
	Items      []TaskView `json:"items"`
	NextOffset *int       `json:"next_offset"`
}
