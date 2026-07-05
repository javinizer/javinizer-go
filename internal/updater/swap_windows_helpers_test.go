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
