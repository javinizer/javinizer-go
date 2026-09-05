package organizer

// #224 phase D: the organizer's destination locks ARE fsutil.SharedDestLocks —
// one process-wide registry shared with the downloader's install paths and the
// history reverter. These tests pin the shared keyspace directly: a direct
// registry acquisition must contend with (or admit) the organizer wrappers
// exactly as the two-tier reader/writer discipline dictates.

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/fsutil"
)

func TestDestLocks_FileLockSharesProcessRegistry(t *testing.T) {
	release := fsutil.SharedDestLocks().Acquire("/phase-d-unify/file.mp4")
	held := true
	defer func() {
		if held {
			release()
		}
	}()

	blocked := make(chan struct{})
	go func() {
		_ = withDestFileLock("/phase-d-unify/file.mp4", func() error { return nil })
		close(blocked)
	}()
	select {
	case <-blocked:
		t.Fatal("organizer file lock bypassed the shared destination registry")
	case <-time.After(100 * time.Millisecond):
	}
	release()
	held = false
	select {
	case <-blocked:
	case <-time.After(2 * time.Second):
		t.Fatal("organizer file lock never acquired after registry release")
	}
}

// Folded keys: a direct acquisition of one CASE SPELLING of a destination
// contends with the organizer's file lock on another spelling of it, even on a
// case-sensitive host volume (registry folding is unconditional).
func TestDestLocks_FileLockContendsAcrossCaseSpellings(t *testing.T) {
	release := fsutil.SharedDestLocks().Acquire("/PHASE-D-CASE/Movie.MKV")
	blocked := make(chan struct{})
	go func() {
		_ = withDestFileLock("/phase-d-case/movie.mkv", func() error { return nil })
		close(blocked)
	}()
	select {
	case <-blocked:
		t.Fatal("case spelling variant bypassed the shared destination lock")
	case <-time.After(100 * time.Millisecond):
	}
	release()
	select {
	case <-blocked:
	case <-time.After(2 * time.Second):
		t.Fatal("organizer file lock never acquired after case-variant release")
	}
}

func TestDestLocks_DirLocksShareProcessRegistry(t *testing.T) {
	// An exclusive registry hold on the directory tier excludes BOTH organizer dir modes.
	release := fsutil.SharedDestLocks().AcquireDirExclusive("/phase-d-unify/dir")
	exBlocked := make(chan struct{})
	go func() {
		_ = withDestDirExclusiveLock("/phase-d-unify/dir", func() error { return nil })
		close(exBlocked)
	}()
	select {
	case <-exBlocked:
		t.Fatal("exclusive dir lock entered while the registry key was held exclusively")
	case <-time.After(100 * time.Millisecond):
	}
	shBlocked := make(chan struct{})
	go func() {
		_ = withDestDirSharedLock("/phase-d-unify/dir", func() error { return nil })
		close(shBlocked)
	}()
	select {
	case <-shBlocked:
		t.Fatal("shared dir lock entered while the registry key was held exclusively")
	case <-time.After(100 * time.Millisecond):
	}
	release()
	select {
	case <-exBlocked:
	case <-time.After(2 * time.Second):
		t.Fatal("exclusive dir lock never acquired after registry release")
	}
	select {
	case <-shBlocked:
	case <-time.After(2 * time.Second):
		t.Fatal("shared dir lock never acquired after registry release")
	}

	// A shared registry hold ADMITS organizer shared dir locks and excludes
	// exclusive ones; a child-FILE lock (distinct descendant key) always proceeds.
	shRelease := fsutil.SharedDestLocks().AcquireDirShared("/phase-d-unify/dir2")
	sharedEntered := make(chan struct{})
	go func() {
		_ = withDestDirSharedLock("/phase-d-unify/dir2", func() error { return nil })
		close(sharedEntered)
	}()
	select {
	case <-sharedEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("shared dir hold must admit concurrent shared dir locks")
	}
	childEntered := make(chan struct{})
	go func() {
		_ = withDestFileLock("/phase-d-unify/dir2/file.mp4", func() error { return nil })
		close(childEntered)
	}()
	select {
	case <-childEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("descendant file lock must never contend with the ancestor dir hold (distinct keys)")
	}
	exBlocked2 := make(chan struct{})
	go func() {
		_ = withDestDirExclusiveLock("/phase-d-unify/dir2", func() error { return nil })
		close(exBlocked2)
	}()
	select {
	case <-exBlocked2:
		t.Fatal("exclusive dir lock entered while a shared hold was live")
	case <-time.After(100 * time.Millisecond):
	}
	shRelease()
	select {
	case <-exBlocked2:
	case <-time.After(2 * time.Second):
		t.Fatal("exclusive dir lock never acquired after shared release")
	}
}

// The organizer's universal holding pattern — ancestor directory first (shared
// for child writes, exclusive for in-place renames), then the descendant file —
// must never deadlock under mixed concurrency on one keyspace.
func TestDestLocks_NestedDirThenFile_NoDeadlock(t *testing.T) {
	const dir = "/phase-d-nest"
	file := filepath.Join(dir, "movie.mkv")
	start := make(chan struct{})
	done := make(chan struct{}, 16)
	for i := 0; i < 8; i++ {
		go func() {
			<-start
			_ = withDestDirSharedLock(dir, func() error {
				return withDestFileLock(file, func() error { return nil })
			})
			done <- struct{}{}
		}()
		go func() {
			<-start
			_ = withDestDirExclusiveLock(dir, func() error {
				return withDestFileLock(file, func() error { return nil })
			})
			done <- struct{}{}
		}()
	}
	close(start)
	for i := 0; i < 16; i++ {
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("nested dir-then-file acquisitions deadlocked")
		}
	}
}

// Degenerate plans exist (an execute-time fixture or a target file rendering to
// ""): TargetPath can clean to the very string TargetDir holds. The directory
// tier's key namespace must keep that nesting deadlock-free — pre-unification
// this shape survived only because file and dir locks lived in separate maps.
func TestDestLocks_DegenerateDirEqualsFile_NoDeadlock(t *testing.T) {
	done := make(chan struct{})
	go func() {
		_ = withDestDirExclusiveLock("/phase-d-degenerate", func() error {
			return withDestFileLock("/phase-d-degenerate/", func() error { return nil })
		})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("exclusive dir hold + same-named file hold self-deadlocked (tiers must not alias)")
	}
	done = make(chan struct{})
	go func() {
		_ = withDestDirSharedLock("/phase-d-degenerate2", func() error {
			return withDestFileLock("/phase-d-degenerate2", func() error { return nil })
		})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("shared dir hold + same-named file hold self-deadlocked (tiers must not alias)")
	}
}
