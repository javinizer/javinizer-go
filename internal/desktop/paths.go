package desktop

import (
	"fmt"
	"os"
	"path/filepath"
)

var (
	userConfigDirFn = os.UserConfigDir
	userHomeDirFn   = os.UserHomeDir
)

// appDataDirName is the per-user directory name under the OS config dir.
const appDataDirName = "Javinizer"

// UserDataDir returns a portable, CWD-independent directory for the desktop
// app's config, database, and logs. It is created if missing.
//
//   - macOS: ~/Library/Application Support/Javinizer
//   - Windows: %APPDATA%\Javinizer
//   - Linux: $XDG_CONFIG_HOME/Javinizer or ~/.config/Javinizer
func UserDataDir() (string, error) {
	base, err := userConfigDirFn()
	if err != nil || base == "" {
		home, homeErr := userHomeDirFn()
		if homeErr != nil {
			return "", fmt.Errorf("desktop: cannot locate user data dir: %w", homeErr)
		}
		base = filepath.Join(home, ".javinizer")
	}
	dir := filepath.Join(base, appDataDirName)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("desktop: cannot create user data dir %s: %w", dir, err)
	}
	return dir, nil
}

// DefaultConfigPath returns the portable config path under UserDataDir. On
// error it falls back to the CLI default so the app still attempts to boot.
func DefaultConfigPath() string {
	dir, err := UserDataDir()
	if err != nil {
		return "configs/config.yaml"
	}
	return filepath.Join(dir, "config.yaml")
}

// SetupPortableEnv points JAVINIZER_DB and JAVINIZER_LOG_DIR at absolute paths
// under UserDataDir when they are not already set. This keeps the database and
// logs in a writable, CWD-independent location when the app is launched from
// Finder/Explorer (where CWD is "/" or the bundle dir and the default
// relative paths "data/javinizer.db" / "data/logs/javinizer.log" would fail).
//
// It should run before config init so ApplyEnvironmentOverrides picks the
// portable paths up.
func SetupPortableEnv() error {
	dir, err := UserDataDir()
	if err != nil {
		return err
	}
	dataDir := filepath.Join(dir, "data")
	logDir := filepath.Join(dataDir, "logs")
	tempDir := filepath.Join(dataDir, "temp")
	for _, sub := range []string{dataDir, logDir, tempDir} {
		if err := os.MkdirAll(sub, 0o750); err != nil {
			return fmt.Errorf("desktop: cannot create %s: %w", sub, err)
		}
	}
	if os.Getenv("JAVINIZER_DB") == "" {
		_ = os.Setenv("JAVINIZER_DB", filepath.Join(dataDir, "javinizer.db"))
	}
	if os.Getenv("JAVINIZER_LOG_DIR") == "" {
		_ = os.Setenv("JAVINIZER_LOG_DIR", logDir)
	}
	// JAVINIZER_TEMP_DIR points temp poster generation at an absolute, writable
	// path under the portable data dir. Without it the config default
	// "data/temp" stays relative and resolves against CWD — which is "/" or the
	// bundle dir when launched from Finder/Explorer — so temp posters can't be
	// written (poster generation fails silently) and the review-page crop modal
	// 404s on the -full.jpg source it expects to find there.
	if os.Getenv("JAVINIZER_TEMP_DIR") == "" {
		_ = os.Setenv("JAVINIZER_TEMP_DIR", tempDir)
	}
	// JAVINIZER_DATA_DIR is the update package's portable cache location
	// (internal/update/paths.go: dataDir()). Without it the update cache falls
	// back to a CWD-relative "data/" path, which is unwritable when the app is
	// launched from Finder/Explorer (CWD is "/" or the bundle dir) — the
	// startup check logs "Update available" but the cache write silently fails,
	// so /version returns source=none and the in-app update indicator never
	// appears. Pointing it at the same portable data dir as the DB/logs makes
	// the cache persist across launches on every platform.
	if os.Getenv("JAVINIZER_DATA_DIR") == "" {
		_ = os.Setenv("JAVINIZER_DATA_DIR", dataDir)
	}
	// The r18.dev dump sidecar (data/r18dev/r18dev_dump.db) is a relative
	// path in the default config. In the desktop app CWD is unwritable, so
	// override it to an absolute path under the portable data dir.
	if os.Getenv("JAVINIZER_R18DEV_DUMP_PATH") == "" {
		_ = os.Setenv("JAVINIZER_R18DEV_DUMP_PATH", filepath.Join(dataDir, "r18dev", "r18dev_dump.db"))
	}
	return nil
}
