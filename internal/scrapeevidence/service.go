// Package scrapeevidence presents bounded immutable source evidence. It has no
// HTTP, database, canonical-name, image, alias, or apply capability.
package scrapeevidence

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/actresscache/artifactstore"
	"github.com/javinizer/javinizer-go/internal/actresscache/offline"
	"github.com/javinizer/javinizer-go/internal/actressmetadata"
	"github.com/javinizer/javinizer-go/internal/models"
)

const r18Source = "r18dev"

const (
	// MaxOperations bounds simultaneous pinned immutable generations.
	MaxOperations = 4
	// MaxReports bounds the whole admitted batch, including multipart files.
	MaxReports = 128
	// MaxReferences bounds exact actor references per movie result.
	MaxReferences = 64
	// MaxReportBytes bounds each encoded report (8 MiB per admitted operation).
	MaxReportBytes = 64 << 10
)

// Outcome describes read availability, never identity verification or consent.
type Outcome string

// Evidence diagnostics are separate from the movie scrape's success or failure.
const (
	Disabled            Outcome = "disabled"
	Unavailable         Outcome = "unavailable"
	Busy                Outcome = "busy"
	NoSourceReference   Outcome = "no_source_reference"
	UnsupportedIdentity Outcome = "unsupported_identity"
	Stale               Outcome = "stale"
	Future              Outcome = "future"
	Unmatched           Outcome = "unmatched"
	Ambiguous           Outcome = "ambiguous"
	Available           Outcome = "evidence_available"
	ProviderFailed      Outcome = "provider_failed"
	Cancelled           Outcome = "cancelled"
	BudgetExceeded      Outcome = "budget_exceeded"
)

// Observation contains whole-name source assertions, not applicable field edits.
type Observation struct {
	Source         string     `json:"source"`
	SourceID       string     `json:"source_id,omitempty"`
	Outcome        Outcome    `json:"outcome"`
	JapaneseName   string     `json:"japanese_name,omitempty"`
	Aliases        []string   `json:"aliases,omitempty"`
	SourceURL      string     `json:"source_url,omitempty"`
	ObservedAt     *time.Time `json:"observed_at,omitempty"`
	DatasetSHA256  string     `json:"dataset_sha256,omitempty"`
	ParserRevision string     `json:"parser_revision,omitempty"`
}

// Report is frozen at scrape time and persisted beside provenance, never Movie.
type Report struct {
	Outcome        Outcome       `json:"outcome"`
	SnapshotDigest string        `json:"snapshot_digest,omitempty"`
	AdmittedAt     time.Time     `json:"admitted_at"`
	Recovered      bool          `json:"recovered,omitempty"`
	Observations   []Observation `json:"observations,omitempty"`
}

// Clone gives result storage and API consumers independent ownership.
func (r *Report) Clone() *Report {
	if r == nil {
		return nil
	}
	out := *r
	if r.Observations != nil {
		out.Observations = append([]Observation{}, r.Observations...)
		for i := range out.Observations {
			out.Observations[i].Aliases = append([]string(nil), r.Observations[i].Aliases...)
			if r.Observations[i].ObservedAt != nil {
				observed := *r.Observations[i].ObservedAt
				out.Observations[i].ObservedAt = &observed
			}
		}
	}
	return &out
}

// Options is startup-owned configuration; no operation supplies a client path.
type Options struct {
	Enabled           bool
	ArtifactDirectory string
	MaxAge            time.Duration
}

// Service admits bounded synchronous reads without queues or background workers.
type Service struct {
	opts  Options
	slots chan struct{}
	now   func() time.Time
	load  func(context.Context, string) (artifactstore.View, error)
}

// New validates configuration without accessing the artifact or changing files.
func New(opts Options) (*Service, error) {
	if !opts.Enabled && opts.MaxAge == 0 {
		opts.MaxAge = 168 * time.Hour
	}
	if opts.MaxAge < time.Hour || opts.MaxAge > 8760*time.Hour {
		return nil, fmt.Errorf("evidence max age must be between 1 and 8760 hours")
	}
	if opts.Enabled && (strings.IndexByte(opts.ArtifactDirectory, 0) >= 0 || !filepath.IsAbs(opts.ArtifactDirectory) || filepath.Clean(opts.ArtifactDirectory) != opts.ArtifactDirectory) {
		return nil, fmt.Errorf("evidence artifact directory must be clean and absolute")
	}
	return &Service{opts: opts, slots: make(chan struct{}, MaxOperations), now: time.Now, load: loadArtifact}, nil
}

