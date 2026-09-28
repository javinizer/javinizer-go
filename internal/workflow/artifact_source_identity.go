package workflow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/afero"

	"github.com/javinizer/javinizer-go/internal/organizer"
)

// errArtifactSourceChanged classifies the fail-closed publication refusal when
// a source path about to be consumed directly from the user's directory no
// longer names the regular file admitted at preparation time: the file was
// replaced, rewritten, or cannot be re-proven. The publication must abort and
// leave the current directory entry untouched — its bytes were never admitted.
var errArtifactSourceChanged = errors.New("artifact source changed since admission")

// artifactSourceIdentity pins which regular file a path named when
// prepareArtifact admitted it: dev+inode where the filesystem exposes a POSIX
// Stat_t (afero.OsFs on unix, including network mounts surfaced through the OS
// VFS), plus size and modtime on every platform. In-memory afero filesystems
// return Sys()==nil and keep only the size+modtime legs — the same posture as
// the downloader/history identity helpers. The tuple deliberately avoids
// hashing: admission must not read multi-GB video payloads a second time.
type artifactSourceIdentity struct {
	known     bool
	hasDevIno bool
	dev       uint64
	ino       uint64
	size      int64
	modTime   time.Time
}

func captureArtifactSourceIdentity(info os.FileInfo) artifactSourceIdentity {
	if info == nil || !info.Mode().IsRegular() {
		return artifactSourceIdentity{}
	}
	id := artifactSourceIdentity{known: true, size: info.Size(), modTime: info.ModTime()}
	if dev, ino, ok := artifactSourceDevIno(info); ok {
		id.hasDevIno = true
		id.dev, id.ino = dev, ino
	}
	return id
}

// matches re-derives the identity legs from a fresh lookup: dev/inode must
// agree whenever BOTH sides expose it (a rename-swap necessarily changes the
// inode even with size and mtime restored), then size and modtime on every
// platform. A nil or non-regular current entry never matches — under the
// no-follow lookup a symlink planted at the admitted pathname reports its own
// ModeSymlink entry, so it fails regularity even when its TARGET still names
// the admitted inode.
func (id artifactSourceIdentity) matches(info os.FileInfo) bool {
	if !id.known || info == nil || !info.Mode().IsRegular() {
		return false
	}
	if id.hasDevIno {
		if dev, ino, ok := artifactSourceDevIno(info); ok && (dev != id.dev || ino != id.ino) {
			return false
		}
	}
	return info.Size() == id.size && info.ModTime().Equal(id.modTime)
}

// lstatArtifactSource resolves path WITHOUT following a final symlink where
// the filesystem exposes the distinction (afero.Lstater: OsFs and wrappers
// that forward it). The following Stat is not an acceptable substitute on
// real filesystems: a rename-aside plus symlink plant at the admitted
// pathname resolves to the admitted inode through Stat, so the identity proof
// would pass and a same-volume publish would then move the LINK object into
// the library (a broken relative link), leaving the video behind. In-memory
// afero filesystems answer Stat-based (LstatIfPossible reports didLstat=false)
// and have no symlink model at all, so there the answer is regular-or-absent
// by construction — the documented test-time posture, matching
// scanner.lstatInfo and fsutil.asideLstat.
func lstatArtifactSource(fs afero.Fs, path string) (os.FileInfo, error) {
	if lst, ok := fs.(afero.Lstater); ok {
		info, _, err := lst.LstatIfPossible(path)
		return info, err
	}
	return fs.Stat(path)
}

// revalidateAdmittedSource proves — immediately before a publish leg moves,
// copies, or removes path — that it still names the regular file admitted at
// preparation time. The lookup never follows a final symlink: any symlink (or
// other non-regular) directory entry at path refuses the leg. Any capture gap
// (admission never pinned this path) skips the proof; any lookup failure or
// identity drift refuses the leg.
func (s *artifactStage) revalidateAdmittedSource(path string, admitted artifactSourceIdentity) error {
	if !admitted.known {
		return nil
	}
	info, err := lstatArtifactSource(s.fs, path)
	if err != nil {
		return fmt.Errorf("%w: revalidate %s: %v", errArtifactSourceChanged, path, err)
	}
	if !admitted.matches(info) {
		return fmt.Errorf("%w: %s", errArtifactSourceChanged, path)
	}
	return nil
}

// revalidateDirectSources re-proves every real source path the plan execution
// is about to consume directly: the video when the plan addresses the real
// source (deferred organize executions, in-place link sources) instead of the
// staged copy, and every planned subtitle endpoint admitted as a sibling at
// preparation. Subtitle endpoints that were never admitted (created inside the
// prepare→publish window) are left to the existing plan semantics.
func (s *artifactStage) revalidateDirectSources(executor artifactPlanExecutor, plan *organizer.OrganizePlan) error {
	if filepath.Clean(plan.SourcePath) == filepath.Clean(s.sourcePath) {
		if err := s.revalidateAdmittedSource(s.sourcePath, s.sourceIdentity); err != nil {
			return err
		}
	}
	admitted := make(map[string]artifactSourceIdentity, len(s.siblings))
	for _, sibling := range s.siblings {
		admitted[filepath.Clean(sibling.sourcePath)] = sibling.identity
	}
	for _, mv := range executor.PlanSubtitleMoves(plan) {
		identity, ok := admitted[filepath.Clean(mv.OriginalPath)]
		if !ok {
			continue
		}
		if err := s.revalidateAdmittedSource(mv.OriginalPath, identity); err != nil {
			return err
		}
	}
	return nil
}
