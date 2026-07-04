package coverage

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

// PatchSummary models Codecov-style patch coverage: hit/miss/partial counts
// restricted to the lines added or modified in the current change (the diff
// against the base ref), excluding ignored paths.
type PatchSummary struct {
	Total   int
	Hit     int
	Partial int
	Miss    int
	Percent float64
	Files   []string
}

// PatchLineSet maps a repo-relative file path to the set of line numbers that
// were added or modified in the diff. Callers build this from `git diff`.
type PatchLineSet map[string]map[int]bool

// PatchOptions configures patch-coverage analysis.
type PatchOptions struct {
	// PatchLines is the added/modified line set (repo-relative paths).
	PatchLines PatchLineSet
	// IgnoreGlobs are codecov.yml-style path patterns to exclude from both
	// the patch line set and the coverage blocks (e.g. "test/e2e/**").
	IgnoreGlobs []string
	// ModulePrefix is the Go module path prefix stripped from coverage-profile
	// file paths to make them repo-relative (e.g. "github.com/javinizer/javinizer-go/").
	ModulePrefix string
}

// AnalyzeProfilePatch reads a Go cover profile and returns Codecov-style patch
// coverage for the lines in opts.PatchLines, excluding ignored paths.
func AnalyzeProfilePatch(profilePath string, opts PatchOptions) (PatchSummary, error) {
	file, err := os.Open(profilePath)
	if err != nil {
		return PatchSummary{}, fmt.Errorf("open profile: %w", err)
	}
	defer func() { _ = file.Close() }()

	return analyzePatch(file, opts)
}

// analyzePatch parses a Go cover profile from r and computes patch coverage.
func analyzePatch(r io.Reader, opts PatchOptions) (PatchSummary, error) {
	ignoreRes := make([]*regexp.Regexp, 0, len(opts.IgnoreGlobs))
	for _, g := range opts.IgnoreGlobs {
		re, err := compileIgnoreGlob(g)
		if err != nil {
			return PatchSummary{}, fmt.Errorf("invalid ignore glob %q: %w", g, err)
		}
		ignoreRes = append(ignoreRes, re)
	}

	isIgnored := func(repoPath string) bool {
		for _, re := range ignoreRes {
			if re.MatchString(repoPath) {
				return true
			}
		}
		return false
	}

	toRepoPath := func(profilePath string) string {
		if opts.ModulePrefix != "" {
			return strings.TrimPrefix(profilePath, opts.ModulePrefix)
		}
		return profilePath
	}

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return PatchSummary{}, fmt.Errorf("read profile header: %w", err)
		}
		return PatchSummary{}, fmt.Errorf("empty coverage profile")
	}

	header := strings.TrimSpace(scanner.Text())
	if !strings.HasPrefix(header, "mode:") {
		return PatchSummary{}, fmt.Errorf("invalid coverage profile header: %q", header)
	}

	merged := make(map[blockKey]block)
	for lineNo := 2; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parsed, key, err := parseBlock(line)
		if err != nil {
			return PatchSummary{}, fmt.Errorf("parse profile line %d: %w", lineNo, err)
		}

		if existing, ok := merged[key]; ok {
			existing.count += parsed.count
			merged[key] = existing
			continue
		}
		merged[key] = parsed
	}
	if err := scanner.Err(); err != nil {
		return PatchSummary{}, fmt.Errorf("read coverage profile: %w", err)
	}

	fileLines := make(map[string]map[int]*lineState)
	changedFiles := make(map[string]bool)

	// Seed every patch line as a miss first. A changed file that appears in
	// the coverage profile will overwrite these with covered/partial states
	// below; a changed file with NO profile entry keeps its misses. Without
	// this, percentage() sees Total==0 and returns 100%, letting a
	// fully-untested diff pass the patch gate (the bug CodeRabbit flagged).
	//
	// The CLI layer (cmd/coveragecheck) is responsible for filtering out
	// files in packages excluded from `go test -coverpkg` before calling
	// this function, so they don't show up as false misses here.
	for repoPath, lines := range opts.PatchLines {
		if isIgnored(repoPath) {
			continue
		}
		changedFiles[repoPath] = true
		state := make(map[int]*lineState, len(lines))
		for line := range lines {
			state[line] = &lineState{}
		}
		fileLines[repoPath] = state
	}

	for _, entry := range merged {
		repoPath := toRepoPath(entry.file)
		if isIgnored(repoPath) {
			continue
		}

		patchLines, hasPatch := opts.PatchLines[repoPath]
		if !hasPatch {
			continue
		}

		changedFiles[repoPath] = true

		lines := fileLines[repoPath]
		if lines == nil {
			lines = make(map[int]*lineState)
			fileLines[repoPath] = lines
		}

		for line := entry.startLine; line <= entry.endLine; line++ {
			if !patchLines[line] {
				continue
			}

			state := lines[line]
			if state == nil {
				state = &lineState{}
				lines[line] = state
			}

			if entry.count > 0 {
				state.hasCovered = true
			} else {
				state.hasUncovered = true
			}
		}
	}

	var summary PatchSummary
	for _, lines := range fileLines {
		for _, state := range lines {
			summary.Total++
			switch {
			case state.hasCovered && state.hasUncovered:
				summary.Partial++
			case state.hasCovered:
				summary.Hit++
			default:
				summary.Miss++
			}
		}
	}

	summary.Percent = percentage(summary.Hit, summary.Total)
	for f := range changedFiles {
		summary.Files = append(summary.Files, f)
	}

	return summary, nil
}

// compileIgnoreGlob converts a codecov.yml-style path glob into a compiled
// regexp. Supported syntax:
//   - ** : matches any sequence of characters including path separators
//   - *  : matches a single path segment (no /)
//   - ?  : matches a single non-separator character
//
// All other characters are treated as literals (regex special chars escaped).
// Trailing /** is handled so "foo/**" matches "foo/bar" but not "foo" itself.
func compileIgnoreGlob(pattern string) (*regexp.Regexp, error) {
	pattern = strings.TrimPrefix(pattern, "./")
	var b strings.Builder
	b.WriteString("^")
	i := 0
	for i < len(pattern) {
		if i+1 < len(pattern) && pattern[i] == '*' && pattern[i+1] == '*' {
			b.WriteString(".*")
			i += 2
			if i < len(pattern) && pattern[i] == '/' {
				i++
			}
		} else if pattern[i] == '*' {
			b.WriteString("[^/]*")
			i++
		} else if pattern[i] == '?' {
			b.WriteString("[^/]")
			i++
		} else if strings.IndexByte(`\.+()|[]{}^$`, pattern[i]) >= 0 {
			b.WriteByte('\\')
			b.WriteByte(pattern[i])
			i++
		} else {
			b.WriteByte(pattern[i])
			i++
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}
