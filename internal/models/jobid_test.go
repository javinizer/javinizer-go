package models

import (
	"strings"
	"testing"
)

func TestParseJobID(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		// Valid inputs
		{"abc-123", false},
		{"550e8400-e29b-41d4-a716-446655440000", false},
		{"scrape", false},
		{"a", false},

		// Empty
		{"", true},

		// Path traversal
		{".", true},
		{"..", true},

		// Path separators
		{"/", true},
		{"\\", true},
		{"foo/bar", true},
		{"foo\\bar", true},
		{"/etc/passwd", true},
		{"C:\\Windows\\System32", true},

		// Mixed
		{"a/b", true},
		{"a\\b", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			id, err := ParseJobID(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseJobID(%q) expected error, got nil", tt.input)
				}
			} else {
				if err != nil {
					t.Errorf("ParseJobID(%q) unexpected error: %v", tt.input, err)
				}
				if string(id) != tt.input {
					t.Errorf("ParseJobID(%q) = %q, want %q", tt.input, string(id), tt.input)
				}
			}
		})
	}
}

func TestMustJobID(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		id := MustJobID("abc-123")
		if string(id) != "abc-123" {
			t.Errorf("MustJobID = %q, want %q", string(id), "abc-123")
		}
	})

	t.Run("panics on invalid", func(t *testing.T) {
		defer func() {
			r := recover()
			if r == nil {
				t.Error("MustJobID expected panic for empty string")
			}
		}()
		MustJobID("")
	})
}

func TestNewJobID(t *testing.T) {
	id := NewJobID()
	if id.IsZero() {
		t.Error("NewJobID() returned zero value")
	}
	if !id.Valid() {
		t.Errorf("NewJobID() = %q, not valid", id)
	}
}

func TestJobIDIsZero(t *testing.T) {
	var zero JobID
	if !zero.IsZero() {
		t.Error("zero JobID should report IsZero()")
	}

	id := MustJobID("test")
	if id.IsZero() {
		t.Error("non-zero JobID should not report IsZero()")
	}
}

func TestSentinelJobID(t *testing.T) {
	id := SentinelJobID()
	if string(id) != "scrape" {
		t.Errorf("SentinelJobID() = %q, want %q", string(id), "scrape")
	}
	if !id.Valid() {
		t.Error("SentinelJobID() should be valid")
	}
}

func TestContainsPathSeparator(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"abc", false},
		{"", false},
		{"a/b", true},
		{"a\\b", true},
		{"/", true},
		{"\\", true},
		{strings.Repeat("a", 100), false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := containsPathSeparator(tt.input)
			if got != tt.want {
				t.Errorf("containsPathSeparator(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
