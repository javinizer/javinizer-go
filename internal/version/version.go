package version

import (
	_ "embed"
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
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

//go:embed version.txt
var trackedVersion string

func init() {
	if info, ok := debug.ReadBuildInfo(); ok {
		applyBuildInfo(info)
	}
	applyTrackedVersion()
}

// applyBuildInfo populates version metadata from Go build info when ldflags
// were not provided (e.g. `go install module@version` or local `go build`).
func applyBuildInfo(info *debug.BuildInfo) {
	if info == nil {
		return
	}

	if (Version == "" || Version == "dev") &&
		info.Main.Version != "" &&
		info.Main.Version != "(devel)" {
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

func applyTrackedVersion() {
	if Version != "" && Version != "dev" {
		return
	}

	if tracked := strings.TrimSpace(trackedVersion); tracked != "" {
		Version = tracked
	}
}

// Info returns formatted version information
func Info() string {
	return fmt.Sprintf("javinizer %s (commit: %s, built: %s, go: %s)",
		Version, Commit, BuildDate, GoVersion)
}

// Short returns just the version number
func Short() string {
	if Version == "dev" {
		commit := Commit
		dirtySuffix := ""
		if strings.HasSuffix(commit, "-dirty") {
			dirtySuffix = "-dirty"
			commit = strings.TrimSuffix(commit, dirtySuffix)
		}
		if len(commit) > 7 {
			commit = commit[:7]
		}
		return fmt.Sprintf("%s-%s%s", Version, commit, dirtySuffix)
	}
	return Version
}
