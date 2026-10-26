package actressnamesync

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"path/filepath"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/actressmetadata"
	"github.com/javinizer/javinizer-go/internal/database"
)

const reviewTTL = 5 * time.Minute
const maxReviews = 8

type ticket struct {
	principal string
	expires   time.Time
	review    database.ActressSyncReview
}
type indexedSource struct {
	choice SourceChoice
	search string
}

// generation is immutable after construction; every read pins the whole bundle.
type generation struct {
	options  Options
	snapshot *actresscache.Snapshot
	sources  []indexedSource
	provider *actressmetadata.SnapshotProvider
	info     SourceInfo
}

// Service owns bounded ephemeral review authority and one durable worker.
// A restart drops unconfirmed reviews; committed request keys remain replayable.
type Service struct {
	ctx         context.Context
	repo        Repository
	opts        Options
	once        sync.Once
	ready       chan struct{}
	wake        chan struct{}
	confirm     chan struct{}
	reviewSlots chan struct{}
	mu          sync.RWMutex
	generation  *generation
	reload      chan struct{}
	readSlots   chan struct{}
	info        SourceInfo
	tickets     map[string]ticket
	workerID    string
	now         func() time.Time
}

// New validates server-owned settings without touching an artifact or database.
func New(lifetime context.Context, repo Repository, opts Options) (*Service, error) {
	if lifetime == nil || repo == nil {
		return nil, ErrInvalid
	}
	if opts.MaxAge == 0 {
		opts.MaxAge = 168 * time.Hour
	}
	if opts.MaxAge < time.Hour || opts.MaxAge > 8760*time.Hour {
		return nil, ErrInvalid
	}
	if opts.QueueLimits.MaxQueuedJobs == 0 {
		opts.QueueLimits.MaxQueuedJobs = database.DefaultActressSyncQueuedJobs
	}
	if opts.QueueLimits.MaxQueuedTasks == 0 {
		opts.QueueLimits.MaxQueuedTasks = database.DefaultActressSyncQueuedTasks
	}
	if opts.QueueLimits.MaxQueuedJobs < 1 || opts.QueueLimits.MaxQueuedJobs > database.MaxActressSyncQueuedJobs || opts.QueueLimits.MaxQueuedTasks < 1 || opts.QueueLimits.MaxQueuedTasks > database.MaxActressSyncQueuedTasks {
		return nil, ErrInvalid
	}
	if opts.Enabled && (!filepath.IsAbs(opts.ArtifactDirectory) || filepath.Clean(opts.ArtifactDirectory) != opts.ArtifactDirectory) {
		return nil, ErrInvalid
	}
	id, err := randomID()
	if err != nil {
		return nil, err
	}
	return &Service{ctx: lifetime, repo: repo, opts: opts, ready: make(chan struct{}), wake: make(chan struct{}, 1), confirm: make(chan struct{}, 1), reload: make(chan struct{}, 1), readSlots: make(chan struct{}, 8), reviewSlots: make(chan struct{}, 4), tickets: make(map[string]ticket), workerID: "offline-" + id, now: time.Now, info: SourceInfo{Enabled: opts.Enabled, Code: "starting", MaxAgeSeconds: int64(opts.MaxAge / time.Second), RestartRequiredForConfigChanges: true}}, nil
}

func randomID() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
func validHex(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
func validJob(id string) bool { return len(id) == 69 && id[:5] == "name-" && validHex(id[5:]) }

// Ready closes after the initial pinned-artifact read, including unavailable states.
func (s *Service) Ready() <-chan struct{} { return s.ready }
func (s *Service) request(ctx context.Context) (context.Context, func(), error) {
	if s.ctx.Err() != nil {
		return nil, nil, ErrStopping
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	linked, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.ctx, cancel)
	return linked, func() { stop(); cancel() }, nil
}

// Info does not disclose local filesystem paths or raw storage failures.
func (s *Service) Info(ctx context.Context) (SourceInfo, error) {
	ctx, done, err := s.request(ctx)
	if err != nil {
		return SourceInfo{}, err
	}
	defer done()
	if err := ctx.Err(); err != nil {
		return SourceInfo{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.info, nil
}
func (s *Service) currentGeneration(digest string) (*generation, error) {
	if digest != "" && !validHex(digest) {
		return nil, ErrInvalid
	}
	if !s.opts.Enabled {
		return nil, ErrDisabled
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.generation == nil {
		return nil, ErrUnavailable
	}
	if digest != "" && digest != s.generation.snapshot.Digest() {
		return nil, ErrStaleGeneration
	}
	return s.generation, nil
}
