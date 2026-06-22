package models

import (
	"fmt"

	"github.com/google/uuid"
)

// JobID is a domain type for batch job identifiers. It wraps a string UUID
// and provides validation at construction time, ensuring that only well-formed
// job IDs circulate through the system. This prevents path traversal attacks
// (jobID is used in filepath.Join) and eliminates the need for ad-hoc
// validation scattered across packages.
//
// Use NewJobID() to generate a fresh UUID-based JobID, or ParseJobID() to
// validate and wrap an existing string (e.g. from c.Param("id")).
type JobID string

// NewJobID generates a new JobID backed by a UUID.
func NewJobID() JobID {
	return JobID(uuid.New().String())
}

// ParseJobID validates and wraps an existing string as a JobID.
// Returns an error if the string is empty, ".", "..", or contains path separators.
func ParseJobID(s string) (JobID, error) {
	if s == "" || s == "." || s == ".." {
		return "", fmt.Errorf("invalid job ID: %q", s)
	}
	if containsPathSeparator(s) {
		return "", fmt.Errorf("invalid job ID: %q: must not contain path separators", s)
	}
	return JobID(s), nil
}

// MustJobID wraps an existing string as a JobID, panicking on invalid input.
// For use in tests and construction paths where the input is known-good.
func MustJobID(s string) JobID {
	id, err := ParseJobID(s)
	if err != nil {
		panic(err)
	}
	return id
}

// String returns the underlying string value.
func (id JobID) String() string { return string(id) }

// IsZero reports whether the JobID is the zero value (empty string).
func (id JobID) IsZero() bool { return id == "" }

// Valid reports whether the JobID is non-zero and well-formed.
func (id JobID) Valid() bool {
	_, err := ParseJobID(string(id))
	return err == nil
}

// SentinelJobID returns a JobID suitable for non-batch scrape operations
// (single-movie API scrape/rescrape). These operations don't belong to a
// batch job, but the poster manager requires a jobID for directory naming.
// The sentinel "scrape" is safe (no path separators) and is only used for
// poster storage, not for DB persistence.
func SentinelJobID() JobID {
	return JobID("scrape")
}

func containsPathSeparator(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '/' || s[i] == '\\' {
			return true
		}
	}
	return false
}
