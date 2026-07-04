package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/coverage"
	"gopkg.in/yaml.v3"
)

// gitAddedLines returns the set of added/modified line numbers per repo-relative
// file in the diff from baseRef...HEAD. It mirrors codecov's patch detection:
// only lines introduced by the current change count, not pre-existing context.
//
// The baseRef is typically "main" (or "origin/main"). It is resolved via
// `git merge-base` so a feature branch that has fallen behind main still diffs
// against the fork point (not the tip of main, which would miss lines added on
// main since the branch diverged — those aren't part of this PR).
type gitDiffFunc func(baseRef string) ([]byte, error)

var gitDiff gitDiffFunc = runGitDiff

const gitDiffTimeout = 30 * time.Second

func runGitDiff(baseRef string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitDiffTimeout)
	defer cancel()
	mergeBase, err := exec.CommandContext(ctx, "git", "merge-base", baseRef, "HEAD").Output() //nolint:gosec // baseRef is a git ref from the user/Makefile, not untrusted input
	if err != nil {
		return nil, fmt.Errorf("git merge-base %s HEAD: %w", baseRef, err)
	}
	base := strings.TrimSpace(string(mergeBase))
	if base == "" {
		return nil, fmt.Errorf("empty merge-base for %s", baseRef)
	}

	out, err := exec.CommandContext(ctx, "git", "diff", "--unified=0", base+"...HEAD").Output() //nolint:gosec // base is a validated git SHA from merge-base output
	if err != nil {
		return nil, fmt.Errorf("git diff %s...HEAD: %w", base, err)
	}
	return out, nil
}

// profileFileSet reads a coverage profile and returns the set of repo-relative
// file paths that have at least one coverage block. Used to filter patch lines
// so files in packages excluded from `go test -coverpkg` don't show as false
// misses (they have no profile entries).
func profileFileSet(profilePath, modulePrefix string) (map[string]bool, error) {
	data, err := os.ReadFile(profilePath)
	if err != nil {
		return nil, fmt.Errorf("read profile: %w", err)
	}
	files := make(map[string]bool)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		colon := strings.LastIndex(line, ":")
		if colon == -1 {
			continue
		}
		file := strings.TrimPrefix(line[:colon], modulePrefix)
		files[file] = true
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan profile: %w", err)
	}
	return files, nil
}

// parseGitDiff parses `git diff --unified=0` output and returns the added-line
// set per file. Renames/deletes are skipped; only added or modified lines count.
// Paths are normalized to forward slashes (repo-relative).
func parseGitDiff(diff []byte) coverage.PatchLineSet {
	patch := coverage.PatchLineSet{}
	scanner := bufio.NewScanner(bytes.NewReader(diff))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	currentFile := ""
	currentLine := 0

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "+++") {
			if rest := strings.TrimPrefix(line, "+++ "); strings.HasPrefix(rest, "b/") {
				currentFile = strings.TrimPrefix(rest, "b/")
				currentFile = strings.ReplaceAll(currentFile, "\\", "/")
				// Only Go source files have coverage profile entries; non-Go
				// changes (YAML, Markdown, Makefile) would otherwise be seeded
				// as misses and skew the patch coverage toward 0%%. Test files
				// (_test.go) are also excluded — `go test -cover` does not emit
				// coverage blocks for test files themselves, and codecov's patch
				// coverage likewise excludes them.
				if !strings.HasSuffix(currentFile, ".go") || strings.HasSuffix(currentFile, "_test.go") {
					currentFile = ""
				}
			} else {
				currentFile = ""
			}
			continue
		}

		if strings.HasPrefix(line, "@@") {
			currentLine = parseHunkStart(line)
			continue
		}

		if currentFile == "" || currentLine == 0 {
			continue
		}

		switch {
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			if patch[currentFile] == nil {
				patch[currentFile] = map[int]bool{}
			}
			patch[currentFile][currentLine] = true
			currentLine++
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			// removal: new-file line counter does not advance
		default:
			currentLine++
		}
	}

	return patch
}