func loadArtifact(ctx context.Context, directory string) (artifactstore.View, error) {
	store, err := artifactstore.Open(directory, offline.DefaultLimits().Store)
	if err != nil {
		return artifactstore.View{}, err
	}
	defer func() { _ = store.Close() }()
	return store.Load(ctx)
}

type sessionKey struct{}

type session struct {
	mu          sync.RWMutex
	base        Report
	provider    actressmetadata.Provider
	maxAge      time.Duration
	maxReports  int
	reportCalls atomic.Int64
	closed      bool
}

// Begin is the single-result adapter for BeginBatch. A nil service is unavailable.
func (s *Service) Begin(ctx context.Context, requested bool) (context.Context, func()) {
	return s.BeginBatch(ctx, requested, 1)
}

// BeginBatch pins one generation before fanout. Oversize declarations fail as a
// whole before I/O, rather than choosing files by concurrent scheduling order.
// Every returned release must run after all users of the operation have joined.
func (s *Service) BeginBatch(ctx context.Context, requested bool, count int) (context.Context, func()) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !requested {
		if ctx.Value(sessionKey{}) != nil {
			ctx = context.WithValue(ctx, sessionKey{}, (*session)(nil))
		}
		return ctx, func() {}
	}
	now := time.Now()
	if s != nil {
		now = s.now()
	}
	state := &session{base: Report{Outcome: Unavailable, AdmittedAt: now.UTC()}, maxReports: count}
	resultCtx := context.WithValue(ctx, sessionKey{}, state)
	var releaseOnce sync.Once
	held := false
	release := func() {
		releaseOnce.Do(func() {
			state.mu.Lock()
			defer state.mu.Unlock()
			state.closed = true
			state.provider = nil
			if held {
				<-s.slots
			}
		})
	}
	if ctx.Err() != nil {
		state.base.Outcome = Cancelled
		return resultCtx, release
	}
	if s == nil {
		return resultCtx, release
	}
	if !s.opts.Enabled {
		state.base.Outcome = Disabled
		return resultCtx, release
	}
	if count < 1 || count > MaxReports {
		state.base.Outcome = BudgetExceeded
		return resultCtx, release
	}
	select {
	case s.slots <- struct{}{}:
	default:
		state.base.Outcome = Busy
		return resultCtx, release
	}
	// A failed load, cancellation or panic cannot leak an admission slot.
	defer func() {
		if !held {
			<-s.slots
		}
	}()
	view, err := s.load(ctx, s.opts.ArtifactDirectory)
	if ctx.Err() != nil {
		state.base.Outcome = Cancelled
		return resultCtx, release
	}
	if err != nil || view.Snapshot == nil {
		return resultCtx, release
	}
	// This constructor validates only the fixed namespace, not mutable input.
	provider, _ := actressmetadata.NewSnapshotProvider(view.Snapshot, r18Source)
	state.provider = provider
	state.maxAge = s.opts.MaxAge
	state.base.Outcome = NoSourceReference
	state.base.SnapshotDigest = view.Snapshot.Digest()
	state.base.Recovered = view.Recovered
	held = true
	return resultCtx, release
}

// Carry transfers only an existing private operation to a destination context.
// Destination cancellation replaces the source request's lifetime intentionally.
func Carry(dst, src context.Context) context.Context {
	if dst == nil {
		dst = context.Background()
	}
	var state *session
	if src != nil {
		state, _ = src.Value(sessionKey{}).(*session)
	}
	return context.WithValue(dst, sessionKey{}, state)
}

func failed(report *Report, outcome Outcome) *Report {
	report.Outcome = outcome
	report.Observations = nil
	return report
}

