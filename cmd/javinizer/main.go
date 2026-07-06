package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	"github.com/javinizer/javinizer-go/internal/desktop"
)

// executeFn is the command entry point main() invokes. It is a package-level
// var so tests can stub it: the real Execute runs cobra with process-wide
// side effects (and main()'s os.Exit) that are impractical to exercise
// directly. Production wires it to Execute at package init.
var executeFn = Execute

// osExit is os.Exit as a var so tests can call main() without terminating
// the test process: main() becomes `osExit(run())`, and stubbing osExit to a
// no-op lets a test exercise main()'s single statement.
var osExit = os.Exit

func main() {
	osExit(run())
}

// run is the testable body of main: it returns the process exit code instead
// of calling os.Exit, and recovers panics so a pre-GUI crash is recorded
// (crash.log on desktop, stderr on CLI) rather than swallowed by the nil
// stderr of a double-click-launched GUI-subsystem binary.
func run() (exitCode int) {
	if desktop.IsDesktopBuild() && len(os.Args) > 1 {
		attachParentConsole()
	}
	defer func() {
		if r := recover(); r != nil {
			writeCrashLog(fmt.Sprintf("panic: %v\n%s", r, debug.Stack()))
			exitCode = 1
		}
	}()
	if err := executeFn(); err != nil {
		writeCrashLog(err.Error())
		return 1
	}
	return 0
}

// writeCrashLog records a pre-GUI failure (panic or Execute error) so a
// double-click-launched desktop app — whose stderr is nil — leaves a
// diagnostic trail. It always also writes to stderr: for CLI builds stderr is
// the conventional error stream, and for desktop builds stderr is harmless
// (nil writes are no-ops) and helps when launched from a terminal.
func writeCrashLog(msg string) {
	_, _ = fmt.Fprintln(os.Stderr, msg)
	if !desktop.IsDesktopBuild() {
		return
	}
	dir, err := desktop.UserDataDir()
	if err != nil {
		return
	}
	path := filepath.Join(dir, "crash.log")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = fmt.Fprintf(f, "%s %s\n", time.Now().Format(time.RFC3339), msg)
}
