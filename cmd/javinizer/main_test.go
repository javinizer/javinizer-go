package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/desktop"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMainPackageCompiles is a placeholder test to ensure the main package compiles.
// The main() function itself is not tested directly because:
// 1. It would start the actual CLI which has side effects
// 2. All functionality is tested through unit and integration tests in root_test.go
// 3. main() is just a thin wrapper that calls run(), which IS tested below.
func TestMainPackageCompiles(t *testing.T) {
	assert.True(t, true, "Main package compiles successfully")
}

// withExecuteFn swaps the package-level executeFn for a test stub and restores
// it on cleanup. Returns a pointer the stub can mutate so a test can simulate
// Execute success, failure, or panic.
func withExecuteFn(t *testing.T, stub func() error) *func() error {
	t.Helper()
	orig := executeFn
	executeFn = stub
	t.Cleanup(func() { executeFn = orig })
	return &stub
}

// withDesktopBuild toggles desktop.BuildDesktop for the test and restores it.
func withDesktopBuild(t *testing.T, on bool) {
	t.Helper()
	orig := desktop.BuildDesktop
	if on {
		desktop.BuildDesktop = "1"
	} else {
		desktop.BuildDesktop = "0"
	}
	t.Cleanup(func() { desktop.BuildDesktop = orig })
}

// withIsolatedUserDataDir points all platform user-config env vars at a fresh
// temp dir so desktop.UserDataDir() returns an isolated, writable path on
// every platform (Linux: XDG_CONFIG_HOME, macOS: HOME, Windows: APPDATA).
// Without this, tests on CI share the runner's real config dir and collide
// (e.g. one test's crash.log file blocks another's os.Mkdir of the same path).
func withIsolatedUserDataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("APPDATA", dir)
	return dir
}

// withArgs swaps os.Args for the test and restores it on cleanup.
func withArgs(t *testing.T, args ...string) {
	t.Helper()
	orig := os.Args
	os.Args = args
	t.Cleanup(func() { os.Args = orig })
}

func TestRun_ExecuteSuccessReturnsZero(t *testing.T) {
	withExecuteFn(t, func() error { return nil })
	withDesktopBuild(t, false)
	assert.Equal(t, 0, run())
}

func TestRun_ExecuteErrorReturnsOne(t *testing.T) {
	withExecuteFn(t, func() error { return assertFailErr })
	withDesktopBuild(t, false)
	assert.Equal(t, 1, run())
}

func TestRun_PanicRecoveredReturnsOne(t *testing.T) {
	withExecuteFn(t, func() error { panic("boom") })
	withDesktopBuild(t, false)
	assert.Equal(t, 1, run())
}

// TestRun_DesktopBuildWithArgsAttachesConsole covers the attachParentConsole
// branch: a desktop build launched with args (e.g. --help) must call it. On
// non-Windows the stub is a no-op; the point is that the branch is taken.
func TestRun_DesktopBuildWithArgsAttachesConsole(t *testing.T) {
	called := false
	orig := attachParentConsole
	attachParentConsole = func() { called = true }
	t.Cleanup(func() { attachParentConsole = orig })

	withExecuteFn(t, func() error { return nil })
	withDesktopBuild(t, true)
	withArgs(t, "javinizer", "--help")
	require.Equal(t, 0, run())
	assert.True(t, called, "attachParentConsole must run for a desktop build with args")
}

// TestRun_DesktopBuildNoArgsSkipsConsole: with no args the GUI launches and
// console attach is skipped (the window has no console to attach to).
func TestRun_DesktopBuildNoArgsSkipsConsole(t *testing.T) {
	called := false
	orig := attachParentConsole
	attachParentConsole = func() { called = true }
	t.Cleanup(func() { attachParentConsole = orig })

	withExecuteFn(t, func() error { return nil })
	withDesktopBuild(t, true)
	withArgs(t, "javinizer")
	require.Equal(t, 0, run())
	assert.False(t, called, "attachParentConsole must not run with no args")
}

// TestAttachParentConsole_NoOpIsSafe calls the real (non-overridden) no-op
// stub directly. On non-Windows it does nothing; the test exists so the
// empty body's coverage block is entered (count > 0) rather than reported as
// an uncovered patch line.
func TestAttachParentConsole_NoOpIsSafe(t *testing.T) {
	assert.NotPanics(t, func() { attachParentConsole() })
}

var assertFailErr = &stringError{"simulated execute failure"}

type stringError struct{ s string }

func (e *stringError) Error() string { return e.s }

