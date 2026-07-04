package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/coverage"
)

func TestParseGitDiff_AddedLines(t *testing.T) {
	t.Parallel()

	diff := []byte(`diff --git a/pkg/file.go b/pkg/file.go
index 1234567..abcdefg 100644
--- a/pkg/file.go
+++ b/pkg/file.go
@@ -10,3 +10,5 @@ func foo() {
 	unchangedLine1
+addedLineA
+addedLineB
 	unchangedLine2
@@ -20,2 +20,3 @@ func bar() {
-removedLine
+addedLineC
 	unchangedLine3
`)

	patch := parseGitDiff(diff)

	want := coverage.PatchLineSet{
		"pkg/file.go": {11: true, 12: true, 20: true},
	}
	if !reflect.DeepEqual(patch, want) {
		t.Fatalf("parseGitDiff() = %v, want %v", patch, want)
	}
}

func TestParseGitDiff_MultipleFiles(t *testing.T) {
	t.Parallel()

	diff := []byte(`diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -1,1 +1,2 @@
+newA
diff --git a/b/c.go b/b/c.go
--- a/b/c.go
+++ b/b/c.go
@@ -5,1 +5,1 @@
-new
+newer
`)

	patch := parseGitDiff(diff)

	want := coverage.PatchLineSet{
		"a.go":   {1: true},
		"b/c.go": {5: true},
	}
	if !reflect.DeepEqual(patch, want) {
		t.Fatalf("parseGitDiff() = %v, want %v", patch, want)
	}
}

func TestParseGitDiff_NoChanges(t *testing.T) {
	t.Parallel()

	patch := parseGitDiff([]byte(""))
	if len(patch) != 0 {
		t.Fatalf("parseGitDiff(empty) = %v, want empty", patch)
	}
}

func TestParseGitDiff_BackslashPaths(t *testing.T) {
	t.Parallel()

	// On Windows, git may emit backslash paths in the +++ header.
	diff := []byte("diff --git a/pkg\\file.go b/pkg\\file.go\r\n--- a/pkg\\file.go\r\n+++ b/pkg\\file.go\r\n@@ -1,1 +1,2 @@\r\n+added\r\n")

	patch := parseGitDiff(diff)

	want := coverage.PatchLineSet{
		"pkg/file.go": {1: true},
	}
	if !reflect.DeepEqual(patch, want) {
		t.Fatalf("parseGitDiff() = %v, want %v", patch, want)
	}
}

func TestParseHunkStart(t *testing.T) {
	t.Parallel()

	cases := []struct {
		line string
		want int
	}{
		{"@@ -10,3 +12,5 @@ func foo() {}", 12},
		{"@@ -10 +12 @@ func foo() {}", 12},
		{"@@ -10,3 +1,5 @@", 1},
		{"@@ -10,3 +1 @@", 1},
		{"no hunk here", 0},
		{"@@ -10,3 +abc @@", 0},
	}

	for _, tc := range cases {
		if got := parseHunkStart(tc.line); got != tc.want {
			t.Fatalf("parseHunkStart(%q) = %d, want %d", tc.line, got, tc.want)
		}
	}
}

func TestLoadCodecovIgnore(t *testing.T) {
	t.Parallel()

	t.Run("parses ignore list", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		content := "ignore:\n  - test/e2e/**\n  - web/**\n  - \"**/node_modules/**\"\n"
		if err := os.WriteFile(filepath.Join(dir, "codecov.yml"), []byte(content), 0o644); err != nil {
			t.Fatalf("write codecov.yml: %v", err)
		}

		got, err := loadCodecovIgnore(dir)
		if err != nil {
			t.Fatalf("loadCodecovIgnore() error = %v", err)
		}
		want := []string{"test/e2e/**", "web/**", "**/node_modules/**"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("loadCodecovIgnore() = %v, want %v", got, want)
		}
	})

	t.Run("missing file returns nil", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		got, err := loadCodecovIgnore(dir)
		if err != nil {
			t.Fatalf("loadCodecovIgnore() error = %v", err)
		}
		if got != nil {
			t.Fatalf("loadCodecovIgnore(missing) = %v, want nil", got)
		}
	})

	t.Run("no ignore key returns nil", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		content := "coverage:\n  status:\n    project:\n      default:\n        target: 80%\n"
		if err := os.WriteFile(filepath.Join(dir, "codecov.yml"), []byte(content), 0o644); err != nil {
			t.Fatalf("write codecov.yml: %v", err)
		}

		got, err := loadCodecovIgnore(dir)
		if err != nil {
			t.Fatalf("loadCodecovIgnore() error = %v", err)
		}
		if got != nil {
			t.Fatalf("loadCodecovIgnore(no ignore) = %v, want nil", got)
		}
	})
}

