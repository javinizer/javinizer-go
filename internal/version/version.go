package version

import (
	"fmt"
	"runtime"
)

// Build information. Values are injected at build time via ldflags.
var (
	// Version is the semantic version (e.g., "1.0.0")
	Version = "dev"

	// Commit is the git commit hash
	Commit = "unknown"

	// BuildDate is the build timestamp
	BuildDate = "unknown"

	// GoVersion is the Go version used to build
	GoVersion = runtime.Version()
)

// Info returns formatted version information
func Info() string {
	return fmt.Sprintf("javinizer %s (commit: %s, built: %s, go: %s)",
		Version, Commit, BuildDate, GoVersion)
}

// Short returns just the version number
func Short() string {
	if Version == "dev" {
		return fmt.Sprintf("%s-%s", Version, Commit[:7])
	}
	return Version
}
