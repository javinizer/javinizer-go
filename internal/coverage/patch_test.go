package coverage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testModulePrefix = "github.com/javinizer/javinizer-go/"

func patchProfile(t *testing.T, content string) *os.File {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "patch-*.out")
	if err != nil {
		t.Fatalf("create temp profile: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		t.Fatalf("write temp profile: %v", err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		_ = f.Close()
		t.Fatalf("seek temp profile: %v", err)
	}
	return f
}

func TestAnalyzePatch_HitMissPartial(t *testing.T) {
	t.Parallel()

	profile := strings.NewReader(`mode: count
github.com/javinizer/javinizer-go/internal/desktop/paths.go:20.1,22.1 2 1
github.com/javinizer/javinizer-go/internal/desktop/paths.go:23.1,25.1 2 0
github.com/javinizer/javinizer-go/internal/desktop/paths.go:26.1,28.1 2 1
github.com/javinizer/javinizer-go/internal/desktop/paths.go:27.1,29.1 1 0
github.com/javinizer/javinizer-go/internal/desktop/paths.go:30.1,32.1 2 0
github.com/javinizer/javinizer-go/internal/desktop/server.go:10.1,12.1 1 1
`)

	patchLines := PatchLineSet{
		"internal/desktop/paths.go":  {20: true, 21: true, 23: true, 24: true, 26: true, 27: true, 30: true, 31: true},
		"internal/desktop/server.go": {10: true, 11: true},
	}

	summary, err := analyzePatch(profile, PatchOptions{
		PatchLines:   patchLines,
		ModulePrefix: testModulePrefix,
	})
	if err != nil {
		t.Fatalf("analyzePatch() error = %v", err)
	}

	if summary.Total != 10 {
		t.Fatalf("Total = %d, want 10 (8 paths + 2 server)", summary.Total)
	}
	if summary.Hit != 5 {
		t.Fatalf("Hit = %d, want 5 (L20-21 hit, L26 hit, L10-11 hit)", summary.Hit)
	}
	if summary.Miss != 4 {
		t.Fatalf("Miss = %d, want 4 (L23-24 miss, L30-31 miss)", summary.Miss)
	}
	if summary.Partial != 1 {
		t.Fatalf("Partial = %d, want 1 (L27 covered by block3 hit AND block4 miss)", summary.Partial)
	}
	if got := summary.Percent; got < 49.9 || got > 50.1 {
		t.Fatalf("Percent = %.2f, want about 50.0 (5 hit / 10 total)", got)
	}
}

func TestAnalyzePatch_OnlyCountsAddedLines(t *testing.T) {
	t.Parallel()

	profile := strings.NewReader(`mode: count
github.com/javinizer/javinizer-go/pkg/file.go:10.1,20.1 5 1
github.com/javinizer/javinizer-go/pkg/file.go:15.1,17.1 1 0
`)

	patchLines := PatchLineSet{
		"pkg/file.go": {15: true, 16: true},
	}

	summary, err := analyzePatch(profile, PatchOptions{
		PatchLines:   patchLines,
		ModulePrefix: testModulePrefix,
	})
	if err != nil {
		t.Fatalf("analyzePatch() error = %v", err)
	}

	if summary.Total != 2 {
		t.Fatalf("Total = %d, want 2 (only L15-16 are in the patch, L10-14/L17-20 are unchanged)", summary.Total)
	}
	if summary.Partial != 2 {
		t.Fatalf("Partial = %d, want 2 (L15-16 each hit by both a covered and uncovered DIFFERENT block)", summary.Partial)
	}
}

func TestAnalyzePatch_IgnoresFilesOutsidePatch(t *testing.T) {
	t.Parallel()

	profile := strings.NewReader(`mode: count
github.com/javinizer/javinizer-go/pkg/changed.go:5.1,7.1 1 0
github.com/javinizer/javinizer-go/pkg/unchanged.go:5.1,7.1 1 0
`)

	patchLines := PatchLineSet{
		"pkg/changed.go": {5: true, 6: true},
	}

	summary, err := analyzePatch(profile, PatchOptions{
		PatchLines:   patchLines,
		ModulePrefix: testModulePrefix,
	})
	if err != nil {
		t.Fatalf("analyzePatch() error = %v", err)
	}

	if summary.Total != 2 {
		t.Fatalf("Total = %d, want 2 (unchanged.go must not count)", summary.Total)
	}
	if summary.Miss != 2 {
		t.Fatalf("Miss = %d, want 2", summary.Miss)
	}
	if len(summary.Files) != 1 || summary.Files[0] != "pkg/changed.go" {
		t.Fatalf("Files = %v, want [pkg/changed.go]", summary.Files)
	}
}

