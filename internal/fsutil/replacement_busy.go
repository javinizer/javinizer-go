package fsutil

import (
	"bytes"
	cryptorand "crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
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
// before touching a backup. A marker from a dead PID is reclaimed, while a
// malformed marker is never reclaimed based on its age alone so an unowned
// file cannot be mistaken for Javinizer's marker.
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
// claimant rename. The exclusive placeholder makes the restore no-replace:
// other claimants can observe it, but cannot acquire or replace it while the
// owned successor is renamed back. If the destination is already occupied,
// preserve the bytes in a unique quarantine sibling instead of overwriting
// that live marker.
func replacementBusyReturnTakeover(fs afero.Fs, path, takeoverPath string, content []byte) error {
	placeholder, err := fs.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err == nil {
		if closeErr := placeholder.Close(); closeErr != nil {
			return fmt.Errorf("close replacement busy restore placeholder: %w", closeErr)
		}
		if renameErr := fs.Rename(takeoverPath, path); renameErr != nil {
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

func releaseReplacementBusy(fs afero.Fs, path, token string) {
	content, err := afero.ReadFile(fs, path)
	if err != nil || string(content) != token {
		return
	}
	_ = fs.Remove(path)
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
//  2. A well-formed marker whose PID probe proves alive is never expired by
//     age.
//  3. Within that live arm, W20's start-time proof that the PID started after
//     the marker marks it stale as PID reuse. Linux derives the start time
//     from /proc/<pid>/stat; K4 arms Windows with the same proof through
//     K32 GetProcessTimes. An unreadable start time keeps the liveness-only
//     behavior rather than inventing evidence.
//  4. A probe that proves the PID is dead marks the marker stale.
//  5. On POSIX, age is consulted only when the probe is
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
