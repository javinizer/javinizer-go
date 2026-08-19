package fsutil

import (
	"bytes"
	cryptorand "crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/spf13/afero"
)

const (
	// ReplacementBusySuffix is adjacent to a destination so every process
	// arbitrating that destination observes the same ownership marker.
	ReplacementBusySuffix         = ".dlbusy"
	replacementBusyStaleAge       = 2 * time.Minute
	replacementBusyQuarantineMark = ".quarantine-"
)

// ErrReplacementBusy means another live process owns a destination replacement.
var ErrReplacementBusy = errors.New("replacement destination is busy")

// replacementBusyBootAt prevents a PID reused after this process started from
// being mistaken for an owner from this boot. The timestamp in the marker is
// written before the writer renames the destination aside.
var replacementBusyBootAt = time.Now()
var replacementIsWindows = runtime.GOOS == "windows"

// replacementPIDLiveness is richer than a bool: failure to inspect a PID can
// mean that the owner is gone, still alive, or simply undecidable from this
// process's permissions and platform probes.
type replacementPIDLiveness uint8

const (
	replacementPIDDead replacementPIDLiveness = iota
	replacementPIDAlive
	replacementPIDUnprobeable
)

var replacementProbePIDAliveAware = replacementProbePIDAliveAwarePlatform
var replacementProcessStartTime = replacementProcessStartTimePlatform

// replacementStartTimeFromUnixNano validates a platform-provided owner start
// time. A non-positive stamp (Windows handed back a zero/1601 FILETIME, or a
// platform reported the Unix epoch itself) cannot describe a real marker
// owner, so it yields nil and classification keeps the liveness-only fallback
// rather than comparing garbage against the marker timestamp.
//
//nolint:unused // wired in the windows-tagged seam (replacement_busy_pid_windows.go); host-GOOS lint cannot see the cross-platform use.
func replacementStartTimeFromUnixNano(nsec int64) *time.Time {
	if nsec <= 0 {
		return nil
	}
	start := time.Unix(0, nsec)
	return &start
}

var replacementBusyRandom = replacementBusyRandomPlatform
var replacementCryptoRandomRead = cryptorand.Read

// ReplacementBusyPath returns the durable in-flight marker for dest.
func ReplacementBusyPath(dest string) string { return dest + ReplacementBusySuffix }

