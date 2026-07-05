//go:build windows

package updater

import (
	"strings"
)

// Windows detached-helper process creation flags. CREATE_NEW_PROCESS_GROUP
// is also exported by the syscall package; DETACHED_PROCESS is not, so it is
// defined here for the windows build to use.
const (
	windowsDetachedProcess       = 0x00000008
	windowsCreateNewProcessGroup = 0x00000200
)

// winBase returns the filename component of a Windows path, handling both
// backslash and forward slash separators. filepath.Base uses the host OS
// separator, which is wrong when generating a Windows .bat on a non-Windows
// host during tests, so this helper splits on both.
func winBase(p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		p = p[i+1:]
	}
	return p
}

// renderWindowsBatScript returns the body of the detached .bat helper that
// completes a Windows desktop self-upgrade after the running process exits.
//
// The batch runs detached (CWD is System32), so all paths MUST be absolute and
// passed in via %1 (PID), %2 (exe path), %3 (new exe staged path). The script:
//   - polls tasklist until the old PID is gone (capped, so a hung process does
//     not block the helper forever),
//   - removes a stale .old from a prior interrupted upgrade,
//   - renames the running exe to <name>.old (Windows permits renaming a
//     running exe; it cannot be overwritten in place),
//   - moves the staged new exe into place (rolling back from .old on failure),
//   - relaunches the new exe detached,
//   - best-effort deletes .old, then self-deletes the .bat.
//
// The helper inherits the parent's token; if the exe dir is not writable
// (e.g. Program Files without elevation) CanSwap already failed upstream, so
// the rename/move here are expected to succeed.
func renderWindowsBatScript(pid int, exePath, newPath string) string {
	exeName := winBase(exePath)
	var b strings.Builder
	b.WriteString("@echo off\r\n")
	b.WriteString("setlocal\r\n")
	b.WriteString("set \"PID=%~1\"\r\n")
	b.WriteString("set \"EXE=%~2\"\r\n")
	b.WriteString("set \"NEW=%~3\"\r\n")
	b.WriteString("set /a COUNT=0\r\n")
	b.WriteString(":wait\r\n")
	b.WriteString("tasklist /FI \"PID eq %PID%\" /NH 2>nul | find \"%PID%\" >nul\r\n")
	b.WriteString("if not errorlevel 1 (\r\n")
	b.WriteString("  ping -n 2 127.0.0.1 >nul\r\n")
	b.WriteString("  set /a COUNT+=1\r\n")
	b.WriteString("  if %COUNT% geq 60 goto cleanup\r\n")
	b.WriteString("  goto wait\r\n")
	b.WriteString(")\r\n")
	b.WriteString("if exist \"%EXE%.old\" del /f /q \"%EXE%.old\" 2>nul\r\n")
	b.WriteString("rename \"%EXE%\" \"" + exeName + ".old\"\r\n")
	b.WriteString("if errorlevel 1 goto cleanup\r\n")
	b.WriteString("move /y \"%NEW%\" \"%EXE%\" 2>nul\r\n")
	b.WriteString("if errorlevel 1 (\r\n")
	b.WriteString("  rename \"%EXE%.old\" \"" + exeName + "\" 2>nul\r\n")
	b.WriteString("  goto cleanup\r\n")
	b.WriteString(")\r\n")
	b.WriteString("start \"\" \"%EXE%\"\r\n")
	b.WriteString("del /f /q \"%EXE%.old\" 2>nul\r\n")
	b.WriteString(":cleanup\r\n")
	b.WriteString("if exist \"%NEW%\" del /f /q \"%NEW%\" 2>nul\r\n")
	b.WriteString("(goto) 2>nul & del \"%~f0\"\r\n")
	return b.String()
}
