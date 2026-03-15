package version

import (
	"runtime/debug"
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

func TestShort_DevVersionCommitShort(t *testing.T) {
	origVersion := Version
	origCommit := Commit
	defer func() {
		Version = origVersion
		Commit = origCommit
	}()

	Version = "dev"
	Commit = "abc"

	got := Short()
	want := "dev-abc"

	if got != want {
		t.Errorf("Short() with short commit = %q, want %q", got, want)
	}
}

func TestShort_DevVersionDirtyCommit(t *testing.T) {
	origVersion := Version
	origCommit := Commit
	defer func() {
		Version = origVersion
		Commit = origCommit
	}()

	Version = "dev"
	Commit = "1234567890abcdef-dirty"

	got := Short()
	want := "dev-1234567-dirty"

	if got != want {
		t.Errorf("Short() with dirty commit = %q, want %q", got, want)
	}
}

func TestApplyBuildInfo(t *testing.T) {
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
		info      *debug.BuildInfo
		wantVer   string
		wantCom   string
		wantDate  string
	}{
		{
			name:      "uses module version when current version is dev",
			version:   "dev",
			commit:    "unknown",
			buildDate: "unknown",
			info: &debug.BuildInfo{
				Main: debug.Module{Version: "v1.2.3"},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "abcdef1234567890"},
					{Key: "vcs.time", Value: "2026-02-23T00:00:00Z"},
				},
			},
			wantVer:  "v1.2.3",
			wantCom:  "abcdef1234567890",
			wantDate: "2026-02-23T00:00:00Z",
		},
		{
			name:      "keeps ldflags values when already set",
			version:   "v9.9.9",
			commit:    "deadbee",
			buildDate: "2025-01-01",
			info: &debug.BuildInfo{
				Main: debug.Module{Version: "v1.2.3"},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "abcdef1234567890"},
					{Key: "vcs.time", Value: "2026-02-23T00:00:00Z"},
				},
			},
			wantVer:  "v9.9.9",
			wantCom:  "deadbee",
			wantDate: "2025-01-01",
		},
		{
			name:      "marks commit dirty when vcs modified",
			version:   "dev",
			commit:    "unknown",
			buildDate: "unknown",
			info: &debug.BuildInfo{
				Main: debug.Module{Version: "(devel)"},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "abcdef1234567890"},
					{Key: "vcs.modified", Value: "true"},
				},
			},
			wantVer:  "dev",
			wantCom:  "abcdef1234567890-dirty",
			wantDate: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Version = tt.version
			Commit = tt.commit
			BuildDate = tt.buildDate

			applyBuildInfo(tt.info)

			if Version != tt.wantVer {
				t.Errorf("Version = %q, want %q", Version, tt.wantVer)
			}
			if Commit != tt.wantCom {
				t.Errorf("Commit = %q, want %q", Commit, tt.wantCom)
			}
			if BuildDate != tt.wantDate {
				t.Errorf("BuildDate = %q, want %q", BuildDate, tt.wantDate)
			}
		})
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

func TestApplyTrackedVersion(t *testing.T) {
	origVersion := Version
	origTracked := trackedVersion
	defer func() {
		Version = origVersion
		trackedVersion = origTracked
	}()

	t.Run("uses tracked version when build version is default", func(t *testing.T) {
		Version = "dev"
		trackedVersion = "v2.3.4-dev\n"

		applyTrackedVersion()

		if Version != "v2.3.4-dev" {
			t.Fatalf("Version = %q, want %q", Version, "v2.3.4-dev")
		}
	})

	t.Run("keeps explicit build version", func(t *testing.T) {
		Version = "v9.9.9"
		trackedVersion = "v2.3.4-dev"

		applyTrackedVersion()

		if Version != "v9.9.9" {
			t.Fatalf("Version = %q, want %q", Version, "v9.9.9")
		}
	})
}