// AcquireReplacementBusy atomically claims the destination-adjacent marker.
// Writers create it before moving the destination aside; sweepers create it
// before touching a backup. A marker from a dead PID is reclaimed, as is a
// well-formed marker carrying released=1 (the in-band release pre-wave-38
// wedged releases recorded so a wedged removal could not busy-block the
// destination for that process's lifetime), while a malformed marker is
// never reclaimed based on its age alone so an unowned file cannot be
// mistaken for Javinizer's marker.
func AcquireReplacementBusy(fs afero.Fs, dest string) (func(), error) {
	path := ReplacementBusyPath(dest)
	for {
		token := replacementBusyToken()
		file, err := fs.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			if _, err = file.WriteString(token); err != nil {
				_ = file.Close()
				_ = fs.Remove(path)
				return nil, fmt.Errorf("write replacement busy marker: %w", err)
			}
			if err = file.Sync(); err != nil {
				_ = file.Close()
				_ = fs.Remove(path)
				return nil, fmt.Errorf("sync replacement busy marker: %w", err)
			}
			if err = file.Close(); err != nil {
				_ = fs.Remove(path)
				return nil, fmt.Errorf("close replacement busy marker: %w", err)
			}
			var once sync.Once
			return func() {
				once.Do(func() { releaseReplacementBusy(fs, path, token) })
			}, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("create replacement busy marker: %w", err)
		}

		inspection, inspectErr := replacementBusyInspect(fs, path)
		if inspectErr != nil {
			return nil, fmt.Errorf("inspect replacement busy marker: %w", inspectErr)
		}
		if !inspection.stale {
			return nil, ErrReplacementBusy
		}
		if !inspection.reclaimable {
			logging.Warnf("replacement busy marker %s is stale but not a recognized Javinizer marker; preserving it", path)
			return nil, ErrReplacementBusy
		}
		if !inspection.hasObservedToken {
			// The marker disappeared while it was being inspected. Refresh from
			// the create attempt instead of renaming without bytes to validate.
			continue
		}

		// Do not remove path based on the earlier inspection. Another claimant
		// may have won the same stale-marker decision in the meantime. Rename
		// is the portable afero ownership claim: only the claimant whose source
		// rename succeeds may inspect and dispose of its uniquely named
		// successor.
		takeoverPath, nameErr := replacementBusyTakeoverPath(path)
		if nameErr != nil {
			return nil, fmt.Errorf("name replacement busy takeover marker: %w", nameErr)
		}
		if renameErr := fs.Rename(path, takeoverPath); renameErr != nil {
			if !os.IsNotExist(renameErr) {
				return nil, fmt.Errorf("claim replacement busy marker: %w", renameErr)
			}

			// A failed source rename means another claimant won. Re-read the
			// marker from scratch; never apply the stale result above to the
			// winner's replacement marker.
			refreshed, refreshErr := replacementBusyInspect(fs, path)
			if refreshErr != nil {
				return nil, fmt.Errorf("reinspect replacement busy marker: %w", refreshErr)
			}
			if !refreshed.stale {
				return nil, ErrReplacementBusy
			}
			if !refreshed.reclaimable {
				logging.Warnf("replacement busy marker %s is stale but not a recognized Javinizer marker; preserving it", path)
				return nil, ErrReplacementBusy
			}
			continue
		}

		claimedToken, readErr := afero.ReadFile(fs, takeoverPath)
		if readErr != nil {
			// We own the successor, but cannot prove which marker it contains.
			// Leave it in place and fail closed rather than consuming it.
			return nil, fmt.Errorf("read replacement busy takeover marker: %w", readErr)
		}
		if !bytes.Equal(claimedToken, inspection.observedToken) {
			if returnErr := replacementBusyReturnTakeover(fs, path, takeoverPath, claimedToken); returnErr != nil {
				return nil, returnErr
			}
			return nil, ErrReplacementBusy
		}
		if removeErr := fs.Remove(takeoverPath); removeErr != nil {
			// The successful rename proves ownership of takeoverPath. If its
			// removal fails, stop rather than guessing about on-disk state.
			return nil, fmt.Errorf("reclaim replacement busy marker: %w", removeErr)
		}
	}
}

func replacementBusyToken() string {
	return fmt.Sprintf("pid=%d,time=%d", os.Getpid(), time.Now().UnixNano())
}

func replacementBusyTakeoverPath(path string) (string, error) {
	random, err := replacementBusyRandom()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s.takeover-%d-%x", path, os.Getpid(), random), nil
}

func replacementBusyQuarantinePath(path string) (string, error) {
	random, err := replacementBusyRandom()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%s%d-%x", path, replacementBusyQuarantineMark, os.Getpid(), random), nil
}