func TestReadModulePrefix(t *testing.T) {
	t.Parallel()

	t.Run("reads module path", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		content := "module github.com/javinizer/javinizer-go\n\ngo 1.24\n"
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(content), 0o644); err != nil {
			t.Fatalf("write go.mod: %v", err)
		}

		got, err := readModulePrefix(dir)
		if err != nil {
			t.Fatalf("readModulePrefix() error = %v", err)
		}
		if got != "github.com/javinizer/javinizer-go/" {
			t.Fatalf("readModulePrefix() = %q, want %q", got, "github.com/javinizer/javinizer-go/")
		}
	})

	t.Run("missing go.mod errors", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		_, err := readModulePrefix(dir)
		if err == nil {
			t.Fatal("readModulePrefix() should error on missing go.mod, got nil")
		}
	})

	t.Run("no module directive errors", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		content := "go 1.24\n"
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(content), 0o644); err != nil {
			t.Fatalf("write go.mod: %v", err)
		}

		_, err := readModulePrefix(dir)
		if err == nil {
			t.Fatal("readModulePrefix() should error on missing module directive, got nil")
		}
	})
}

func TestRunPatchCheck(t *testing.T) {
	t.Run("100% coverage passes", func(t *testing.T) {
		dir := t.TempDir()

		profileContent := `mode: count
github.com/javinizer/javinizer-go/pkg/file.go:1.1,3.1 1 1
`
		profilePath := filepath.Join(dir, "coverage.out")
		if err := os.WriteFile(profilePath, []byte(profileContent), 0o644); err != nil {
			t.Fatalf("write profile: %v", err)
		}

		goModPath := filepath.Join(dir, "go.mod")
		if err := os.WriteFile(goModPath, []byte("module github.com/javinizer/javinizer-go\n\ngo 1.24\n"), 0o644); err != nil {
			t.Fatalf("write go.mod: %v", err)
		}

		codecovYmlPath := filepath.Join(dir, "codecov.yml")
		if err := os.WriteFile(codecovYmlPath, []byte("ignore:\n  - test/e2e/**\n"), 0o644); err != nil {
			t.Fatalf("write codecov.yml: %v", err)
		}

		origGitDiff, origModulePrefix, origGetwd := gitDiff, modulePrefix, osGetwd
		t.Cleanup(func() {
			gitDiff, modulePrefix, osGetwd = origGitDiff, origModulePrefix, origGetwd
		})

		gitDiff = func(baseRef string) ([]byte, error) {
			if baseRef != "main" {
				t.Fatalf("baseRef = %q, want %q", baseRef, "main")
			}
			return []byte("diff --git a/pkg/file.go b/pkg/file.go\n+++ b/pkg/file.go\n@@ -1,1 +1,2 @@\n+added\n"), nil
		}
		modulePrefix = func(repoRoot string) (string, error) {
			return "github.com/javinizer/javinizer-go/", nil
		}
		osGetwd = func() (string, error) { return dir, nil }

		var stdout, stderr bytes.Buffer
		exitCode := runPatchCheck(profilePath, "main", 80, &stdout, &stderr)

		if exitCode != 0 {
			t.Fatalf("exitCode = %d, want 0; stderr=%s", exitCode, stderr.String())
		}
		if !strings.Contains(stdout.String(), "100.00%") {
			t.Fatalf("stdout should contain 100.00%%, got: %s", stdout.String())
		}
		if !strings.Contains(stdout.String(), "PASSED") {
			t.Fatalf("stdout should contain PASSED, got: %s", stdout.String())
		}
	})

	t.Run("0% coverage fails", func(t *testing.T) {
		dir := t.TempDir()

		profileContent := `mode: count
github.com/javinizer/javinizer-go/pkg/file.go:1.1,3.1 1 0
`
		profilePath := filepath.Join(dir, "coverage.out")
		if err := os.WriteFile(profilePath, []byte(profileContent), 0o644); err != nil {
			t.Fatalf("write profile: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/javinizer/javinizer-go\n\ngo 1.24\n"), 0o644); err != nil {
			t.Fatalf("write go.mod: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "codecov.yml"), []byte("ignore: []\n"), 0o644); err != nil {
			t.Fatalf("write codecov.yml: %v", err)
		}

		origGitDiff, origModulePrefix, origGetwd := gitDiff, modulePrefix, osGetwd
		t.Cleanup(func() {
			gitDiff, modulePrefix, osGetwd = origGitDiff, origModulePrefix, origGetwd
		})

		gitDiff = func(baseRef string) ([]byte, error) {
			return []byte("diff --git a/pkg/file.go b/pkg/file.go\n+++ b/pkg/file.go\n@@ -1,1 +1,2 @@\n+added\n"), nil
		}
		modulePrefix = func(repoRoot string) (string, error) {
			return "github.com/javinizer/javinizer-go/", nil
		}
		osGetwd = func() (string, error) { return dir, nil }

		var stdout, stderr bytes.Buffer
		exitCode := runPatchCheck(profilePath, "main", 80, &stdout, &stderr)

		if exitCode != 1 {
			t.Fatalf("exitCode = %d, want 1", exitCode)
		}
		if !strings.Contains(stdout.String(), "FAILED") {
			t.Fatalf("stdout should contain FAILED, got: %s", stdout.String())
		}
	})

	t.Run("no changes passes with 100%", func(t *testing.T) {
		dir := t.TempDir()

		profilePath := filepath.Join(dir, "coverage.out")
		if err := os.WriteFile(profilePath, []byte("mode: count\n"), 0o644); err != nil {
			t.Fatalf("write profile: %v", err)
		}

		origGitDiff, origGetwd := gitDiff, osGetwd
		t.Cleanup(func() { gitDiff, osGetwd = origGitDiff, origGetwd })

		gitDiff = func(baseRef string) ([]byte, error) {
			return []byte(""), nil
		}
		osGetwd = func() (string, error) { return dir, nil }

		var stdout, stderr bytes.Buffer
		exitCode := runPatchCheck(profilePath, "main", 80, &stdout, &stderr)

		if exitCode != 0 {
			t.Fatalf("exitCode = %d, want 0; stderr=%s", exitCode, stderr.String())
		}
		if !strings.Contains(stdout.String(), "nothing to cover") {
			t.Fatalf("stdout should mention 'nothing to cover', got: %s", stdout.String())
		}
	})

	t.Run("git diff error returns 2", func(t *testing.T) {
		dir := t.TempDir()

		profilePath := filepath.Join(dir, "coverage.out")
		if err := os.WriteFile(profilePath, []byte("mode: count\n"), 0o644); err != nil {
			t.Fatalf("write profile: %v", err)
		}

		origGitDiff, origGetwd := gitDiff, osGetwd
		t.Cleanup(func() { gitDiff, osGetwd = origGitDiff, origGetwd })

		gitDiff = func(baseRef string) ([]byte, error) {
			return nil, errors.New("simulated git failure")
		}
		osGetwd = func() (string, error) { return dir, nil }

		var stdout, stderr bytes.Buffer
		exitCode := runPatchCheck(profilePath, "main", 80, &stdout, &stderr)

		if exitCode != 2 {
			t.Fatalf("exitCode = %d, want 2", exitCode)
		}
		if !strings.Contains(stderr.String(), "simulated git failure") {
			t.Fatalf("stderr should contain git error, got: %s", stderr.String())
		}
	})
}

func TestRunWithAnalyze_PatchFlag(t *testing.T) {
	t.Run("--patch delegates to runPatchCheck", func(t *testing.T) {
		dir := t.TempDir()
		profilePath := filepath.Join(dir, "coverage.out")
		if err := os.WriteFile(profilePath, []byte("mode: count\n"), 0o644); err != nil {
			t.Fatalf("write profile: %v", err)
		}

		origGitDiff, origGetwd := gitDiff, osGetwd
		t.Cleanup(func() { gitDiff, osGetwd = origGitDiff, origGetwd })

		gitDiff = func(baseRef string) ([]byte, error) { return []byte(""), nil }
		osGetwd = func() (string, error) { return dir, nil }

		var stdout, stderr bytes.Buffer
		exitCode := runWithAnalyze(
			[]string{"--patch", "--profile", profilePath},
			&stdout, &stderr,
			func(string) (coverage.Summary, error) {
				t.Fatal("analyze should not be called in patch mode")
				return coverage.Summary{}, nil
			},
		)

		if exitCode != 0 {
			t.Fatalf("exitCode = %d, want 0; stderr=%s", exitCode, stderr.String())
		}
		if !strings.Contains(stdout.String(), "Patch Coverage Report") {
			t.Fatalf("stdout should contain Patch Coverage Report, got: %s", stdout.String())
		}
	})

	t.Run("--patch --base custom", func(t *testing.T) {
		dir := t.TempDir()
		profilePath := filepath.Join(dir, "coverage.out")
		if err := os.WriteFile(profilePath, []byte("mode: count\n"), 0o644); err != nil {
			t.Fatalf("write profile: %v", err)
		}

		origGitDiff, origGetwd := gitDiff, osGetwd
		t.Cleanup(func() { gitDiff, osGetwd = origGitDiff, origGetwd })

		var capturedBase string
		gitDiff = func(baseRef string) ([]byte, error) {
			capturedBase = baseRef
			return []byte(""), nil
		}
		osGetwd = func() (string, error) { return dir, nil }

		var stdout, stderr bytes.Buffer
		exitCode := runWithAnalyze(
			[]string{"--patch", "--profile", profilePath, "--base", "develop"},
			&stdout, &stderr,
			func(string) (coverage.Summary, error) { return coverage.Summary{}, nil },
		)

		if exitCode != 0 {
			t.Fatalf("exitCode = %d, want 0", exitCode)
		}
		if capturedBase != "develop" {
			t.Fatalf("base ref passed to gitDiff = %q, want %q", capturedBase, "develop")
		}
	})
}

func TestRunPatchCheck_FiltersFilesNotInProfile(t *testing.T) {
	dir := t.TempDir()

	// Profile covers pkg/covered.go only; pkg/excluded.go is in a package
	// excluded from `go test -coverpkg` (no profile entries).
	profileContent := `mode: count
github.com/javinizer/javinizer-go/pkg/covered.go:1.1,3.1 1 1
`
	profilePath := filepath.Join(dir, "coverage.out")
	if err := os.WriteFile(profilePath, []byte(profileContent), 0o644); err != nil {
		t.Fatalf("write profile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/javinizer/javinizer-go\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "codecov.yml"), []byte("ignore: []\n"), 0o644); err != nil {
		t.Fatalf("write codecov.yml: %v", err)
	}

	origGitDiff, origModulePrefix, origGetwd := gitDiff, modulePrefix, osGetwd
	t.Cleanup(func() { gitDiff, modulePrefix, osGetwd = origGitDiff, origModulePrefix, origGetwd })

	// Diff touches both files, but only covered.go is in the profile.
	gitDiff = func(baseRef string) ([]byte, error) {
		return []byte("diff --git a/pkg/covered.go b/pkg/covered.go\n+++ b/pkg/covered.go\n@@ -1,1 +1,2 @@\n+added\n" +
			"diff --git a/pkg/excluded.go b/pkg/excluded.go\n+++ b/pkg/excluded.go\n@@ -1,1 +1,2 @@\n+added\n"), nil
	}
	modulePrefix = func(repoRoot string) (string, error) {
		return "github.com/javinizer/javinizer-go/", nil
	}
	osGetwd = func() (string, error) { return dir, nil }

	var stdout, stderr bytes.Buffer
	exitCode := runPatchCheck(profilePath, "main", 80, &stdout, &stderr)

	if exitCode != 0 {
		t.Fatalf("exitCode = %d, want 0 (excluded.go must not count as misses); stderr=%s", exitCode, stderr.String())
	}
	// covered.go L1 is hit (count=1), excluded.go is filtered out → 100%.
	if !strings.Contains(stdout.String(), "100.00%") {
		t.Fatalf("stdout should contain 100.00%% (excluded file filtered out), got: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "PASSED") {
		t.Fatalf("stdout should contain PASSED, got: %s", stdout.String())
	}
}

func TestRunGitDiff_RealGitRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping git subprocess test in short mode")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	run("config", "commit.gpgsign", "false")

	if err := os.WriteFile(filepath.Join(dir, "file.go"), []byte("package p\n"), 0o644); err != nil {
		t.Fatalf("write base file: %v", err)
	}
	run("add", "-A")
	run("commit", "-m", "base")

	if err := os.WriteFile(filepath.Join(dir, "file.go"), []byte("package p\n\nfunc f() {}\n"), 0o644); err != nil {
		t.Fatalf("write head file: %v", err)
	}
	run("add", "-A")
	run("commit", "-m", "head")

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	out, err := runGitDiff("HEAD~1")
	if err != nil {
		t.Fatalf("runGitDiff: %v", err)
	}
	if !bytes.Contains(out, []byte("func f()")) {
		t.Fatalf("runGitDiff output missing added line, got: %s", out)
	}
}

func TestProfileFileSet(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	profileContent := `mode: count
github.com/javinizer/javinizer-go/pkg/a.go:1.1,3.1 1 1
github.com/javinizer/javinizer-go/pkg/b.go:5.1,7.1 1 0
`
	profilePath := filepath.Join(dir, "coverage.out")
	if err := os.WriteFile(profilePath, []byte(profileContent), 0o644); err != nil {
		t.Fatalf("write profile: %v", err)
	}

	files, err := profileFileSet(profilePath, "github.com/javinizer/javinizer-go/")
	if err != nil {
		t.Fatalf("profileFileSet() error = %v", err)
	}

	if !files["pkg/a.go"] || !files["pkg/b.go"] {
		t.Fatalf("expected pkg/a.go and pkg/b.go in profileFileSet, got %v", files)
	}
	if files["pkg/c.go"] {
		t.Fatalf("pkg/c.go should not be in profileFileSet")
	}
}
