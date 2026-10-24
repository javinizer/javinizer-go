package actresscache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"sync"
	"sync/atomic"
)

// Snapshot owns immutable evidence and lookup indexes. Accessors return copies.
type Snapshot struct {
	records      []Evidence
	byKey        map[string]int
	byExternalID map[ExternalID][]int
	data         []byte
	digest       string
}

func newSnapshot(entries []keyedEvidence, data []byte) *Snapshot {
	snapshot := &Snapshot{byKey: make(map[string]int), byExternalID: make(map[ExternalID][]int), data: append([]byte(nil), data...)}
	for i, entry := range entries {
		snapshot.records = append(snapshot.records, entry.record)
		snapshot.byKey[entry.key] = i
		for _, id := range entry.record.ExternalIDs {
			snapshot.byExternalID[id] = append(snapshot.byExternalID[id], i)
		}
	}
	digest := sha256.Sum256(snapshot.data)
	snapshot.digest = hex.EncodeToString(digest[:])
	return snapshot
}

// Digest identifies these artifact bytes; it does not authenticate source claims.
func (s *Snapshot) Digest() string { return s.digest }

// Bytes returns a copy of the v1 deterministic NDJSON artifact.
func (s *Snapshot) Bytes() []byte { return append([]byte(nil), s.data...) }

// Records returns all independent evidence records in stable-key order.
func (s *Snapshot) Records() []Evidence {
	records := make([]Evidence, len(s.records))
	for i, record := range s.records {
		records[i] = cloneEvidence(record)
	}
	return records
}

// Get returns a copied source record by its stable key, without identity inference.
func (s *Snapshot) Get(key string) (Evidence, bool) {
	index, ok := s.byKey[key]
	if !ok {
		return Evidence{}, false
	}
	return cloneEvidence(s.records[index]), true
}

// LookupResult preserves every source claim and exposes multiple owners explicitly.
type LookupResult struct {
	Candidates []Evidence
	Ambiguous  bool
}

// Lookup returns exact external-ID claims, never a canonical local actor identity.
func (s *Snapshot) Lookup(namespace, value string) (LookupResult, error) {
	if _, err := SourceKey(namespace, value); err != nil {
		return LookupResult{}, err
	}
	indexes := s.byExternalID[ExternalID{namespace, value}]
	result := LookupResult{Candidates: make([]Evidence, 0, len(indexes)), Ambiguous: len(indexes) > 1}
	for _, index := range indexes {
		result.Candidates = append(result.Candidates, cloneEvidence(s.records[index]))
	}
	return result, nil
}

// ErrBusy reports bounded writer admission before any replacement input is read.
var ErrBusy = errors.New("cache snapshot replacement is busy")

// Store publishes fully constructed immutable snapshots with one admitted writer.
// Its zero value is empty and ready for use; old snapshots remain valid after refresh.
type Store struct {
	writer  sync.Mutex
	current atomic.Pointer[Snapshot]
}

// Current returns the immutable snapshot, or nil before the first publication.
func (s *Store) Current() *Snapshot { return s.current.Load() }

// Replace validates all input before one atomic publication. Failure retains the old
// snapshot; after publication it returns success even if cancellation follows.
func (s *Store) Replace(ctx context.Context, input io.Reader, limits Limits) (*Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !s.writer.TryLock() {
		return nil, ErrBusy
	}
	defer s.writer.Unlock()
	snapshot, err := Decode(ctx, input, limits)
	if err != nil {
		return nil, err
	}
	return s.publish(ctx, snapshot)
}

func (s *Store) publish(ctx context.Context, snapshot *Snapshot) (*Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.current.Store(snapshot)
	return snapshot, nil
}
