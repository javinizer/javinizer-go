package fsutil

import (
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

// replacementPIDLiveness is deliberately richer than a bool on Windows:
// failure to open a process can mean either that the PID is gone or that the
// caller lacks permission to inspect a still-live owner.
type replacementPIDLiveness uint8

const (
	replacementPIDDead replacementPIDLiveness = iota
	replacementPIDAlive
	replacementPIDUnprobeable
)

var replacementProbePIDAliveAware = replacementProbePIDAliveAwarePlatform

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
		if removeErr := fs.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
			return nil, fmt.Errorf("reclaim replacement busy marker: %w", removeErr)
		}
	}
}

func replacementBusyToken() string {
	return fmt.Sprintf("pid=%d,time=%d", os.Getpid(), time.Now().UnixNano())
}

func releaseReplacementBusy(fs afero.Fs, path, token string) {
	content, err := afero.ReadFile(fs, path)
	if err != nil || string(content) != token {
		return
	}
	_ = fs.Remove(path)
}

// replacementBusyState separates age/liveness from ownership. An old
// malformed marker may be stale for arbitration purposes, but it is not safe
// to remove because its name and mtime do not prove Javinizer created it.
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
	// A well-formed foreign marker is decided by owner liveness, never by its
	// age. In particular, Windows must not expire a live critical section.
	liveness := replacementProbePIDAliveAware(pid)
	if liveness == replacementPIDAlive {
		return false, true, nil
	}
	if replacementIsWindows && liveness == replacementPIDUnprobeable {
		// Windows access failures do not prove that the owner is dead. Retain
		// the marker rather than allowing an untrusted process to reclaim it.
		return false, true, nil
	}
	return liveness == replacementPIDDead, true, nil
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