// replacementBusyReturnTakeover puts back the bytes found after a successful
// claimant rename. The exclusive placeholder serializes the restore: other
// claimants can observe it, but cannot acquire or replace it while the owned
// successor is renamed back over it. If the destination is already occupied,
// preserve the bytes in a unique quarantine sibling instead of overwriting
// that live marker.
func replacementBusyReturnTakeover(fs afero.Fs, path, takeoverPath string, content []byte) error {
	placeholder, err := fs.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err == nil {
		if closeErr := placeholder.Close(); closeErr != nil {
			// codex PR#215 w17: returning here with the placeholder still at
			// path strands a zero-byte (tokenless) marker — and malformed
			// markers are deliberately NEVER reclaimed, so the destination
			// would busy-block forever. Recover before surfacing the close
			// error, in the order that can never strand BOTH the displaced
			// bytes and an unreclaimable marker:
			//
			//  1. RESTORE the takeover bytes back onto path first (the wave-12
			//     ReplaceFile leg renames over the placeholder we still own).
			//     Doing this FIRST keeps the placeholder's serialization
			//     property through the restore: a remove-first order would open
			//     a window in which a foreign claimant creates its own live
			//     marker and then has it clobbered by our trailing restore.
			//  2. Only if the restore itself fails, REMOVE the placeholder: an
			//     ABSENT marker self-heals (the next claimant re-creates it),
			//     while an unreclaimable 0-byte one does not. The takeover
			//     bytes then stay in their uniquely-named sibling — inspectable
			//     garbage, never a busy-block.
			//
			// The original close error still surfaces either way.
			if restoreErr := ReplaceFile(fs, takeoverPath, path); restoreErr != nil {
				logging.Warnf("replacement busy takeover restore after placeholder close failure failed for %s: %v; removing the placeholder (claimed bytes retained at %s)", path, restoreErr, takeoverPath)
				if removeErr := fs.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
					logging.Warnf("replacement busy restore placeholder %s could not be removed after close failure: %v; the destination may busy-block until the marker is removed manually", path, removeErr)
				}
			}
			return fmt.Errorf("close replacement busy restore placeholder: %w", closeErr)
		}
		// The rename target is the 0-byte placeholder just claimed above, so
		// this leg renames onto an EXISTING path: route through the
		// platform-aware replacement primitive. Windows OsFs rename
		// (MoveFileW) refuses an existing destination, which would strand a
		// malformed 0-byte .dlbusy marker no claimant can reclaim — a
		// permanent busy block on that destination (codex PR#215 w12).
		// ReplaceFile is MoveFileExW+MOVEFILE_REPLACE_EXISTING for OsFs and a
		// plain atomic rename on POSIX, where renaming over the placeholder
		// is already safe.
		if renameErr := ReplaceFile(fs, takeoverPath, path); renameErr != nil {
			return fmt.Errorf("restore replacement busy marker: %w", renameErr)
		}
		return nil
	}
	if !os.IsExist(err) {
		return fmt.Errorf("reserve replacement busy restore path: %w", err)
	}

	quarantinePath, nameErr := replacementBusyQuarantinePath(path)
	if nameErr != nil {
		return fmt.Errorf("name replacement busy quarantine marker: %w", nameErr)
	}
	quarantine, openErr := fs.OpenFile(quarantinePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if openErr != nil {
		return fmt.Errorf("create replacement busy quarantine marker: %w", openErr)
	}
	if written, writeErr := quarantine.Write(content); writeErr != nil {
		_ = quarantine.Close()
		return fmt.Errorf("write replacement busy quarantine marker: %w", writeErr)
	} else if written != len(content) {
		_ = quarantine.Close()
		return fmt.Errorf("write replacement busy quarantine marker: short write (%d/%d)", written, len(content))
	}
	if syncErr := quarantine.Sync(); syncErr != nil {
		_ = quarantine.Close()
		return fmt.Errorf("sync replacement busy quarantine marker: %w", syncErr)
	}
	if closeErr := quarantine.Close(); closeErr != nil {
		return fmt.Errorf("close replacement busy quarantine marker: %w", closeErr)
	}
	logging.Warnf("replacement busy marker %s was claimed by another process; preserved it in quarantine %s", path, quarantinePath)
	if removeErr := fs.Remove(takeoverPath); removeErr != nil {
		return fmt.Errorf("remove replacement busy takeover marker after quarantine: %w", removeErr)
	}
	return nil
}