// Resolve is called only after a successful movie scrape. It never changes raw
// results, canonical metadata, or the movie scrape's outcome. Cached/decoded
// views cannot recover authority by supplying legacy IDs or public JSON claims.
func Resolve(ctx context.Context, raw []*models.ScraperResult, cached bool) *Report {
	if ctx == nil {
		return nil
	}
	state, _ := ctx.Value(sessionKey{}).(*session)
	if state == nil {
		return nil
	}
	state.mu.RLock()
	defer state.mu.RUnlock()
	report := state.base.Clone()
	if state.closed || ctx.Err() != nil {
		return failed(report, Cancelled)
	}
	if state.provider == nil {
		return report
	}
	if state.reportCalls.Add(1) > int64(state.maxReports) || len(raw) > MaxReferences {
		return failed(report, BudgetExceeded)
	}
	if cached {
		return report
	}
	seen := make(map[string]bool)
	total := 0
	for _, result := range raw {
		if ctx.Err() != nil {
			return failed(report, Cancelled)
		}
		if result == nil || result.Source != r18Source {
			continue
		}
		refs := result.R18DumpActorReferences()
		if refs == nil {
			report.Observations = append(report.Observations, Observation{Source: r18Source, Outcome: UnsupportedIdentity})
			continue
		}
		total += len(refs)
		if total > MaxReferences {
			return failed(report, BudgetExceeded)
		}
		for _, id := range refs {
			if _, err := actresscache.SourceKey(r18Source, id); err != nil {
				return failed(report, UnsupportedIdentity)
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			observation, outcome := state.resolveOne(ctx, id)
			if outcome != "" {
				return failed(report, outcome)
			}
			report.Observations = append(report.Observations, observation)
		}
	}
	if ctx.Err() != nil {
		return failed(report, Cancelled)
	}
	for _, observation := range report.Observations {
		if outcomeRank(observation.Outcome) > outcomeRank(report.Outcome) {
			report.Outcome = observation.Outcome
		}
	}
	encoded, err := json.Marshal(report)
	if err != nil || len(encoded) > MaxReportBytes {
		return failed(report, BudgetExceeded)
	}
	if ctx.Err() != nil {
		return failed(report, Cancelled)
	}
	return report
}

func (s *session) resolveOne(ctx context.Context, id string) (Observation, Outcome) {
	observation := Observation{Source: r18Source, SourceID: id}
	result, err := s.provider.LookupEvidence(ctx, actressmetadata.Query{Reference: &actressmetadata.Reference{Source: r18Source, ID: id}})
	if ctx.Err() != nil {
		return observation, Cancelled
	}
	if err != nil || result.SnapshotDigest != s.base.SnapshotDigest || (result.Outcome != actressmetadata.Available && result.Evidence != nil) {
		return observation, ProviderFailed
	}
	switch result.Outcome {
	case actressmetadata.Unmatched:
		observation.Outcome = Unmatched
	case actressmetadata.Ambiguous:
		observation.Outcome = Ambiguous
	case actressmetadata.Available:
		record := result.Evidence
		if record == nil || record.Source != r18Source || record.SourceID != id || record.Provenance.ObservedAt.IsZero() || record.Provenance.DatasetSHA256 == "" || record.Provenance.ParserRevision == "" || record.Provenance.SourceURL == "" {
			return observation, ProviderFailed
		}
		observed := record.Provenance.ObservedAt
		observation.ObservedAt = &observed
		observation.SourceURL = presentationURL(record.Provenance.SourceURL)
		observation.DatasetSHA256 = record.Provenance.DatasetSHA256
		observation.ParserRevision = record.Provenance.ParserRevision
		switch age := s.base.AdmittedAt.Sub(observed); {
		case age < 0:
			observation.Outcome = Future
		case age > s.maxAge:
			observation.Outcome = Stale
		default:
			observation.Outcome = Available
			observation.JapaneseName = record.Metadata.JapaneseName
			observation.Aliases = append([]string(nil), record.Metadata.Aliases...)
		}
	default:
		return observation, ProviderFailed
	}
	return observation, ""
}

func presentationURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return ""
	}
	u.User, u.RawQuery, u.Fragment, u.RawFragment = nil, "", "", ""
	u.ForceQuery = false
	return u.String()
}

func outcomeRank(outcome Outcome) int {
	switch outcome {
	case Ambiguous:
		return 6
	case Future:
		return 5
	case Stale:
		return 4
	case UnsupportedIdentity:
		return 3
	case Available:
		return 2
	case Unmatched:
		return 1
	default:
		return 0
	}
}
