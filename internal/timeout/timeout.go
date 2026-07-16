package timeout

import (
	"fmt"
	"time"
)

// Source identifies where a resolved timeout value originated (config, fallback, or explicit caller).
type Source string

const (
	sourcePrefixConfig   = "config:"
	sourcePrefixFallback = "fallback:"
	sourcePrefixExplicit = "explicit:"
)

// Timeout carries a resolved duration alongside its provenance source tag.
type Timeout struct {
	Duration time.Duration
	Source   Source
}

// FromConfig resolves a Timeout from a config field name and its seconds value.
// When seconds is positive, the source identifies the config field. When zero or
// negative, the fallback duration is used and the source identifies it as a fallback.
func FromConfig(name string, seconds int, fallback time.Duration) Timeout {
	if seconds > 0 {
		return Timeout{
			Duration: time.Duration(seconds) * time.Second,
			Source:   Source(sourcePrefixConfig + name),
		}
	}
	return Timeout{
		Duration: fallback,
		Source:   Source(fmt.Sprintf("%sdefault-%s", sourcePrefixFallback, fallback)),
	}
}

// FromDuration constructs a Timeout from a pre-resolved duration and an explicit source tag.
func FromDuration(d time.Duration, source Source) Timeout {
	return Timeout{Duration: d, Source: source}
}

// String returns a human-readable representation for logging.
func (t Timeout) String() string {
	return fmt.Sprintf("%s (%s)", t.Duration, t.Source)
}
