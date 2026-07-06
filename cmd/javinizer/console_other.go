//go:build !windows

package main

// attachParentConsole is a no-op on non-Windows: those platforms don't use a
// GUI subsystem flag, so the process inherits the parent terminal's stdio
// directly. It is a var (not a func) so tests can assert the desktop-build
// branch in run() takes it.
var attachParentConsole = func() {}
