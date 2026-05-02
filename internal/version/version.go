package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
)

// Build information. Values are injected at build time via ldflags.
var (
	Version   = "dev"
	Commit    = unknown
	BuildDate = unknown
	GoVersion = runtime.Version()
)

const unknown = "unknown"

func init() {
	// applyBuildInfo must run before applyDevVersion.
	// applyDevVersion only acts when Version == "dev", and applyBuildInfo
	// may set Version from module info. Reversing the order would cause
	// isPseudoVersion() to reject the v0.0.0-* format that applyDevVersion produces.
	if info, ok := debug.ReadBuildInfo(); ok {
		applyBuildInfo(info)
	}
	applyDevVersion()
}

// applyBuildInfo populates version metadata from Go build info when ldflags
// were not provided (e.g. `go install module@version` or local `go build`).
func applyBuildInfo(info *debug.BuildInfo) {
	if info == nil {
		return
	}

	if (Version == "" || Version == "dev") &&
		info.Main.Version != "" &&
		info.Main.Version != "(devel)" &&
		!isPseudoVersion(info.Main.Version) {
		Version = info.Main.Version
	}

	settings := make(map[string]string, len(info.Settings))
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}

	if (Commit == "" || Commit == "unknown") && settings["vcs.revision"] != "" {
		Commit = settings["vcs.revision"]
	}
	if (BuildDate == "" || BuildDate == "unknown") && settings["vcs.time"] != "" {
		BuildDate = settings["vcs.time"]
	}
	if settings["vcs.modified"] == "true" &&
		Commit != "" &&
		Commit != "unknown" &&
		!strings.HasSuffix(Commit, "-dirty") {
		Commit += "-dirty"
	}
}

func isPseudoVersion(v string) bool {
	return strings.HasPrefix(v, "v0.0.0-") || strings.Contains(v, "+dirty")
}

func applyDevVersion() {
	if Version != "dev" {
		return
	}

	Version = "v0.0.0"
	if Commit != "" && Commit != unknown {
		commit := strings.TrimSuffix(Commit, "-dirty")
		if len(commit) > 12 {
			commit = commit[:12]
		}
		Version = "v0.0.0-" + commit
		if strings.HasSuffix(Commit, "-dirty") {
			Version += "-dirty"
		}
	}
}

// Info returns formatted version information
func Info() string {
	return fmt.Sprintf("javinizer %s (commit: %s, built: %s, go: %s)",
		Version, Commit, BuildDate, GoVersion)
}

// Short returns just the version number
func Short() string {
	return Version
}

// IsPrerelease returns true if the version is a prerelease.
// A prerelease version contains a hyphen followed by identifiers (e.g., 1.6.0-rc1).
func IsPrerelease(version string) bool {
	// Remove leading 'v' if present
	v := strings.TrimPrefix(version, "v")
	// Prereleases contain a hyphen followed by identifiers (e.g., 1.6.0-rc1)
	return strings.Contains(v, "-")
}
