package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/javinizer/javinizer-go/internal/desktop"
)

func main() {
	if desktop.IsDesktopBuild() && len(os.Args) > 1 {
		attachParentConsole()
	}
	defer func() {
		if r := recover(); r != nil {
			writeCrashLog(fmt.Sprintf("panic: %v", r))
			os.Exit(1)
		}
	}()
	if err := Execute(); err != nil {
		writeCrashLog(err.Error())
		os.Exit(1)
	}
}

func writeCrashLog(msg string) {
	if !desktop.IsDesktopBuild() {
		fmt.Println(msg)
		return
	}
	dir, err := desktop.UserDataDir()
	if err == nil {
		_ = os.WriteFile(filepath.Join(dir, "crash.log"), []byte(msg+"\n"), 0o600)
	}
	_, _ = fmt.Fprintln(os.Stderr, msg)
}
