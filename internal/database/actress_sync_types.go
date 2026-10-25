package database

import (
	"errors"
	"time"
)

// Confirmed-name sync errors are fail-closed repository outcomes, not retry permissions.
var (
	ErrActressSyncProtected = errors.New("actress name sync is protected or identity is unavailable")
	ErrActressSyncStale     = errors.New("actress sync confirmation or evidence is stale")
	ErrActressSyncConflict  = errors.New("actress sync request conflicts with existing work or source ownership")
	ErrActressSyncLease     = errors.New("actress sync lease is unavailable or fenced")
)

// ActressSyncChoice is an explicit actor/source association chosen for review.
// SourceKey is never inferred from names or a local DMM ID.
type ActressSyncChoice struct {
	ActressID uint
	SourceKey string
}

// ActressSyncProposal describes the exact identity and Japanese-name change to confirm.
type ActressSyncProposal struct {
	ActressID  uint
	SourceKey  string
	ActorToken string
	BeforeName string
	AfterName  string
}

// ActressSyncReview is a bounded one-shot confirmation payload, not recurring consent.
// A future UI must show the whole proposed batch and obtain explicit confirmation
// before calling SubmitConfirmedOfflineNameSync. All values are revalidated there.
type ActressSyncReview struct {
	SnapshotDigest string
	MaxAge         time.Duration
	Proposals      []ActressSyncProposal
}

// ActressSyncJob is a durable request and its bounded task receipts.
// Completion is derived from Tasks; cancellation does not undo committed changes.
type ActressSyncJob struct {
	ID             string
	RequestKey     string
	RequestHash    string
	SnapshotDigest string
	MaxAgeNS       int64
	CreatedAt      time.Time
	Cancelled      bool
	Tasks          []ActressSyncTask `gorm:"-"`
}

// ActressSyncTask retains identity, evidence, fencing and result history after deletion.
// The transient 'applying' state must never be committed independently of a result.
type ActressSyncTask struct {
	ID              uint
	JobID           string
	ActressID       uint
	BindingID       uint
	BindingRevision int64
	SourceKey       string
	SnapshotDigest  string
	ActorToken      string
	BeforeName      string
	AfterName       string
	Status          string
	Reason          string
	LeaseGeneration int64
	LeaseOwner      string
	LeaseUntilMS    int64 `gorm:"column:lease_until_ms"`
	FinishedAt      *time.Time
}

// ActressSyncLease is the complete fencing token issued by ClaimOfflineNameSync.
// Its times are supplied by the server, never from an untrusted API clock.
type ActressSyncLease struct {
	TaskID     uint
	Generation int64
	Owner      string
}

type actressSyncBinding struct {
	ID             uint
	ActressID      uint
	SourceKey      string
	Revision       int64
	SnapshotDigest string
	ActorToken     string
	JapaneseName   string
	Status         string
}

const (
	syncColReason   = "reason"
	syncTaskPending = "pending"
	syncTaskLeased  = "leased"
)
