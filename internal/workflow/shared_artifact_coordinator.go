package workflow

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/javinizer/javinizer-go/internal/fsutil"
)

type sharedArtifactKeyResolver interface {
	Key(string) string
}

// SharedArtifactCompletionDisposition states whether a completed owner published, restored, or poisoned its destination.
type SharedArtifactCompletionDisposition uint8

const (
	// SharedArtifactPublished means the owner durably published the claimed bytes.
	SharedArtifactPublished SharedArtifactCompletionDisposition = iota
	// SharedArtifactSafeToPromote means publication did not occur or every mutation
	// was successfully compensated back to the preclaim state.
	SharedArtifactSafeToPromote
	// SharedArtifactPoisoned means destination state is uncertain and no contender
	// may publish or consume this bucket for the remainder of the apply scope.
	SharedArtifactPoisoned
)

// SharedArtifactCoordinator coordinates final-path publication within one apply run.
type SharedArtifactCoordinator struct {
	mu       sync.Mutex
	rank     map[string]int
	done     map[string]bool
	paths    map[string]*sharedArtifactPath
	resolver sharedArtifactKeyResolver
	absolute func(string) (string, error)
	changed  chan struct{}
}

type sharedArtifactPath struct {
	candidates map[string]string
	failed     map[string]bool
	owner      string
	digest     string
	published  bool
	poisoned   bool
}

// SharedArtifactClaim is the immutable identity of one publication claim.
// Its canonical destination key is derived exactly once and reused for completion.
type SharedArtifactClaim struct {
	coordinator *SharedArtifactCoordinator
	destination string
	requested   string
	key         string
	contender   string
	digest      string
	owner       bool
}

// OwnsPublication reports whether this claim must publish the artifact.
func (c SharedArtifactClaim) OwnsPublication() bool { return c.owner }

// NewSharedArtifactCoordinator creates a coordinator with deterministic contender priority.
func NewSharedArtifactCoordinator(contenders []string) *SharedArtifactCoordinator {
	return newSharedArtifactCoordinator(contenders, fsutil.NewDestKeyResolver())
}

func newSharedArtifactCoordinator(contenders []string, resolver sharedArtifactKeyResolver) *SharedArtifactCoordinator {
	rank := make(map[string]int, len(contenders))
	for i, contender := range contenders {
		rank[contender] = i
	}
	return &SharedArtifactCoordinator{
		rank: rank, done: make(map[string]bool, len(contenders)),
		paths: make(map[string]*sharedArtifactPath), resolver: resolver, absolute: filepath.Abs, changed: make(chan struct{}),
	}
}

// Claim returns an immutable claim that either owns publication or may consume
// an identical result. The final destination becomes absolute before the
// coordinator's job-local resolver freezes its filesystem posture.
func (c *SharedArtifactCoordinator) Claim(ctx context.Context, destination, digest, contender string) (SharedArtifactClaim, error) {
	if c == nil {
		return SharedArtifactClaim{destination: destination, contender: contender, digest: digest, owner: true}, nil
	}
	absolute, err := c.absolute(destination)
	if err != nil {
		return SharedArtifactClaim{}, fmt.Errorf("canonicalize shared artifact destination %q: %w", destination, err)
	}

	c.mu.Lock()
	rank, known := c.rank[contender]
	if !known {
		c.mu.Unlock()
		return SharedArtifactClaim{}, fmt.Errorf("shared artifact contender is not registered: %s", contender)
	}
	if c.done[contender] {
		c.mu.Unlock()
		return SharedArtifactClaim{}, fmt.Errorf("shared artifact contender is already done: %s", contender)
	}
	// DestKeyResolver is intentionally guarded by the coordinator mutex: its
	// resolver-local posture map is mutable and is not documented as concurrent.
	key := c.resolver.Key(absolute)
	claim := SharedArtifactClaim{coordinator: c, destination: absolute, requested: filepath.Clean(destination), key: key, contender: contender, digest: digest}
	path := c.paths[key]
	if path == nil {
		path = &sharedArtifactPath{candidates: make(map[string]string), failed: make(map[string]bool)}
		c.paths[key] = path
	}
	if existing, ok := path.candidates[contender]; ok && existing != digest {
		c.mu.Unlock()
		return SharedArtifactClaim{}, fmt.Errorf("shared artifact contender changed content for %s", destination)
	}
	for other, existing := range path.candidates {
		if other != contender && existing != digest {
			c.mu.Unlock()
			return SharedArtifactClaim{}, fmt.Errorf("shared artifact content conflict at %s", destination)
		}
	}
	path.candidates[contender] = digest
	for {
		if path.poisoned {
			c.mu.Unlock()
			return SharedArtifactClaim{}, fmt.Errorf("shared artifact destination is poisoned: %s", destination)
		}
		if path.published {
			c.mu.Unlock()
			return claim, nil
		}
		if path.owner == contender {
			claim.owner = true
			c.mu.Unlock()
			return claim, nil
		}
		if path.owner == "" {
			blocked := false
			for other, otherRank := range c.rank {
				if otherRank < rank && !c.done[other] && !path.failed[other] {
					blocked = true
					break
				}
			}
			if !blocked {
				path.owner = contender
				claim.owner = true
				c.mu.Unlock()
				return claim, nil
			}
		}
		changed := c.changed
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return SharedArtifactClaim{}, ctx.Err()
		case <-changed:
		}
		c.mu.Lock()
		path = c.paths[key]
		if path == nil {
			c.mu.Unlock()
			return SharedArtifactClaim{}, fmt.Errorf("shared artifact claim retired before completion: %s", destination)
		}
	}
}

// Complete broadcasts an explicit owner publication outcome. Promotion is legal
// only when the caller proves that the destination is back at its preclaim state.
func (c *SharedArtifactCoordinator) Complete(claim SharedArtifactClaim, disposition SharedArtifactCompletionDisposition) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.notifyLocked()
	if claim.coordinator != c || !claim.owner {
		return
	}
	path := c.paths[claim.key]
	if path == nil || path.owner != claim.contender || path.poisoned {
		return
	}
	path.owner = ""
	switch disposition {
	case SharedArtifactPublished:
		path.published = true
		path.digest = claim.digest
	case SharedArtifactSafeToPromote:
		path.failed[claim.contender] = true
	case SharedArtifactPoisoned:
		path.poisoned = true
	default:
		// Unknown or incomplete outcomes fail closed.
		path.poisoned = true
	}
}

// Done retires a contender and releases any unresolved ownership.
func (c *SharedArtifactCoordinator) Done(contender string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	if _, known := c.rank[contender]; !known {
		c.mu.Unlock()
		return
	}
	if c.done[contender] {
		c.mu.Unlock()
		return
	}
	c.done[contender] = true
	for _, path := range c.paths {
		if path.owner == contender && !path.published {
			path.owner = ""
			path.poisoned = true
		}
	}
	if len(c.done) == len(c.rank) {
		// The coordinator is job-scoped. Once every registered contender has
		// retired, release per-path candidate maps and the resolver posture cache.
		c.paths = nil
		c.rank = nil
		c.done = nil
		c.resolver = nil
	}
	c.notifyLocked()
}

func (c *SharedArtifactCoordinator) notifyLocked() {
	close(c.changed)
	c.changed = make(chan struct{})
	c.mu.Unlock()
}