func replacementBusyRandomPlatform() (uint64, error) {
	var raw [8]byte
	if _, err := replacementCryptoRandomRead(raw[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(raw[:]), nil
}

// replacementBusyReleaseBackoff is the brief delay between release unlink
// retries. A worthy unlink failure is typically a transient network-FS
// hiccup, so two short retries buy recovery time without stalling callers.
var replacementBusyReleaseBackoff = []time.Duration{10 * time.Millisecond, 25 * time.Millisecond}

// replacementBusyReleasedField is the in-band release field
// replacementBusyInspect decodes (keeping the pid/time fields so the token
// still parses as well-formed). Wave-38 (codex P2, PR#215 finding F4) moved
// the release path to the take-aside unlink (a wedged release frees the
// marker name BEFORE any unlink can wedge, so release no longer rewrites
// anything); the decode arm stays for markers left on disk by pre-wave-38
// wedged releases, which remain reclaimable through the takeover rules.

// releaseReplacementBusy removes the marker only when its bytes AND its
// identity still carry our token — and never by the marker PATHNAME
// (wave-38, codex P2, PR#215 finding F4): a directory writer swapping the
// marker between the token read and a pathname Remove would have release
// delete the REPLACEMENT marker, letting a third process acquire the name
// while the replacement marker's real owner stays active. The release runs
// the generalized no-replace take-aside instead:
//
//  1. OBSERVE the marker through one open handle (replacementBusyReleaseObserve):
//     the identity snapshot (dev/inode where exposed, size, mtime) rides the
//     same descriptor as THE bytes read, so the recorded token can never be
//     aliased to a successor object;
//  2. TAKE the observed marker aside onto a fresh O_EXCL-reserved sibling
//     scratch (a take can never displace foreign bytes: the scratch is our
//     own claimed placeholder) and re-prove the moved object at the scratch
//     name against the observed identity — a mid-take swap restores the
//     moved object back NO-REPLACE where the name is still free (carry-on,
//     never a deletion of what it cannot prove);
//  3. UNLINK only the scratch, re-bound to the observed identity at every
//     unlink attempt. A transiently failing take-aside unlink is retried
//     with replacementBusyReleaseBackoff; a persistent wedge leaves only the
//     inert scratch sibling (the marker name is already free, so the release
//     is achieved and no later claimant is ever busy-blocked) and is
//     surfaced through a warn for manual cleanup.
func releaseReplacementBusy(fs afero.Fs, path, token string) {
	content, observed, ok := replacementBusyReleaseObserve(fs, path)
	if !ok || content != token {
		return
	}
	scratch, claim, cerr := replacementBusyClaimReleaseScratch(fs, path)
	if cerr != nil {
		logging.Warnf("replacement busy marker %s could not reserve a release take-aside name (%v); the marker stays as-is — later claims arbitrate it through the normal stale rules", path, cerr)
		return
	}
	hold, terr := TakeAside(TakeAsideSpec{
		FS:      fs,
		Src:     path,
		Scratch: scratch,
		Claim:   claim,
		Prove: func(moved os.FileInfo) error {
			if !asideSameObject(moved, observed) {
				return fmt.Errorf("marker taken aside from %s is not the observed token object — foreign marker preserved: %w", path, ErrTakeAsideForeign)
			}
			return nil
		},
	})
	if terr != nil {
		logging.Warnf("replacement busy marker %s could not be taken aside for release (%v); nothing was removed by name — later claims arbitrate the marker normally", path, terr)
		return
	}
	var removeErr error
	for attempt := 0; ; attempt++ {
		removeErr = hold.Unlink()
		if removeErr == nil || errors.Is(removeErr, ErrTakeAsideForeign) {
			// nil: taken-aside object removed (or vanished by itself).
			// Foreign: a swap raced the unlink window — the refusal preserved
			// it and the marker name is already free; retrying cannot help.
			break
		}
		if attempt >= len(replacementBusyReleaseBackoff) {
			break
		}
		time.Sleep(replacementBusyReleaseBackoff[attempt])
	}
	if removeErr != nil {
		logging.Warnf("replacement busy marker %s release: the take-aside unlink of %s failed (%v); the marker name is free, the inert scratch awaits manual cleanup", path, scratch, removeErr)
	}
}

// replacementBusyReleaseObserve reads the marker and captures ITS identity
// through one open handle (wave-38, finding F4): Stat and Read ride the SAME
// descriptor, so the recorded token and the dev/inode snapshot provably
// belong to ONE observed object — a pathname swap between a path-based read
// and a separate Lstat could never alias the record. Close errors are
// ignored: a read-only close mutates nothing and the identity is already
// bound. Any observation failure answers not-ok (best-effort release posture).
func replacementBusyReleaseObserve(fs afero.Fs, path string) (string, os.FileInfo, bool) {
	handle, err := fs.Open(path)
	if err != nil {
		return "", nil, false
	}
	info, serr := handle.Stat()
	content := []byte(nil)
	var rerr error
	if serr == nil {
		content, rerr = io.ReadAll(handle)
	}
	_ = handle.Close()
	if serr != nil || rerr != nil {
		return "", nil, false
	}
	return string(content), info, true
}

// replacementBusyReleaseClaimTries bounds the release-take-aside scratch
// claim loop; every collision or racing claimant costs one draw.
const replacementBusyReleaseClaimTries = 16

// replacementBusyClaimReleaseScratch atomically reserves a uniquely named
// sibling scratch for the release take-aside (wave-38, finding F4): mint via
// the takeover-name grammar (PID + crypto random), claim O_CREATE|O_EXCL,
// capture the reservation's own identity through the open handle's pre-close
// Stat (mirroring the quarantine-claim discipline) so the take's pre-move
// re-proof can refuse a foreign reservation swap. An O_EXCL collision
// re-draws; a reservation whose identity cannot be read (or whose close
// fails) is dropped rather than renaming over unverified bytes.
func replacementBusyClaimReleaseScratch(fs afero.Fs, path string) (string, os.FileInfo, error) {
	for attempt := 0; attempt < replacementBusyReleaseClaimTries; attempt++ {
		candidate, err := replacementBusyTakeoverPath(path)
		if err != nil {
			return "", nil, fmt.Errorf("name release take-aside scratch for %s: %w", path, err)
		}
		reservation, rerr := fs.OpenFile(candidate, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		switch {
		case rerr == nil:
			info, serr := reservation.Stat()
			if serr != nil {
				_ = reservation.Close()
				_ = fs.Remove(candidate)
				return "", nil, fmt.Errorf("stat release take-aside reservation %s: %w", candidate, serr)
			}
			if cerr := reservation.Close(); cerr != nil {
				_ = fs.Remove(candidate)
				return "", nil, fmt.Errorf("close release take-aside reservation %s: %w", candidate, cerr)
			}
			return candidate, info, nil
		case os.IsExist(rerr):
			continue // a racer claimed this draw first — draw again
		default:
			return "", nil, fmt.Errorf("reserve release take-aside scratch %s: %w", candidate, rerr)
		}
	}
	return "", nil, fmt.Errorf("release take-aside names exhausted for %s after %d attempts", path, replacementBusyReleaseClaimTries)
}

// replacementBusyIsReleased decodes the in-band release field with the same
// field discipline as parseReplacementBusyToken so a hand-crafted lookalike
// (missing pid/time) still classifies as malformed, never as released.
func replacementBusyIsReleased(content string) bool {
	parts := strings.FieldsFunc(content, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	})
	for _, part := range parts {
		keyValue := strings.SplitN(part, "=", 2)
		if len(keyValue) == 2 && strings.TrimSpace(keyValue[0]) == "released" && strings.TrimSpace(keyValue[1]) == "1" {
			return true
		}
	}
	return false
}

type replacementBusyInspection struct {
	stale            bool
	reclaimable      bool
	observedToken    []byte
	hasObservedToken bool
}

// replacementBusyInspect separates age/liveness/start-time classification from
// ownership. An old malformed marker may be stale for arbitration purposes,
// but it is not safe to remove because its name and mtime do not prove
// Javinizer created it.
//
// Classification precedence (the first applicable arm wins):
//  1. Malformed content is retained and is never reclaimable by age alone.
//  2. A well-formed marker carrying the released field (the owner's final
//     unlink failed transiently and it recorded the release in-band via
//     releaseReplacementBusy) is stale and reclaimable regardless of PID
//     liveness or age, ending the process-lifetime block a wedged removal
//     would otherwise create. The reclaim still goes through the wave
//     takeover/quarantine rules; the released bytes are preserved or
//     returned, never silently overwritten. This arm deliberately precedes
//     the live-PID arms because a marker may carry this very process's PID.
//  3. A well-formed marker whose PID probe proves alive is never expired by
//     age.
//  4. Within that live arm, W20's start-time proof that the PID started after
//     the marker marks it stale as PID reuse. Linux derives the start time
//     from /proc/<pid>/stat; K4 arms Windows with the same proof through
//     K32 GetProcessTimes. An unreadable start time keeps the liveness-only
//     behavior rather than inventing evidence.
//  5. A probe that proves the PID is dead marks the marker stale.
//  6. On POSIX, age is consulted only when the probe is
//     undecidable/unprobeable; Windows retains its conservative access-denied
//     behavior and does not expire an unprobeable owner by age.
func replacementBusyInspect(fs afero.Fs, path string) (replacementBusyInspection, error) {
	content, err := afero.ReadFile(fs, path)
	if err != nil {
		if os.IsNotExist(err) {
			return replacementBusyInspection{stale: true, reclaimable: true}, nil
		}
		return replacementBusyInspection{}, err
	}
	inspection := replacementBusyInspection{observedToken: content, hasObservedToken: true}
	info, err := fs.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return replacementBusyInspection{stale: true, reclaimable: true, observedToken: content, hasObservedToken: true}, nil
		}
		return replacementBusyInspection{}, err
	}
	pid, created, ok := parseReplacementBusyToken(string(content))
	if !ok {
		inspection.stale = time.Since(info.ModTime()) > replacementBusyStaleAge
		return inspection, nil
	}
	if replacementBusyIsReleased(string(content)) {
		// The owner declared the release in-band after its final unlink wedged.
		// This arm deliberately precedes the same-PID and live-probe arms: the
		// marker is well-formed and may carry this very process's PID, but the
		// recorded release supersedes both, exactly as a proven-dead PID would.
		inspection.stale = true
		inspection.reclaimable = true
		return inspection, nil
	}
	createdAt := time.Unix(0, created)
	if pid == os.Getpid() {
		// This timestamp is a PID-reuse boundary, not a two-minute lease:
		// markers from before this process started belong to a prior owner,
		// while current-run markers stay busy for as long as this process runs.
		inspection.stale = createdAt.Before(replacementBusyBootAt)
		inspection.reclaimable = true
		return inspection, nil
	}
	// First prove that the recorded owner is live. Linux then distinguishes a
	// reused PID by comparing /proc starttime with the marker's wall-clock
	// timestamp. A start time after the marker proves that this is a different
	// process and makes the marker stale. If start time cannot be established,
	// the positive liveness proof still wins over marker age.
	liveness := replacementProbePIDAliveAware(pid)
	switch liveness {
	case replacementPIDAlive:
		if replacementProcessStartTime != nil {
			if processStartTime := replacementProcessStartTime(pid); processStartTime != nil && processStartTime.After(createdAt) {
				inspection.stale = true
				inspection.reclaimable = true
				return inspection, nil
			}
		}
		inspection.reclaimable = true
		return inspection, nil
	case replacementPIDDead:
		inspection.stale = true
		inspection.reclaimable = true
		return inspection, nil
	case replacementPIDUnprobeable:
		if replacementIsWindows {
			// Access denial does not prove that a Windows owner is gone; retain
			// the marker rather than allowing an untrusted process to reclaim it.
			inspection.reclaimable = true
			return inspection, nil
		}
		inspection.stale = time.Since(createdAt) > replacementBusyStaleAge
		inspection.reclaimable = true
		return inspection, nil
	default:
		// An unknown result is not the explicit undecidable seam. Fail closed
		// rather than letting an unexpected value use the age fallback.
		inspection.reclaimable = true
		return inspection, nil
	}
}

func parseReplacementBusyToken(content string) (pid int, created int64, ok bool) {
	var pidSet, timeSet bool
	parts := strings.FieldsFunc(content, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	})
	for _, part := range parts {
		keyValue := strings.SplitN(part, "=", 2)
		if len(keyValue) != 2 {
			continue
		}
		switch strings.TrimSpace(keyValue[0]) {
		case "pid":
			value, err := strconv.Atoi(strings.TrimSpace(keyValue[1]))
			if err != nil {
				return 0, 0, false
			}
			pid, pidSet = value, true
		case "time":
			value, err := strconv.ParseInt(strings.TrimSpace(keyValue[1]), 10, 64)
			if err != nil {
				return 0, 0, false
			}
			created, timeSet = value, true
		}
	}
	return pid, created, pidSet && timeSet
}
