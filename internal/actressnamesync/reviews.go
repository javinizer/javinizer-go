package actressnamesync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/actressmetadata"
	"github.com/javinizer/javinizer-go/internal/actresssync"
	"github.com/javinizer/javinizer-go/internal/database"
	"gorm.io/gorm"
)

func validPrincipal(p string) bool {
	return p != "" && len(p) <= 256 && utf8.ValidString(p) && !strings.ContainsAny(p, "\x00\r\n")
}
func requestKey(principal, nonce string) string {
	sum := sha256.Sum256([]byte(principal + "\x00" + nonce))
	return "review-" + hex.EncodeToString(sum[:])
}

// Review retains server-generated proposals under bounded, expiring authority.
func (s *Service) Review(ctx context.Context, principal string, choices []database.ActressSyncChoice) (*Review, error) {
	return s.ReviewAt(ctx, principal, choices, "")
}

// ReviewAt accepts a displayed generation only as an equality precondition.
func (s *Service) ReviewAt(ctx context.Context, principal string, choices []database.ActressSyncChoice, digest string) (*Review, error) {
	ctx, done, err := s.request(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	if !validPrincipal(principal) || len(choices) < 1 || len(choices) > actresssync.MaxSelection {
		return nil, ErrInvalid
	}
	seen := make(map[uint]bool, len(choices))
	for _, c := range choices {
		if c.ActressID == 0 || seen[c.ActressID] || len(c.SourceKey) == 0 || len(c.SourceKey) > 512 {
			return nil, ErrInvalid
		}
		seen[c.ActressID] = true
	}
	select {
	case s.reviewSlots <- struct{}{}:
		defer func() { <-s.reviewSlots }()
	default:
		return nil, ErrReviewCapacity
	}
	g, err := s.currentGeneration(digest)
	if err != nil {
		return nil, err
	}
	snapshot := g.snapshot
	// Only exact selected r18dev references reach the metadata adapter. No claims,
	// numeric equivalence, names, aliases or existing bindings expand identity.
	evidence := make(map[string]actresscache.Evidence, len(choices))
	for _, c := range choices {
		e, ok := snapshot.Get(c.SourceKey)
		if !ok || e.Source != "r18dev" {
			return nil, ErrInvalid
		}
		result, err := g.provider.LookupEvidence(ctx, actressmetadata.Query{Reference: &actressmetadata.Reference{Source: "r18dev", ID: e.SourceID}})
		if err != nil {
			return nil, err
		}
		if result.Outcome != actressmetadata.Available || result.SnapshotDigest != snapshot.Digest() || result.Evidence == nil {
			return nil, ErrUnavailable
		}
		evidence[c.SourceKey] = *result.Evidence
	}
	now := s.now()
	review, err := s.repo.ReviewOfflineNameSync(ctx, choices, snapshot, actresssync.Options{Now: now, MaxAge: g.options.MaxAge, MaxActors: actresssync.MaxSelection})
	if err != nil {
		return nil, err
	}
	response := &Review{ArtifactDigest: review.SnapshotDigest, MaxAgeSeconds: int64(review.MaxAge.Seconds()), ExpiresAt: now.Add(reviewTTL), Proposals: make([]ReviewProposal, 0, len(review.Proposals))}
	for _, p := range review.Proposals {
		e, ok := evidence[p.SourceKey]
		if !ok || len(p.BeforeName) > 4096 || len(p.AfterName) > 4096 {
			return nil, ErrInvalid
		}
		response.Proposals = append(response.Proposals, ReviewProposal{ActressID: p.ActressID, Source: sourceChoice(e), BeforeName: p.BeforeName, AfterName: p.AfterName})
	}
	nonce, err := randomID()
	if err != nil {
		return nil, err
	}
	response.ReviewID = nonce
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.generation != g {
		return nil, ErrStaleGeneration
	}
	for id, t := range s.tickets {
		if !s.now().Before(t.expires) {
			delete(s.tickets, id)
		}
	}
	if len(s.tickets) >= maxReviews {
		return nil, ErrReviewCapacity
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.tickets[nonce] = ticket{principal: principal, expires: response.ExpiresAt, review: *review}
	return response, nil
}

// Confirm consumes only a server-held review. Durable retries use its principal-
// bound key even after restart; an absent ticket cannot authorize a new job.
func (s *Service) Confirm(ctx context.Context, principal, reviewID string, confirmed bool) (*JobView, error) {
	ctx, done, err := s.request(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	if !confirmed || !validPrincipal(principal) || !validHex(reviewID) {
		return nil, ErrInvalid
	}
	key := requestKey(principal, reviewID)
	if job, err := s.repo.OfflineNameSyncJobByRequestKey(ctx, key); err == nil {
		return jobView(job), nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	select {
	case s.confirm <- struct{}{}:
		defer func() { <-s.confirm }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if job, err := s.repo.OfflineNameSyncJobByRequestKey(ctx, key); err == nil {
		return jobView(job), nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	s.mu.Lock()
	t, ok := s.tickets[reviewID]
	if ok && !s.now().Before(t.expires) {
		delete(s.tickets, reviewID)
		ok = false
	}
	s.mu.Unlock()
	if !ok {
		return nil, ErrReviewExpired
	}
	if t.principal != principal {
		return nil, ErrPrincipal
	}
	g, err := s.currentGeneration("")
	if err != nil {
		return nil, err
	}
	snapshot := g.snapshot
	if snapshot.Digest() != t.review.SnapshotDigest {
		return nil, ErrStaleGeneration
	}
	job, err := s.repo.SubmitConfirmedOfflineNameSyncWithLimits(ctx, key, t.review, snapshot, s.now(), g.options.QueueLimits)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	delete(s.tickets, reviewID)
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
	return jobView(job), nil
}
