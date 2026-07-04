package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/javinizer/javinizer-go/internal/coverage"
)

var osExit = os.Exit

func main() {
	osExit(run(os.Args[1:], os.Stdout, os.Stderr))
}

type analyzeProfileFn func(path string) (coverage.Summary, error)

func run(args []string, stdout, stderr io.Writer) int {
	return runWithAnalyze(args, stdout, stderr, coverage.AnalyzeProfile)
}

func runWithAnalyze(args []string, stdout, stderr io.Writer, analyze analyzeProfileFn) int {
	var minCoverage float64
	var profilePath string
	var metric string
	var patch bool
	var baseRef string

	fs := flag.NewFlagSet("coveragecheck", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Float64Var(&minCoverage, "min", 75, "minimum required coverage percentage")
	fs.StringVar(&profilePath, "profile", "coverage.out", "path to coverage profile")
	fs.StringVar(&metric, "metric", "line", "coverage metric to enforce: line or statement")
	fs.BoolVar(&patch, "patch", false, "check patch coverage (new/changed lines vs base ref) like codecov/patch")
	fs.StringVar(&baseRef, "base", "main", "git base ref for --patch (the merge-base is used so a behind branch still diffs against the fork point)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if patch {
		return runPatchCheck(profilePath, baseRef, minCoverage, stdout, stderr)
	}

	summary, err := analyze(profilePath)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
		return 2
	}

	selectedPercent, selectedLabel, selectedDetails, err := selectMetric(metric, summary)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
		return 2
	}

	if _, err := fmt.Fprintln(stdout, "=========================================="); err != nil {
		return 2
	}
	if _, err := fmt.Fprintln(stdout, "Test Coverage Report"); err != nil {
		return 2
	}
	if _, err := fmt.Fprintln(stdout, "=========================================="); err != nil {
		return 2
	}
	if _, err := fmt.Fprintf(stdout, "Coverage Profile: %s\n", profilePath); err != nil {
		return 2
	}
	if _, err := fmt.Fprintf(stdout, "Enforced Metric:  %s\n", selectedLabel); err != nil {
		return 2
	}
	if _, err := fmt.Fprintf(stdout, "Current Coverage: %.2f%%\n", selectedPercent); err != nil {
		return 2
	}
	if _, err := fmt.Fprintf(stdout, "Required Minimum: %.2f%%\n", minCoverage); err != nil {
		return 2
	}
	if _, err := fmt.Fprintln(stdout, "=========================================="); err != nil {
		return 2
	}
	if _, err := fmt.Fprintf(stdout, "Line Coverage:     %.2f%% (%d hit, %d partial, %d miss, %d total)\n",
		summary.Line.Percent, summary.Line.Hit, summary.Line.Partial, summary.Line.Miss, summary.Line.Total); err != nil {
		return 2
	}
	if _, err := fmt.Fprintf(stdout, "Statement Coverage %.2f%% (%d covered, %d total)\n",
		summary.Statement.Percent, summary.Statement.Covered, summary.Statement.Total); err != nil {
		return 2
	}
	if _, err := fmt.Fprintf(stdout, "Metric Details:    %s\n", selectedDetails); err != nil {
		return 2
	}

	if selectedPercent+1e-9 < minCoverage {
		if _, err := fmt.Fprintln(stdout, "Coverage check FAILED"); err != nil {
			return 2
		}
		return 1
	}

	if _, err := fmt.Fprintln(stdout, "Coverage check PASSED"); err != nil {
		return 2
	}
	return 0
}

func selectMetric(metric string, summary coverage.Summary) (float64, string, string, error) {
	switch metric {
	case "line":
		return summary.Line.Percent, "Codecov line coverage", "fully covered lines only count toward the percentage", nil
	case "statement":
		return summary.Statement.Percent, "Go statement coverage", "matches go tool cover -func total", nil
	default:
		return 0, "", "", fmt.Errorf("unsupported metric %q", metric)
	}
}