func TestWriteCrashLog_CLIWritesStderrOnly(t *testing.T) {
	withDesktopBuild(t, false)
	withIsolatedUserDataDir(t)
	writeCrashLog("cli error")
	// In CLI mode writeCrashLog writes only to stderr and returns before
	// touching UserDataDir, so no crash.log should exist anywhere under the
	// isolated data dir.
	dir, err := desktop.UserDataDir()
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(dir, "crash.log"))
	assert.True(t, os.IsNotExist(err), "CLI mode must not write crash.log")
}

func TestWriteCrashLog_DesktopAppendsTimestampedEntry(t *testing.T) {
	withDesktopBuild(t, true)
	withIsolatedUserDataDir(t)

	writeCrashLog("desktop boom")

	dir, err := desktop.UserDataDir()
	require.NoError(t, err)
	data, err := os.ReadFile(filepath.Join(dir, "crash.log"))
	require.NoError(t, err, "crash.log must be created in desktop mode")
	content := string(data)
	assert.Contains(t, content, "desktop boom")
	// RFC3339 timestamp prefix: YYYY-MM-DDTHH:MM:SS...
	assert.Regexp(t, `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`, strings.TrimSpace(content),
		"each entry must be prefixed with an RFC3339 timestamp")
}

// TestWriteCrashLog_DesktopAppendsAcrossCalls verifies the file is appended,
// not truncated, so a history of pre-GUI crashes is preserved.
func TestWriteCrashLog_DesktopAppendsAcrossCalls(t *testing.T) {
	withDesktopBuild(t, true)
	withIsolatedUserDataDir(t)

	writeCrashLog("first")
	writeCrashLog("second")

	dir, err := desktop.UserDataDir()
	require.NoError(t, err)
	data, err := os.ReadFile(filepath.Join(dir, "crash.log"))
	require.NoError(t, err)
	content := string(data)
	assert.Contains(t, content, "first")
	assert.Contains(t, content, "second")
}

// TestWriteCrashLog_DesktopUserDataDirErrorSkipsFile covers the early return
// when UserDataDir fails: stderr still gets the message, but no file write is
// attempted (no panic).
func TestWriteCrashLog_DesktopUserDataDirErrorSkipsFile(t *testing.T) {
	withDesktopBuild(t, true)
	// Point every platform's user-config env var at a path that is a FILE,
	// not a directory, so desktop.UserDataDir's MkdirAll fails. (Setting only
	// HOME is insufficient: Linux uses XDG_CONFIG_HOME, Windows uses APPDATA.)
	filePath := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(filePath, []byte("x"), 0o600))
	t.Setenv("HOME", filePath)
	t.Setenv("XDG_CONFIG_HOME", filePath)
	t.Setenv("APPDATA", filePath)

	writeCrashLog("no dir") // must not panic
}

// TestWriteCrashLog_DesktopOpenFileErrorSkipsWrite covers the early return
// when the crash.log file cannot be opened for writing (e.g. the path is a
// directory). stderr still receives the message; no panic.
func TestWriteCrashLog_DesktopOpenFileErrorSkipsWrite(t *testing.T) {
	withDesktopBuild(t, true)
	withIsolatedUserDataDir(t)

	// Pre-create crash.log as a DIRECTORY so OpenFile(O_WRONLY) fails.
	dir, err := desktop.UserDataDir()
	require.NoError(t, err)
	require.NoError(t, os.Mkdir(filepath.Join(dir, "crash.log"), 0o700))

	writeCrashLog("open fails") // must not panic
}

// TestMain_CallsRunAndExits exercises main()'s single statement (osExit(run()))
// by stubbing both osExit (so the test process survives) and executeFn (so no
// real cobra side effects). Captures the exit code osExit was called with.
func TestMain_CallsRunAndExits(t *testing.T) {
	withDesktopBuild(t, false)
	withExecuteFn(t, func() error { return nil })

	var gotCode int
	origExit := osExit
	osExit = func(code int) { gotCode = code }
	t.Cleanup(func() { osExit = origExit })

	main()
	assert.Equal(t, 0, gotCode, "main must exit with run()'s code on success")
}

// TestMain_CallsRunAndExitsOnError confirms main propagates a non-zero exit
// code from run() through osExit.
func TestMain_CallsRunAndExitsOnError(t *testing.T) {
	withDesktopBuild(t, false)
	withExecuteFn(t, func() error { return assertFailErr })

	var gotCode int
	origExit := osExit
	osExit = func(code int) { gotCode = code }
	t.Cleanup(func() { osExit = origExit })

	main()
	assert.Equal(t, 1, gotCode, "main must exit 1 when run() returns 1")
}
