package version

import (
	"strings"
	"testing"
)

func TestInfo(t *testing.T) {
	// Save original values
	origVersion := Version
	origCommit := Commit
	origBuildDate := BuildDate
	defer func() {
		Version = origVersion
		Commit = origCommit
		BuildDate = origBuildDate
	}()

	tests := []struct {
		name      string
		version   string
		commit    string
		buildDate string
		want      []string // substrings that should be present
	}{
		{
			name:      "production build",
			version:   "1.2.3",
			commit:    "abc123",
			buildDate: "2024-01-15",
			want:      []string{"javinizer", "1.2.3", "abc123", "2024-01-15", "go"},
		},
		{
			name:      "dev build",
			version:   "dev",
			commit:    "unknown",
			buildDate: "unknown",
			want:      []string{"javinizer", "dev", "unknown"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Version = tt.version
			Commit = tt.commit
			BuildDate = tt.buildDate

			got := Info()

			for _, substr := range tt.want {
				if !strings.Contains(got, substr) {
					t.Errorf("Info() = %q, should contain %q", got, substr)
				}
			}
		})
	}
}

func TestShort(t *testing.T) {
	// Save original values
	origVersion := Version
	origCommit := Commit
	defer func() {
		Version = origVersion
		Commit = origCommit
	}()

	tests := []struct {
		name    string
		version string
		commit  string
		want    string
	}{
		{
			name:    "production version",
			version: "1.2.3",
			commit:  "abc123def",
			want:    "1.2.3",
		},
		{
			name:    "dev version with commit",
			version: "dev",
			commit:  "abc123def",
			want:    "dev-abc123d",
		},
		{
			name:    "version with metadata",
			version: "2.0.0-beta.1",
			commit:  "xyz789abc",
			want:    "2.0.0-beta.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Version = tt.version
			Commit = tt.commit

			got := Short()

			if got != tt.want {
				t.Errorf("Short() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestShort_DevVersionCommitShortening(t *testing.T) {
	// Save original values
	origVersion := Version
	origCommit := Commit
	defer func() {
		Version = origVersion
		Commit = origCommit
	}()

	Version = "dev"
	Commit = "1234567890abcdef"

	got := Short()
	want := "dev-1234567"

	if got != want {
		t.Errorf("Short() with dev version = %q, want %q", got, want)
	}

	// Verify it takes exactly 7 characters
	if !strings.HasPrefix(got, "dev-") {
		t.Errorf("Short() dev version should start with 'dev-', got %q", got)
	}
	commitPart := strings.TrimPrefix(got, "dev-")
	if len(commitPart) != 7 {
		t.Errorf("Short() dev version should have 7-char commit hash, got %d chars: %q", len(commitPart), commitPart)
	}
}

func TestGoVersion(t *testing.T) {
	// GoVersion is set from runtime.Version()
	if GoVersion == "" {
		t.Error("GoVersion should not be empty")
	}

	// Should start with "go"
	if !strings.HasPrefix(GoVersion, "go") {
		t.Errorf("GoVersion should start with 'go', got %q", GoVersion)
	}
}
