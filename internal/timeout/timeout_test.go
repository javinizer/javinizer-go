package timeout

import (
	"testing"
	"time"
)

func TestFromConfig_PositiveValue(t *testing.T) {
	tt := FromConfig("metadata.translation.timeout_seconds", 300, 60*time.Second)
	if tt.Duration != 300*time.Second {
		t.Fatalf("Duration = %v, want 300s", tt.Duration)
	}
	if tt.Source != "config:metadata.translation.timeout_seconds" {
		t.Fatalf("Source = %q, want config:metadata.translation.timeout_seconds", tt.Source)
	}
}

func TestFromConfig_ZeroFallsBack(t *testing.T) {
	tt := FromConfig("metadata.translation.timeout_seconds", 0, 60*time.Second)
	if tt.Duration != 60*time.Second {
		t.Fatalf("Duration = %v, want 60s fallback", tt.Duration)
	}
	if tt.Source != "fallback:default-1m0s" {
		t.Fatalf("Source = %q, want fallback:default-1m0s", tt.Source)
	}
}

func TestFromConfig_NegativeFallsBack(t *testing.T) {
	tt := FromConfig("scrapers.timeout_seconds", -1, 30*time.Second)
	if tt.Duration != 30*time.Second {
		t.Fatalf("Duration = %v, want 30s fallback", tt.Duration)
	}
	if tt.Source != "fallback:default-30s" {
		t.Fatalf("Source = %q, want fallback:default-30s", tt.Source)
	}
}

func TestFromDuration(t *testing.T) {
	src := Source("explicit:caller")
	tt := FromDuration(2*time.Second, src)
	if tt.Duration != 2*time.Second {
		t.Fatalf("Duration = %v, want 2s", tt.Duration)
	}
	if tt.Source != src {
		t.Fatalf("Source = %q, want %q", tt.Source, src)
	}
}

func TestTimeout_String(t *testing.T) {
	tt := FromConfig("scrapers.request_timeout_seconds", 120, 60*time.Second)
	got := tt.String()
	if got != "2m0s (config:scrapers.request_timeout_seconds)" {
		t.Fatalf("String() = %q", got)
	}
}
