package version

import (
	"runtime/debug"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
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
	origVersion := Version
	defer func() { Version = origVersion }()

	tests := []struct {
		name    string
		version string
		want    string
	}{
		{
			name:    "production version",
			version: "1.2.3",
			want:    "1.2.3",
		},
		{
			name:    "dev pseudo-version",
			version: "v0.0.0-abc123def456",
			want:    "v0.0.0-abc123def456",
		},
		{
			name:    "version with metadata",
			version: "2.0.0-beta.1",
			want:    "2.0.0-beta.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Version = tt.version

			got := Short()

			if got != tt.want {
				t.Errorf("Short() = %q, want %q", got, tt.want)
			}
		})
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

func TestApplyDevVersion(t *testing.T) {
	origVersion := Version
	origCommit := Commit
	defer func() {
		Version = origVersion
		Commit = origCommit
	}()

	t.Run("constructs pseudo-version from commit when build version is dev", func(t *testing.T) {
		Version = "dev"
		Commit = "abcdef1234567890"

		applyDevVersion()

		if Version != "v0.0.0-abcdef123456" {
			t.Fatalf("Version = %q, want %q", Version, "v0.0.0-abcdef123456")
		}
	})

	t.Run("handles dirty commit suffix", func(t *testing.T) {
		Version = "dev"
		Commit = "abcdef1234567890-dirty"

		applyDevVersion()

		if Version != "v0.0.0-abcdef123456-dirty" {
			t.Fatalf("Version = %q, want %q", Version, "v0.0.0-abcdef123456-dirty")
		}
	})

	t.Run("keeps explicit build version", func(t *testing.T) {
		Version = "v9.9.9"
		Commit = "abcdef1234567890"

		applyDevVersion()

		if Version != "v9.9.9" {
			t.Fatalf("Version = %q, want %q", Version, "v9.9.9")
		}
	})

	t.Run("falls back to v0.0.0 when commit is unknown", func(t *testing.T) {
		Version = "dev"
		Commit = "unknown"

		applyDevVersion()

		if Version != "v0.0.0" {
			t.Fatalf("Version = %q, want %q", Version, "v0.0.0")
		}
	})
}

func TestIsPseudoVersion(t *testing.T) {
	tests := []struct {
		v    string
		want bool
	}{
		{"v0.0.0-20260101120000-abcdef123456", true},
		{"v1.2.3+dirty", true},
		{"v1.2.3", false},
		{"v0.0.0", false},
		{"(devel)", false},
	}

	for _, tt := range tests {
		t.Run(tt.v, func(t *testing.T) {
			assert.Equal(t, tt.want, isPseudoVersion(tt.v))
		})
	}
}

func TestApplyBuildInfo_SkipsPseudoVersion(t *testing.T) {
	origVersion := Version
	defer func() { Version = origVersion }()

	Version = "dev"
	info := &debug.BuildInfo{
		Main: debug.Module{Version: "v0.0.0-20260101120000-abcdef123456"},
	}
	applyBuildInfo(info)

	assert.Equal(t, "dev", Version)
}

func TestIsPrerelease(t *testing.T) {
	tests := []struct {
		version  string
		expected bool
	}{
		{"v1.6.0", false},
		{"1.6.0", false},
		{"v1.6.0-rc1", true},
		{"1.6.0-rc1", true},
		{"v1.6.0-beta.2", true},
		{"1.6.0-beta.2", true},
		{"v1.6.0-alpha", true},
		{"1.6.0-alpha", true},
		{"v1.6.0-rc1-123-gabc123", true},
		{"v2.0.0", false},
		{"1.0.0", false},
		{"v0.1.0-dev", true},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			result := IsPrerelease(tt.version)
			assert.Equal(t, tt.expected, result, "IsPrerelease(%q)", tt.version)
		})
	}
}
