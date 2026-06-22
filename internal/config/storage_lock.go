package config

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	configLockWaitInterval = 50 * time.Millisecond
	configLockTimeout      = 10 * time.Second
	configLockStaleAge     = 2 * time.Minute
)

func makeConfigLockToken() string {
	return fmt.Sprintf("pid=%d,time=%d", os.Getpid(), time.Now().UnixNano())
}

func releaseConfigFileLock(lockPath string, token string) {
	currentToken, err := os.ReadFile(lockPath)
	if err != nil {
		return
	}
	if string(currentToken) != token {
		return
	}
	_ = os.Remove(lockPath)
}

func parseConfigLockMetadata(content string) (pid int, createdUnixNano int64, ok bool) {
	pidSet := false
	timeSet := false

	parts := strings.FieldsFunc(content, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	})

	for _, part := range parts {
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		value := strings.TrimSpace(kv[1])
		switch key {
		case "pid":
			parsedPID, err := strconv.Atoi(value)
			if err != nil {
				return 0, 0, false
			}
			pid = parsedPID
			pidSet = true
		case "time":
			parsedTime, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return 0, 0, false
			}
			createdUnixNano = parsedTime
			timeSet = true
		}
	}

	return pid, createdUnixNano, pidSet && timeSet
}

func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// Signal(0) probes process existence without sending a signal.
	err = process.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	return err == syscall.EPERM
}

func shouldReapConfigLock(content []byte, modTime time.Time, now time.Time) bool {
	pid, createdUnixNano, ok := parseConfigLockMetadata(string(content))
	if !ok {
		// Fallback for corrupt/partial lock files (e.g., crash between create and write):
		// reclaim only when the lock file itself is stale by mtime.
		return now.Sub(modTime) > configLockStaleAge
	}

	createdAt := time.Unix(0, createdUnixNano)
	if now.Sub(createdAt) <= configLockStaleAge {
		return false
	}

	if runtime.GOOS == "windows" {
		// PID liveness probing via Signal(0) is unreliable on Windows.
		// Reclaim parseable stale locks by age, but avoid stealing our own lock.
		return pid != os.Getpid()
	}

	return !isProcessAlive(pid)
}

func acquireConfigFileLock(path string) (func(), error) {
	lockPath := path + ".lock"
	deadline := time.Now().Add(configLockTimeout)
	token := makeConfigLockToken()

	for {
		lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			if _, writeErr := lockFile.WriteString(token); writeErr != nil {
				_ = lockFile.Close()
				_ = os.Remove(lockPath)
				return nil, fmt.Errorf("failed to write config lock: %w", writeErr)
			}
			if syncErr := lockFile.Sync(); syncErr != nil {
				_ = lockFile.Close()
				_ = os.Remove(lockPath)
				return nil, fmt.Errorf("failed to sync config lock: %w", syncErr)
			}
			if closeErr := lockFile.Close(); closeErr != nil {
				_ = os.Remove(lockPath)
				return nil, fmt.Errorf("failed to close config lock: %w", closeErr)
			}

			var once sync.Once
			return func() {
				once.Do(func() {
					releaseConfigFileLock(lockPath, token)
				})
			}, nil
		}

		if !os.IsExist(err) {
			return nil, fmt.Errorf("failed to acquire config lock: %w", err)
		}

		lockContent, readErr := os.ReadFile(lockPath)
		if readErr == nil {
			now := time.Now()
			lockInfo, statErr := os.Stat(lockPath)
			if os.IsNotExist(statErr) {
				continue
			}
			if statErr == nil && shouldReapConfigLock(lockContent, lockInfo.ModTime(), now) {
				if removeErr := os.Remove(lockPath); removeErr == nil || os.IsNotExist(removeErr) {
					continue
				}
			}
		} else if os.IsNotExist(readErr) {
			continue
		}

		if _, statErr := os.Stat(lockPath); os.IsNotExist(statErr) {
			continue
		}

		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for config lock: %s", lockPath)
		}

		time.Sleep(configLockWaitInterval)
	}
}
