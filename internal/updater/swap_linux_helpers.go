//go:build linux

package updater

import (
	"fmt"
	"os"
	"strings"
)

// renderLinuxHelperScript builds the sh -c body executed by the detached Linux
// swap helper. It waits for oldPID to exit (capped so a wedged process does not
// hang the helper forever), atomically moves the staged .AppImage over the
// target, marks it executable, optionally re-exports APPIMAGE_EXTRACT_AND_RUN
// (so an extract-mode AppImage relaunches in extract mode), and detaches the
// relaunch via nohup. Paths are single-quoted for the shell.
func renderLinuxHelperScript(oldPID int, targetPath, stagedPath string, preserveExtract bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "i=0\nwhile kill -0 %d 2>/dev/null; do\n", oldPID)
	b.WriteString("  i=$((i+1))\n")
	fmt.Fprintf(&b, "  if [ \"$i\" -ge %d ]; then echo 'javinizer: timed out waiting for process exit' >&2; exit 1; fi\n", swapWaitMaxIters)
	b.WriteString("  sleep 0.2\n")
	b.WriteString("done\n")
	fmt.Fprintf(&b, "mv -f %s %s\n", shellQuote(stagedPath), shellQuote(targetPath))
	fmt.Fprintf(&b, "chmod +x %s\n", shellQuote(targetPath))
	if preserveExtract {
		b.WriteString("export APPIMAGE_EXTRACT_AND_RUN=1\n")
	}
	fmt.Fprintf(&b, "nohup %s >/dev/null 2>&1 &\n", shellQuote(targetPath))
	return b.String()
}

// resolveAppImageTarget resolves the on-disk .AppImage to replace. The AppImage
// runtime exports APPIMAGE in both FUSE and extract-and-run modes, pointing at
// the real bundle path (os.Executable would return a /tmp/.mount_* FUSE
// mountpoint, which is not the swap target). When APPIMAGE is unset or points
// at a non-executable file (direct-binary dev/test runs), it falls back to the
// symlink-resolved executable path. It returns an error if neither resolves.
func resolveAppImageTarget(envAppImage, exePath string) (string, error) {
	if envAppImage != "" && fileExistsAndExecutable(envAppImage) {
		return envAppImage, nil
	}
	resolved, err := evalSymlinks(exePath)
	if err != nil {
		return "", fmt.Errorf("resolve AppImage target: APPIMAGE=%q not usable and executable path %q did not resolve: %w", envAppImage, exePath, err)
	}
	return resolved, nil
}

// fileExistsAndExecutable reports whether path is a regular file with any
// executable bit set. It is the portable (os.Access-free) equivalent of an
// X_OK check, so it compiles and runs on every OS the helper tests run on.
func fileExistsAndExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode().Perm()&0o111 != 0
}