// parseHunkStart extracts the new-file start line number from a unified diff
// hunk header: "@@ -a,b +c,d @@". Returns c (the start line in the new file),
// or 0 if the header cannot be parsed.
func parseHunkStart(line string) int {
	idx := strings.Index(line, "+")
	if idx == -1 {
		return 0
	}
	rest := line[idx+1:]
	end := strings.IndexAny(rest, ", ")
	if end == -1 {
		end = len(rest)
	}
	n := 0
	for i := 0; i < end; i++ {
		c := rest[i]
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// codecovYAML is the subset of codecov.yml we read. Only the ignore list
// affects patch coverage; the rest (status targets, comment layout) is
// codecov-server-side config irrelevant to the local check.
type codecovYAML struct {
	Ignore []string `yaml:"ignore"`
}

// loadCodecovIgnore reads the ignore list from codecov.yml at repoRoot. Returns
// an empty slice (not an error) if the file is absent — no ignores configured.
func loadCodecovIgnore(repoRoot string) ([]string, error) {
	path := repoRoot + "/codecov.yml"
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var cfg codecovYAML
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg.Ignore, nil
}

// modulePrefix reads the Go module path from go.mod at repoRoot (the first
// `module` directive). Coverage-profile file paths are prefixed with it.
type modulePrefixFunc func(repoRoot string) (string, error)

var modulePrefix modulePrefixFunc = readModulePrefix

// osGetwd is a seam for os.Getwd so tests can inject a temp repoRoot without
// chdir-ing the running process.
var osGetwd = os.Getwd

func readModulePrefix(repoRoot string) (string, error) {
	data, err := os.ReadFile(repoRoot + "/go.mod")
	if err != nil {
		return "", fmt.Errorf("read go.mod: %w", err)
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module ")) + "/", nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan go.mod: %w", err)
	}
	return "", fmt.Errorf("no module directive in go.mod")
}

// runPatchCheck computes and enforces patch coverage. It mirrors codecov/patch:
// only lines added/modified in the diff against baseRef count, paths in
// codecov.yml's ignore list are excluded, and the result must meet minCoverage
// (default 80, matching codecov.yml's patch target).
func runPatchCheck(profilePath, baseRef string, minCoverage float64, stdout, stderr io.Writer) int {
	repoRoot, err := osGetwd()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Error: getwd: %v\n", err)
		return 2
	}

	diff, err := gitDiff(baseRef)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
		return 2
	}

	patchLines := parseGitDiff(diff)
	if len(patchLines) == 0 {
		_, _ = fmt.Fprintln(stdout, "==========================================")
		_, _ = fmt.Fprintln(stdout, "Patch Coverage Report")
		_, _ = fmt.Fprintln(stdout, "==========================================")
		_, _ = fmt.Fprintf(stdout, "Base Ref:          %s\n", baseRef)
		_, _ = fmt.Fprintln(stdout, "Added Lines:       0 (no changes vs base)")
		_, _ = fmt.Fprintln(stdout, "Patch Coverage:    100.00% (nothing to cover)")
		_, _ = fmt.Fprintln(stdout, "Coverage check PASSED")
		return 0
	}

	ignoreGlobs, err := loadCodecovIgnore(repoRoot)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
		return 2
	}

	modPrefix, err := modulePrefix(repoRoot)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
		return 2
	}

	// Filter patchLines to files that appear in the coverage profile. Files in
	// packages excluded from `go test -coverpkg` (e.g. this repo excludes
	// cmd/coveragecheck + internal/coverage as test utilities) have no profile
	// entries; without this filter, the library would seed every line of an
	// excluded package as a miss and skew patch coverage toward 0%%. Codecov
	// achieves the same effect via its ignore list + the packages it actually
	// instruments.
	profileFiles, err := profileFileSet(profilePath, modPrefix)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
		return 2
	}
	for f := range patchLines {
		if !profileFiles[f] {
			delete(patchLines, f)
		}
	}

	summary, err := coverage.AnalyzeProfilePatch(profilePath, coverage.PatchOptions{
		PatchLines:   patchLines,
		IgnoreGlobs:  ignoreGlobs,
		ModulePrefix: modPrefix,
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
		return 2
	}

	_, _ = fmt.Fprintln(stdout, "==========================================")
	_, _ = fmt.Fprintln(stdout, "Patch Coverage Report")
	_, _ = fmt.Fprintln(stdout, "==========================================")
	_, _ = fmt.Fprintf(stdout, "Base Ref:          %s\n", baseRef)
	_, _ = fmt.Fprintf(stdout, "Coverage Profile: %s\n", profilePath)
	_, _ = fmt.Fprintf(stdout, "Changed Files:     %d\n", len(summary.Files))
	_, _ = fmt.Fprintf(stdout, "Patch Coverage:    %.2f%% (%d hit, %d partial, %d miss, %d total)\n",
		summary.Percent, summary.Hit, summary.Partial, summary.Miss, summary.Total)
	if len(ignoreGlobs) > 0 {
		_, _ = fmt.Fprintf(stdout, "Ignored Globs:     %s\n", strings.Join(ignoreGlobs, ", "))
	}
	_, _ = fmt.Fprintf(stdout, "Required Minimum:  %.2f%%\n", minCoverage)
	_, _ = fmt.Fprintln(stdout, "==========================================")

	if summary.Percent+1e-9 < minCoverage {
		_, _ = fmt.Fprintln(stdout, "Patch coverage check FAILED")
		if summary.Miss > 0 {
			_, _ = fmt.Fprintln(stdout, "")
			_, _ = fmt.Fprintln(stdout, "Uncovered changed lines remain. Run with -v for details,")
			_, _ = fmt.Fprintln(stdout, "or open coverage.html (make coverage-html) and filter to your changed files.")
		}
		return 1
	}

	_, _ = fmt.Fprintln(stdout, "Patch coverage check PASSED")
	return 0
}