func TestAnalyzePatch_IgnoreGlobs(t *testing.T) {
	t.Parallel()

	profile := strings.NewReader(`mode: count
github.com/javinizer/javinizer-go/test/e2e/cli/run.go:5.1,7.1 1 0
github.com/javinizer/javinizer-go/internal/desktop/app.go:5.1,7.1 1 0
github.com/javinizer/javinizer-go/internal/desktop/paths.go:5.1,7.1 1 1
github.com/javinizer/javinizer-go/web/dist/index.html:5.1,7.1 1 0
github.com/javinizer/javinizer-go/vendor/node_modules/pkg/lib.go:5.1,7.1 1 0
`)

	patchLines := PatchLineSet{
		"test/e2e/cli/run.go":            {5: true, 6: true},
		"internal/desktop/app.go":        {5: true, 6: true},
		"internal/desktop/paths.go":      {5: true, 6: true},
		"web/dist/index.html":            {5: true, 6: true},
		"vendor/node_modules/pkg/lib.go": {5: true, 6: true},
	}

	summary, err := analyzePatch(profile, PatchOptions{
		PatchLines: patchLines,
		IgnoreGlobs: []string{
			"test/e2e/**",
			"internal/desktop/app.go",
			"docs/swagger/**",
			"web/**",
			"**/node_modules/**",
		},
		ModulePrefix: testModulePrefix,
	})
	if err != nil {
		t.Fatalf("analyzePatch() error = %v", err)
	}

	if summary.Total != 2 {
		t.Fatalf("Total = %d, want 2 (only paths.go should survive all ignores)", summary.Total)
	}
	if summary.Hit != 2 {
		t.Fatalf("Hit = %d, want 2", summary.Hit)
	}
}

func TestAnalyzePatch_EmptyPatch(t *testing.T) {
	t.Parallel()

	profile := strings.NewReader(`mode: count
github.com/javinizer/javinizer-go/pkg/file.go:5.1,7.1 1 1
`)

	summary, err := analyzePatch(profile, PatchOptions{
		PatchLines:   PatchLineSet{},
		ModulePrefix: testModulePrefix,
	})
	if err != nil {
		t.Fatalf("analyzePatch() error = %v", err)
	}

	if summary.Total != 0 {
		t.Fatalf("Total = %d, want 0 for empty patch", summary.Total)
	}
	if summary.Percent != 100 {
		t.Fatalf("Percent = %.2f, want 100 for empty patch (no lines to cover)", summary.Percent)
	}
}

func TestAnalyzePatch_InvalidProfileHeader(t *testing.T) {
	t.Parallel()

	profile := strings.NewReader(`not a profile
github.com/javinizer/javinizer-go/pkg/file.go:5.1,7.1 1 1
`)

	_, err := analyzePatch(profile, PatchOptions{
		PatchLines:   PatchLineSet{"pkg/file.go": {5: true}},
		ModulePrefix: testModulePrefix,
	})
	if err == nil {
		t.Fatal("analyzePatch() should error on invalid header, got nil")
	}
}

func TestAnalyzePatch_EmptyProfile(t *testing.T) {
	t.Parallel()

	profile := strings.NewReader("")

	_, err := analyzePatch(profile, PatchOptions{
		PatchLines:   PatchLineSet{"pkg/file.go": {5: true}},
		ModulePrefix: testModulePrefix,
	})
	if err == nil {
		t.Fatal("analyzePatch() should error on empty profile, got nil")
	}
}

func TestAnalyzeProfilePatch_OpensFile(t *testing.T) {
	t.Parallel()

	f := patchProfile(t, `mode: count
github.com/javinizer/javinizer-go/pkg/file.go:5.1,7.1 1 1
`)
	defer func() { _ = f.Close() }()

	summary, err := AnalyzeProfilePatch(f.Name(), PatchOptions{
		PatchLines:   PatchLineSet{"pkg/file.go": {5: true, 6: true}},
		ModulePrefix: testModulePrefix,
	})
	if err != nil {
		t.Fatalf("AnalyzeProfilePatch() error = %v", err)
	}
	if summary.Total != 2 || summary.Hit != 2 {
		t.Fatalf("got Total=%d Hit=%d, want Total=2 Hit=2", summary.Total, summary.Hit)
	}
}

