package artifactstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/actresscache"
)

// LoadReferencedSnapshot reads exactly one immutable object referenced by an
// already admitted durable job. Callers must obtain digest from that job, never
// an arbitrary request, directory scan, timestamp or guessed latest generation.
// Unlike Load, this does not establish a committed current/previous manifest or
// reconstruct a source receipt. It revalidates exact bytes, canonical encoding
// and the r18dev-only source contract independently of manifest rotation.
//
// The caller must retain all objects referenced by active durable jobs. Missing,
// replaced, corrupt or noncanonical objects fail closed without a fallback or
// retry; the current generation must not be substituted for the requested one.
func (s *Store) LoadReferencedSnapshot(ctx context.Context, digest string) (*actresscache.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validDigest(digest) {
		return nil, fmt.Errorf("invalid referenced artifact digest")
	}
	data, err := s.readRegular(ctx, digest+".ndjson", s.limits.Artifact.MaxBytes)
	if err != nil {
		return nil, err
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != digest {
		return nil, fmt.Errorf("referenced artifact digest mismatch")
	}
	snapshot, err := actresscache.Decode(ctx, bytes.NewReader(data), s.limits.Artifact)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(data, snapshot.Bytes()) {
		return nil, fmt.Errorf("referenced artifact must already be canonical")
	}
	for _, record := range snapshot.Records() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if record.Source != "r18dev" {
			return nil, fmt.Errorf("referenced artifact source must be r18dev")
		}
	}
	return snapshot, nil
}
