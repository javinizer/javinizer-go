package organizer

import (
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/logging"

	"github.com/spf13/afero"
)

// ConflictKind classifies a destination occupation detected during organize
// planning. Force-update/overwrite authorization suppresses ONLY ConflictFile
// kinds; directories and symlinks always conflict regardless of authorization.
// (#224 Phase C.)
type ConflictKind int

const (
	// ConflictFile is a regular file already occupying the destination — the
	// only kind authorization may suppress.
	ConflictFile ConflictKind = iota
	// ConflictDirectory is a directory at what must be a file path — never
	// authorizable-over; also covers a TargetDir that exists as a regular file
	// (a folder conflict).
	ConflictDirectory
	// ConflictSymlink is a symlink object (live or dangling) at what must be a
	// file path — never authorizable-over (replacement would destroy the link
	// target's metadata chain).
	ConflictSymlink
	// ConflictDuplicate is an intra-batch destination collision detected at
	// plan time (#224 phase E): an earlier file of the same batch already
	// claimed the proven-equal canonical target key. It is reserved from the
	// destination-occupation kinds (no object exists at the destination yet —
	// the collision is between planned outcomes) and flows through the same
	// plan-conflict pipeline; overwrite authorization demotes it to a persisted
	// per-file warning + audit event instead of silently suppressing it.
	ConflictDuplicate
)

// PlanConflict is a destination occupation recorded on a plan.
// Path is always populated; Kind classifies how an authorized run may react.
// String() renders the bare path (parity with the string-slice pipeline that
// predates this type); the full refusal sentence is produced at execute time
// by refuseExistingDestination on its own lane.
type PlanConflict struct {
	Path string
	Kind ConflictKind
}

func (c PlanConflict) String() string { return c.Path }

// folderFallbackUnknown is the fallback folder display name when formats
// produce an empty result (also used for kindName's unknown).
const folderFallbackUnknown = "unknown"

const (
	kindNameFile      = "file"
	kindNameDirectory = "directory"
	kindNameSymlink   = "symlink"
	kindNameDuplicate = "duplicate"
	kindNameUnknown   = folderFallbackUnknown
)

// kindName renders the kind for internal logs/errors; users see the concrete
// refusal sentence at execute time (per D1).
func (c PlanConflict) kindName() string {
	switch c.Kind {
	case ConflictFile:
		return kindNameFile
	case ConflictDirectory:
		return kindNameDirectory
	case ConflictSymlink:
		return kindNameSymlink
	case ConflictDuplicate:
		return kindNameDuplicate
	default:
		return kindNameUnknown
	}
}

// logDestRefusal emits the consistent blocked-destination log when an execute
// path encounters a non-suppressible conflict.
func logDestRefusal(c PlanConflict) {
	logging.Warnf("[organizer] destination conflict (%s) cannot be authorized-over: %s", c.kindName(), c.Path)
}

// refuseIfUnsuppressibleAuthorizedDestination is the authorized-execute
// leg gate (task 2.3): symlink and directory destinations refuse with a new
// sentence (authorized overlays deliberately report a different class); regular-file
// destinations are suppressed per authorization. self/same-inode → no-op per D2.
func refuseIfUnsuppressibleAuthorizedDestination(fs afero.Fs, src, dst string) (identical, sameInode bool, err error) {
	c := classifyExistingDestination(fs, src, dst)
	if c.Err != nil {
		return false, false, c.Err
	}
	if c.Conflict == nil {
		return c.Identical, c.SameInode, nil
	}
	if c.Conflict.Kind == ConflictFile {
		return c.Identical, c.SameInode, nil // suppressed by authorization
	}
	logDestRefusal(*c.Conflict)
	return false, false, fmt.Errorf("cannot authorize-over a %s destination (refusing to replace): %s", c.Conflict.kindName(), c.Conflict.Path)
}

// joinPlanConflictPaths joins conflict paths with "; " matching today's plan
// rendering (bare paths, in order). PlanConflict.String() renders a bare
// path; conflict semantics live in Kind.
func joinPlanConflictPaths(cs []PlanConflict) string {
	paths := make([]string, 0, len(cs))
	for _, c := range cs {
		paths = append(paths, c.String())
	}
	return strings.Join(paths, "; ")
}