func TestAnalyzeProfilePatch_MissingFile(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "does-not-exist.out")
	_, err := AnalyzeProfilePatch(missing, PatchOptions{
		PatchLines:   PatchLineSet{},
		ModulePrefix: testModulePrefix,
	})
	if err == nil {
		t.Fatal("AnalyzeProfilePatch() should error on missing file, got nil")
	}
}

func TestCompileIgnoreGlob(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		pattern string
		path    string
		want    bool
	}{
		{"doublestar prefix matches nested", "test/e2e/**", "test/e2e/cli/run.go", true},
		{"doublestar prefix matches direct child", "test/e2e/**", "test/e2e/run.go", true},
		{"doublestar prefix does not match dir itself", "test/e2e/**", "test/e2e", false},
		{"doublestar prefix does not match other dir", "test/e2e/**", "test/unit/run.go", false},
		{"exact file matches", "internal/desktop/app.go", "internal/desktop/app.go", true},
		{"exact file does not match other", "internal/desktop/app.go", "internal/desktop/paths.go", false},
		{"web doublestar matches nested", "web/**", "web/dist/index.html", true},
		{"web doublestar does not match sibling", "web/**", "webb/index.html", false},
		{"leading doublestar matches any depth", "**/node_modules/**", "vendor/node_modules/pkg/lib.go", true},
		{"leading doublestar matches at root", "**/node_modules/**", "node_modules/pkg/lib.go", true},
		{"leading doublestar does not match absent", "**/node_modules/**", "vendor/other/lib.go", false},
		{"single star matches one segment", "pkg/*.go", "pkg/file.go", true},
		{"single star does not cross slash", "pkg/*.go", "pkg/sub/file.go", false},
		{"question mark matches one char", "pkg/fil?.go", "pkg/file.go", true},
		{"question mark does not match slash", "pkg/fil?.go", "pkg/fil/.go", false},
		{"dot-slash prefix is stripped", "./web/**", "web/dist/index.html", true},
		{"regex special chars escaped", "pkg/file.go", "pkg/fileXgo", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			re, err := compileIgnoreGlob(tc.pattern)
			if err != nil {
				t.Fatalf("compileIgnoreGlob(%q) error = %v", tc.pattern, err)
			}
			if got := re.MatchString(tc.path); got != tc.want {
				t.Fatalf("compileIgnoreGlob(%q).MatchString(%q) = %v, want %v", tc.pattern, tc.path, got, tc.want)
			}
		})
	}
}

// TestAnalyzePatch_PatchLineMissingFromProfile covers the case CodeRabbit
// flagged: a changed file with no coverage blocks in the profile. Without the
// miss-seeding fix, such a file disappears from Total entirely and percentage()
// returns 100% (Total==0), letting a fully-untested diff pass the patch gate.
// With the fix, every patch line with no profile entry counts as a miss.
func TestAnalyzePatch_PatchLineMissingFromProfile(t *testing.T) {
	t.Parallel()

	profile := strings.NewReader(`mode: count
github.com/javinizer/javinizer-go/pkg/covered.go:5.1,7.1 1 1
`)

	summary, err := analyzePatch(profile, PatchOptions{
		PatchLines: PatchLineSet{
			"pkg/covered.go":  {5: true, 6: true},
			"pkg/untested.go": {10: true, 11: true},
		},
		ModulePrefix: testModulePrefix,
	})
	if err != nil {
		t.Fatalf("analyzePatch() error = %v", err)
	}

	if summary.Total != 4 {
		t.Fatalf("Total = %d, want 4 (untested.go lines must count as misses, not disappear)", summary.Total)
	}
	if summary.Hit != 2 {
		t.Fatalf("Hit = %d, want 2", summary.Hit)
	}
	if summary.Miss != 2 {
		t.Fatalf("Miss = %d, want 2 (the untested.go lines)", summary.Miss)
	}
	if got := summary.Percent; got < 49.9 || got > 50.1 {
		t.Fatalf("Percent = %.2f, want about 50.0 (2 hit / 4 total) — a fully-untested file must NOT pass at 100%%", got)
	}
}
