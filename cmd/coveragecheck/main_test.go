package main

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/coverage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func stubSummary() coverage.Summary {
	return coverage.Summary{
		Line: coverage.LineSummary{
			Total:   100,
			Hit:     80,
			Partial: 10,
			Miss:    10,
			Percent: 80.0,
		},
		Statement: coverage.StatementSummary{
			Total:   200,
			Covered: 170,
			Percent: 85.0,
		},
	}
}

func TestRunWithAnalyze(t *testing.T) {
	t.Run("line metric passes", func(t *testing.T) {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		called := false
		exitCode := runWithAnalyze(
			[]string{"--profile", "coverage.out", "--metric", "line", "--min", "79.9"},
			&stdout,
			&stderr,
			func(path string, _ coverage.ReportOptions) (coverage.Summary, error) {
				called = true
				assert.Equal(t, "coverage.out", path)
				return stubSummary(), nil
			},
		)

		require.True(t, called)
		assert.Equal(t, 0, exitCode)
		assert.Empty(t, stderr.String())
		assert.Contains(t, stdout.String(), "Enforced Metric:  Codecov line coverage")
		assert.Contains(t, stdout.String(), "Coverage check PASSED")
	})

	t.Run("statement metric fails threshold", func(t *testing.T) {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exitCode := runWithAnalyze(
			[]string{"--metric", "statement", "--min", "90"},
			&stdout,
			&stderr,
			func(string, coverage.ReportOptions) (coverage.Summary, error) {
				return stubSummary(), nil
			},
		)

		assert.Equal(t, 1, exitCode)
		assert.Empty(t, stderr.String())
		assert.Contains(t, stdout.String(), "Enforced Metric:  Go statement coverage")
		assert.Contains(t, stdout.String(), "Coverage check FAILED")
	})

	t.Run("analyze failure returns code 2", func(t *testing.T) {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exitCode := runWithAnalyze(
			[]string{},
			&stdout,
			&stderr,
			func(string, coverage.ReportOptions) (coverage.Summary, error) {
				return coverage.Summary{}, errors.New("boom")
			},
		)

		assert.Equal(t, 2, exitCode)
		assert.Empty(t, stdout.String())
		assert.Contains(t, stderr.String(), "Error: boom")
	})

	t.Run("unsupported metric returns code 2", func(t *testing.T) {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exitCode := runWithAnalyze(
			[]string{"--metric", "bogus"},
			&stdout,
			&stderr,
			func(string, coverage.ReportOptions) (coverage.Summary, error) {
				return stubSummary(), nil
			},
		)

		assert.Equal(t, 2, exitCode)
		assert.Empty(t, stdout.String())
		assert.Contains(t, stderr.String(), `unsupported metric "bogus"`)
	})

	t.Run("flag parse failure returns code 2", func(t *testing.T) {
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		exitCode := runWithAnalyze(
			[]string{"--not-a-flag"},
			&stdout,
			&stderr,
			func(string, coverage.ReportOptions) (coverage.Summary, error) {
				t.Fatalf("analyze should not be called when flag parsing fails")
				return coverage.Summary{}, nil
			},
		)

		assert.Equal(t, 2, exitCode)
		assert.Empty(t, stdout.String())
		assert.True(t, strings.Contains(stderr.String(), "flag provided but not defined") || strings.Contains(stderr.String(), "unknown flag"))
	})
}

func TestSelectMetric(t *testing.T) {
	summary := stubSummary()

	t.Run("line", func(t *testing.T) {
		percent, label, details, err := selectMetric("line", summary)
		require.NoError(t, err)
		assert.Equal(t, 80.0, percent)
		assert.Equal(t, "Codecov line coverage", label)
		assert.Contains(t, details, "fully covered lines")
	})

	t.Run("statement", func(t *testing.T) {
		percent, label, details, err := selectMetric("statement", summary)
		require.NoError(t, err)
		assert.Equal(t, 85.0, percent)
		assert.Equal(t, "Go statement coverage", label)
		assert.Contains(t, details, "go tool cover")
	})

	t.Run("invalid", func(t *testing.T) {
		_, _, _, err := selectMetric("invalid", summary)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `unsupported metric "invalid"`)
	})
}

