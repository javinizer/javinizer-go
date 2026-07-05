package updater

import (
	"fmt"
	"runtime"
)

// BundleAssetName returns the release-asset filename for the desktop bundle
// on the given GOOS/GOARCH. This is distinct from internal/update.AssetName
// (which returns the CLI binary asset): desktop builds ship native bundles
// (.zip-wrapped .app on macOS, a bare .exe on Windows, an .AppImage on Linux).
//
// The names MUST stay in sync with the assets published by
// .github/workflows/cli-release.yml (build-desktop-darwin/windows/linux jobs)
// and listed in the release checksums.txt. Desktop arch naming follows the
// bundle convention, not Go's: Linux AppImages use uname -m arches
// (x86_64/aarch64), not amd64/arm64.
func BundleAssetName(goos, goarch string) (string, error) {
	switch goos {
	case "darwin":
		// Universal zip (amd64+arm64) — one asset for both arches.
		return "Javinizer-macos-universal.zip", nil
	case "windows":
		if goarch == "amd64" {
			return "Javinizer.exe", nil
		}
	case "linux":
		arch, ok := appImageArch(goarch)
		if ok {
			return "Javinizer-linux-" + arch + ".AppImage", nil
		}
	}
	return "", fmt.Errorf("no desktop bundle asset for %s/%s", goos, goarch)
}

// appImageArch maps a Go GOARCH to the uname -m arch used in AppImage asset
// names. Returns ok=false for unsupported arches.
func appImageArch(goarch string) (string, bool) {
	switch goarch {
	case "amd64":
		return "x86_64", true
	case "arm64":
		return "aarch64", true
	}
	return "", false
}

// currentBundleAsset returns the asset name for the running build, or an error
// if this platform has no published desktop bundle (e.g. windows-arm64).
//
// The error branch is intentionally NOT covered by tests: it only fires on a
// host OS/arch with no published desktop bundle (windows/arm64, freebsd/*).
// CI runs the test suite on darwin/amd64+arm64, linux/amd64+arm64, and
// windows/amd64 — all of which have a published bundle — so currentBundleAsset
// always succeeds there. BundleAssetName itself is covered for the error
// branches by TestBundleAssetName, but the currentBundleAsset() wrapper is
// only reached via Upgrade(), which cannot be made to fail at this step in CI.
func currentBundleAsset() (string, error) {
	return BundleAssetName(runtime.GOOS, runtime.GOARCH)
}
