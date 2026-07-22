//go:build windows

package updater

import (
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

// TestWindowsHelperCreationFlags_NoVisibleWindow pins the fix for the stray
// 'find "<pid>"' Windows Terminal window users saw mid desktop upgrade.
//
// The desktop exe is built with -H windowsgui (GUI subsystem, no console).
// DETACHED_PROCESS (0x8) only detaches cmd.exe from the parent's console; the
// batch's children (tasklist.exe, ping.exe) then each allocate their OWN
// visible console -> a #0c0c0c Windows Terminal pops up echoing the poll line
// 'tasklist ... | find "<pid>" >nul' and lingering until the app quits.
// CREATE_NO_WINDOW (0x08000000) suppresses all child console windows instead.
//
// This test is the regression guard: it does not spawn a process, so it runs
// deterministically on Windows CI; it asserts the invariant by value.
func TestWindowsHelperCreationFlags_NoVisibleWindow(t *testing.T) {
	const createNoWindow = uint32(0x08000000)
	const detachedProcess = uint32(0x00000008)

	flags := uint32(windowsHelperCreationFlags)

	if flags&createNoWindow == 0 {
		t.Fatalf("windowsHelperCreationFlags = %#x is missing CREATE_NO_WINDOW (0x08000000); "+
			"the desktop upgrade helper would pop a visible Windows Terminal window "+
			"(regression: previously used DETACHED_PROCESS)", flags)
	}
	if flags&detachedProcess != 0 {
		t.Fatalf("windowsHelperCreationFlags = %#x must NOT include DETACHED_PROCESS (0x8); "+
			"DETACHED_PROCESS + GUI-subsystem parent lets tasklist/ping allocate visible "+
			"consoles, and DETACHED_PROCESS is mutually exclusive with CREATE_NO_WINDOW at "+
			"the Win32 level", flags)
	}
}