func TestRun_ProjectModeAppliesCodecovIgnores(t *testing.T) {
	// The ignore list must apply to PROJECT coverage too — otherwise the
	// local number underreads versus the Codecov report (patch mode already
	// applied it; project mode did not).
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(dir+"/go.mod", []byte("module github.com/javinizer/javinizer-go\n"), 0o644))
	require.NoError(t, os.WriteFile(dir+"/codecov.yml", []byte("coverage:\n  status: {}\nignore:\n  - \"docs/swagger/**\"\n"), 0o644))
	profilePath := dir + "/coverage.out"
	require.NoError(t, os.WriteFile(profilePath, []byte(
		"mode: atomic\n"+
			"github.com/javinizer/javinizer-go/internal/x/ok.go:4.1,6.1 2 1\n"+
			"github.com/javinizer/javinizer-go/docs/swagger/docs.go:10.1,12.1 2 0\n"), 0o644))

	origGetwd := osGetwd
	t.Cleanup(func() { osGetwd = origGetwd })
	osGetwd = func() (string, error) { return dir, nil }

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"--profile", profilePath, "--min", "100"}, &stdout, &stderr)
	require.Equal(t, 0, exitCode, "ignored file must not count against project coverage; stderr=%s", stderr)
	assert.Contains(t, stdout.String(), "Ignored Globs:")
	assert.Contains(t, stdout.String(), "docs/swagger/**")
	assert.Contains(t, stdout.String(), "Coverage check PASSED")
}

func TestRun_ProjectModeWarnsOnGetwdError(t *testing.T) {
	dir := t.TempDir()
	profilePath := dir + "/coverage.out"
	require.NoError(t, os.WriteFile(profilePath, []byte(
		"mode: atomic\n"+
			"github.com/javinizer/javinizer-go/internal/x/ok.go:4.1,6.1 2 1\n"), 0o644))

	origGetwd := osGetwd
	t.Cleanup(func() { osGetwd = origGetwd })
	osGetwd = func() (string, error) { return "", errors.New("cwd unavailable") }

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"--profile", profilePath, "--min", "1"}, &stdout, &stderr)
	require.Equal(t, 0, exitCode)
	assert.Contains(t, stderr.String(), "Warning: cannot determine repo root")
}

func TestRun_ProjectModeWarnsOnMalformedCodecovYml(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(dir+"/go.mod", []byte("module github.com/javinizer/javinizer-go\n"), 0o644))
	require.NoError(t, os.WriteFile(dir+"/codecov.yml", []byte("coverage:\n  status: [unclosed\n"), 0o644))
	profilePath := dir + "/coverage.out"
	require.NoError(t, os.WriteFile(profilePath, []byte(
		"mode: atomic\n"+
			"github.com/javinizer/javinizer-go/internal/x/ok.go:4.1,6.1 2 1\n"), 0o644))

	origGetwd := osGetwd
	t.Cleanup(func() { osGetwd = origGetwd })
	osGetwd = func() (string, error) { return dir, nil }

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"--profile", profilePath, "--min", "1"}, &stdout, &stderr)
	require.Equal(t, 0, exitCode)
	assert.Contains(t, stderr.String(), "Warning: cannot load codecov ignore rules")
}

func TestRun_ProjectModeWarnsOnMissingGoMod(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(dir+"/codecov.yml", []byte("ignore:\n  - \"docs/**\"\n"), 0o644))
	profilePath := dir + "/coverage.out"
	require.NoError(t, os.WriteFile(profilePath, []byte(
		"mode: atomic\n"+
			"github.com/javinizer/javinizer-go/internal/x/ok.go:4.1,6.1 2 1\n"), 0o644))

	origGetwd := osGetwd
	t.Cleanup(func() { osGetwd = origGetwd })
	osGetwd = func() (string, error) { return dir, nil }

	var stdout, stderr bytes.Buffer
	exitCode := run([]string{"--profile", profilePath, "--min", "1"}, &stdout, &stderr)
	require.Equal(t, 0, exitCode)
	assert.Contains(t, stderr.String(), "Warning: cannot resolve module prefix")
}

func TestRun_Wrapper(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := run([]string{"--profile", "does-not-exist.out"}, &stdout, &stderr)
	assert.Equal(t, 2, exitCode)
	assert.Empty(t, stdout.String())
	assert.Contains(t, stderr.String(), "does-not-exist.out")
}

func TestMain_InvokesExitWithRunCode(t *testing.T) {
	oldArgs := os.Args
	oldExit := osExit
	t.Cleanup(func() {
		os.Args = oldArgs
		osExit = oldExit
	})

	os.Args = []string{"coveragecheck", "--profile", "does-not-exist.out"}
	exitCode := 0
	osExit = func(code int) {
		exitCode = code
	}

	main()
	assert.Equal(t, 2, exitCode)
}
