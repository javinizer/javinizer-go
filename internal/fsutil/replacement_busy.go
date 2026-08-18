package fsutil

import (
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
	ReplacementBusySuffix   = ".dlbusy"
	replacementBusyStaleAge = 2 * time.Minute
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

		stale, reclaimable, inspectErr := replacementBusyState(fs, path)
		if inspectErr != nil {
			return nil, fmt.Errorf("inspect replacement busy marker: %w", inspectErr)
		}
		if !stale {
			return nil, ErrReplacementBusy
		}
		if !reclaimable {
			logging.Warnf("replacement busy marker %s is stale but not a recognized Javinizer marker; preserving it", path)
			return nil, ErrReplacementBusy
		}

		// Do not remove path based on the earlier inspection. Another claimant
		// may have won the same stale-marker decision in the meantime. Rename
		// is the portable afero ownership claim: only the claimant whose source
		// rename succeeds may remove its uniquely named successor and recreate
		// the marker.
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
			stale, reclaimable, inspectErr = replacementBusyState(fs, path)
			if inspectErr != nil {
				return nil, fmt.Errorf("reinspect replacement busy marker: %w", inspectErr)
			}
			if !stale {
				return nil, ErrReplacementBusy
			}
			if !reclaimable {
				logging.Warnf("replacement busy marker %s is stale but not a recognized Javinizer marker; preserving it", path)
				return nil, ErrReplacementBusy
			}
			continue
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

// replacementBusyState separates age/liveness/start-time classification from
// ownership. An old malformed marker may be stale for arbitration purposes,
// but it is not safe to remove because its name and mtime do not prove
// Javinizer created it.
//
// Classification precedence (the first applicable arm wins):
//  1. Malformed content is retained and is never reclaimable by age alone.
//  2. A well-formed marker whose PID probe proves alive is never expired by
//     age.
//  3. Within that live arm, W20's start-time proof that the PID started after
//     the marker marks it stale as PID reuse.
//  4. A probe that proves the PID is dead marks the marker stale.
//  5. On POSIX, age is consulted only when the probe is
//     undecidable/unprobeable; Windows retains its conservative access-denied
//     behavior and does not expire an unprobeable owner by age.
func replacementBusyState(fs afero.Fs, path string) (stale, reclaimable bool, err error) {
	content, err := afero.ReadFile(fs, path)
	if err != nil {
		if os.IsNotExist(err) {
			return true, true, nil
		}
		return false, false, err
	}
	info, err := fs.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return true, true, nil
		}
		return false, false, err
	}
	pid, created, ok := parseReplacementBusyToken(string(content))
	if !ok {
		return time.Since(info.ModTime()) > replacementBusyStaleAge, false, nil
	}
	createdAt := time.Unix(0, created)
	if pid == os.Getpid() {
		// This timestamp is a PID-reuse boundary, not a two-minute lease:
		// markers from before this process started belong to a prior owner,
		// while current-run markers stay busy for as long as this process runs.
		return createdAt.Before(replacementBusyBootAt), true, nil
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
				return true, true, nil
			}
		}
		return false, true, nil
	case replacementPIDDead:
		return true, true, nil
	case replacementPIDUnprobeable:
		if replacementIsWindows {
			// Access denial does not prove that a Windows owner is gone; retain
			// the marker rather than allowing an untrusted process to reclaim it.
			return false, true, nil
		}
		return time.Since(createdAt) > replacementBusyStaleAge, true, nil
	default:
		// An unknown result is not the explicit undecidable seam. Fail closed
		// rather than letting an unexpected value use the age fallback.
		return false, true, nil
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
