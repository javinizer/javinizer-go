//go:build linux

package updater

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRenderLinuxHelperScript(t *testing.T) {
	const pid = 12345
	const target = "/home/user/Applications/Javinizer-linux-x86_64.AppImage"
	const staged = "/home/user/Applications/.javinizer-bundle-upgrade-42.tmp"

	cases := []struct {
		name            string
		preserveExtract bool
	}{
		{"preserve_extract", true},
		{"no_preserve_extract", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			script := renderLinuxHelperScript(pid, target, staged, c.preserveExtract)

			mustContain := []string{
				strconv.Itoa(pid),
				target,
				staged,
				"kill -0",
				"mv -f",
				"chmod +x",
				"nohup",
			}
			for _, want := range mustContain {
				if !strings.Contains(script, want) {
					t.Errorf("script missing %q\n--- script ---\n%s", want, script)
				}
			}

			// The wait loop must be capped, and on timeout must exit (not break
			// and proceed to swap a still-running app's bundle).
			if !strings.Contains(script, "150") {
				t.Errorf("script missing wait-loop iteration cap\n--- script ---\n%s", script)
			}
			if !strings.Contains(script, "exit 1") {
				t.Errorf("script must exit on wait-loop timeout (not break and proceed)\n--- script ---\n%s", script)
			}

			// preserveExtract toggles the APPIMAGE_EXTRACT_AND_RUN export line.
			hasExport := strings.Contains(script, "APPIMAGE_EXTRACT_AND_RUN")
			if c.preserveExtract && !hasExport {
				t.Errorf("preserveExtract=true but script lacks APPIMAGE_EXTRACT_AND_RUN export\n--- script ---\n%s", script)
			}
			if !c.preserveExtract && hasExport {
				t.Errorf("preserveExtract=false but script contains APPIMAGE_EXTRACT_AND_RUN export\n--- script ---\n%s", script)
			}

			// Ordering: wait -> mv -> chmod -> (optional export) -> nohup relaunch.
			idxKill := strings.Index(script, "kill -0")
			idxMv := strings.Index(script, "mv -f")
			idxChmod := strings.Index(script, "chmod +x")
			idxNohup := strings.Index(script, "nohup")
			if !(idxKill < idxMv && idxMv < idxChmod && idxChmod < idxNohup) {
				t.Errorf("unexpected ordering: kill=%d mv=%d chmod=%d nohup=%d\n--- script ---\n%s", idxKill, idxMv, idxChmod, idxNohup, script)
			}

			// The relaunch must detach (background) and not block the helper.
			if !strings.Contains(script, "&\n") && !strings.HasSuffix(strings.TrimSpace(script), "&") {
				t.Errorf("relaunch is not backgrounded\n--- script ---\n%s", script)
			}
		})
	}
}

func TestResolveAppImageTarget(t *testing.T) {
	makeExecFile := func(t *testing.T, name string) string {
		t.Helper()
		dir := t.TempDir()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatalf("write exec file: %v", err)
		}
		return p
	}

	// t.TempDir() may live under a symlinked path (e.g. /var -> /private/var),
	// so compare against the EvalSymlinks-resolved form.
	resolved := func(t *testing.T, p string) string {
		t.Helper()
		r, err := filepath.EvalSymlinks(p)
		if err != nil {
			t.Fatalf("EvalSymlinks %q: %v", p, err)
		}
		return r
	}

	cases := []struct {
		name        string
		envAppImage string
		exePath     func(t *testing.T) string
		wantEnv     bool
		wantErr     bool
	}{
		{
			name:        "env_set_valid_uses_env",
			envAppImage: makeExecFile(t, "env.AppImage"),
			exePath:     func(t *testing.T) string { return "/nonexistent/exe" },
			wantEnv:     true,
		},
		{
			name:        "env_empty_falls_back_to_exe",
			envAppImage: "",
			exePath:     func(t *testing.T) string { return makeExecFile(t, "exe") },
		},
		{
			name:        "env_set_missing_file_falls_back_to_exe",
			envAppImage: filepath.Join(t.TempDir(), "missing.AppImage"),
			exePath:     func(t *testing.T) string { return makeExecFile(t, "exe") },
		},
		{
			name:        "both_unresolvable_errors",
			envAppImage: "/nonexistent/env.AppImage",
			exePath:     func(t *testing.T) string { return "/nonexistent/exe" },
			wantErr:     true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			exePath := c.exePath(t)
			got, err := resolveAppImageTarget(c.envAppImage, exePath)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			want := c.envAppImage
			if !c.wantEnv {
				want = resolved(t, exePath)
			}
			if got != want {
				t.Errorf("target = %q, want %q", got, want)
			}
		})
	}
}
