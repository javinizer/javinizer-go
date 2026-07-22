//go:build windows

package updater

import (
	"strconv"
	"strings"
	"testing"
)

func TestWinBase(t *testing.T) {
	cases := []struct{ in, want string }{
		{`C:\Program Files\Javinizer\Javinizer.exe`, "Javinizer.exe"},
		{`C:/Users/me/Javinizer.exe`, "Javinizer.exe"},
		{`Javinizer.exe`, "Javinizer.exe"},
		{`C:\foo\bar\`, ""},
	}
	for _, c := range cases {
		if got := winBase(c.in); got != c.want {
			t.Errorf("winBase(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRenderWindowsBatScript_ContainsRequiredSteps(t *testing.T) {
	pid := 12345
	exe := `C:\Program Files\Javinizer\Javinizer.exe`
	newPath := `C:\Program Files\Javinizer\.javinizer-bundle-upgrade-123.tmp`
	script := renderWindowsBatScript(pid, exe, newPath)

	required := []string{
		"@echo off",
		"set \"PID=%~1\"",
		"set \"EXE=%~2\"",
		"set \"NEW=%~3\"",
		":wait",
		"tasklist /FI \"PID eq %PID%\" /NH",
		"ping -n 2 127.0.0.1",
		"if %COUNT% geq 60 goto cleanup",
		"if exist \"%EXE%.old\" del /f /q \"%EXE%.old\"",
		"rename \"%EXE%\" \"Javinizer.exe.old\"",
		"move /y \"%NEW%\" \"%EXE%\"",
		"rename \"%EXE%.old\" \"Javinizer.exe\"",
		"start \"\" \"%EXE%\"",
		"del /f /q \"%EXE%.old\"",
		":cleanup",
		"(goto) 2>nul & del \"%~f0\"",
	}
	for _, want := range required {
		if !strings.Contains(script, want) {
			t.Errorf("script missing %q\n--- script ---\n%s", want, script)
		}
	}
}

func TestRenderWindowsBatScript_UsesArgPassing(t *testing.T) {
	exe := `C:\Apps\Javinizer.exe`
	newPath := `C:\Apps\.javinizer-bundle-upgrade-1.tmp`
	script := renderWindowsBatScript(1, exe, newPath)

	// Paths flow in as batch args (%~1 PID, %~2 EXE, %~3 NEW), NOT hardcoded —
	// the .bat is reusable and the literal exe/staged paths never appear in it.
	required := []string{
		`set "PID=%~1"`,
		`set "EXE=%~2"`,
		`set "NEW=%~3"`,
		`rename "%EXE%"`,
		`move /y "%NEW%" "%EXE%"`,
		`start "" "%EXE%"`,
	}
	for _, want := range required {
		if !strings.Contains(script, want) {
			t.Errorf("script missing %q\n--- script ---\n%s", want, script)
		}
	}
	if strings.Contains(script, exe) {
		t.Errorf("script should NOT hardcode the exe path (uses %%~2); got %q in script", exe)
	}
	if strings.Contains(script, newPath) {
		t.Errorf("script should NOT hardcode the staged path (uses %%~3); got %q in script", newPath)
	}
}

// TestNewWindowsHelperCmd_NoVisibleWindow asserts the actual *exec.Cmd that
// SwapAndRelaunch builds (via newWindowsHelperCmd) carries the
// process-creation attributes that keep the Windows upgrade helper silent —
// not just the constant value. It is the regression guard for the stray
// 'find "<pid>"' Windows Terminal window users saw mid desktop upgrade.
//
// The desktop exe is built with -H windowsgui (GUI subsystem, no console).
// DETACHED_PROCESS (0x8) only detaches cmd.exe from the parent's console; the
// batch's children (tasklist.exe, ping.exe) then each allocate their OWN
// visible console -> a #0c0c0c Windows Terminal pops up. CREATE_NO_WINDOW
// (0x08000000) suppresses console allocation for the helper and the
// console-subsystem commands it runs. Per MS docs, CREATE_NO_WINDOW is
// ignored when combined with DETACHED_PROCESS or CREATE_NEW_CONSOLE, so
// neither may be present alongside it.
//
// This test does not spawn a process (cmd.Start is never called), so it is
// deterministic on Windows CI.
func TestNewWindowsHelperCmd_NoVisibleWindow(t *testing.T) {
	const (
		createNoWindow        = uint32(0x08000000)
		detachedProcess       = uint32(0x00000008)
		createNewConsole      = uint32(0x00000010)
		createNewProcessGroup = uint32(0x00000200)
	)

	const pid = 41784
	exe := "C:/Program Files/Javinizer/Javinizer.exe"
	batch := "C:/Users/me/AppData/Local/Temp/javinizer-update-41784.bat"
	staged := "C:/Users/me/.javinizer-bundle-upgrade-41784.tmp"

	cmd := newWindowsHelperCmd(pid, exe, batch, staged)

	// The helper is invoked as: cmd.exe /c <batch> <pid> <exe> <staged>
	wantArgs := []string{"cmd.exe", "/c", batch, strconv.Itoa(pid), exe, staged}
	if len(cmd.Args) != len(wantArgs) {
		t.Fatalf("cmd.Args length = %d, want %d; got %#v", len(cmd.Args), len(wantArgs), cmd.Args)
	}
	for i, want := range wantArgs {
		if cmd.Args[i] != want {
			t.Fatalf("cmd.Args[%d] = %q, want %q; full args %#v", i, cmd.Args[i], want, cmd.Args)
		}
	}

	sp := cmd.SysProcAttr
	if sp == nil {
		t.Fatal("cmd.SysProcAttr is nil; expected CREATE_NO_WINDOW flags to be set")
	}
	flags := uint32(sp.CreationFlags)

	if flags&createNoWindow == 0 {
		t.Errorf("CreationFlags = %#x is missing CREATE_NO_WINDOW (0x08000000); "+
			"the desktop upgrade helper would pop a visible Windows Terminal window "+
			"(regression: previously used DETACHED_PROCESS)", flags)
	}
	if flags&detachedProcess != 0 {
		t.Errorf("CreationFlags = %#x must NOT include DETACHED_PROCESS (0x8); "+
			"CREATE_NO_WINDOW is ignored when combined with DETACHED_PROCESS, so "+
			"the no-window behavior would be silently dropped", flags)
	}
	if flags&createNewConsole != 0 {
		t.Errorf("CreationFlags = %#x must NOT include CREATE_NEW_CONSOLE (0x10); "+
			"it allocates a visible console for the helper", flags)
	}
	if flags&createNewProcessGroup == 0 {
		t.Errorf("CreationFlags = %#x is missing CREATE_NEW_PROCESS_GROUP (0x200); "+
			"process-group/Ctrl+C isolation for the helper was lost", flags)
	}
	if !sp.HideWindow {
		t.Error("HideWindow = false; expected true as defense in depth (cmd.exe window)")
	}
	if cmd.Stdin != nil || cmd.Stdout != nil || cmd.Stderr != nil {
		t.Errorf("stdio must be nil so the helper never touches the parent's pipes; "+
			"got stdin=%v stdout=%v stderr=%v", cmd.Stdin, cmd.Stdout, cmd.Stderr)
	}
}
