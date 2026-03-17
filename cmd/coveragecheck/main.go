package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/javinizer/javinizer-go/internal/coverage"
)

func main() {
	var minCoverage float64
	var profilePath string
	var metric string

	flag.Float64Var(&minCoverage, "min", 75, "minimum required coverage percentage")
	flag.StringVar(&profilePath, "profile", "coverage.out", "path to coverage profile")
	flag.StringVar(&metric, "metric", "line", "coverage metric to enforce: line or statement")
	flag.Parse()

	summary, err := coverage.AnalyzeProfile(profilePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(2)
	}

	selectedPercent, selectedLabel, selectedDetails := selectMetric(metric, summary)

	fmt.Println("==========================================")
	fmt.Println("Test Coverage Report")
	fmt.Println("==========================================")
	fmt.Printf("Coverage Profile: %s\n", profilePath)
	fmt.Printf("Enforced Metric:  %s\n", selectedLabel)
	fmt.Printf("Current Coverage: %.2f%%\n", selectedPercent)
	fmt.Printf("Required Minimum: %.2f%%\n", minCoverage)
	fmt.Println("==========================================")
	fmt.Printf("Line Coverage:     %.2f%% (%d hit, %d partial, %d miss, %d total)\n",
		summary.Line.Percent, summary.Line.Hit, summary.Line.Partial, summary.Line.Miss, summary.Line.Total)
	fmt.Printf("Statement Coverage %.2f%% (%d covered, %d total)\n",
		summary.Statement.Percent, summary.Statement.Covered, summary.Statement.Total)
	fmt.Printf("Metric Details:    %s\n", selectedDetails)

	if selectedPercent+1e-9 < minCoverage {
		fmt.Println("Coverage check FAILED")
		os.Exit(1)
	}

	fmt.Println("Coverage check PASSED")
}

func selectMetric(metric string, summary coverage.Summary) (float64, string, string) {
	switch metric {
	case "line":
		return summary.Line.Percent, "Codecov line coverage", "fully covered lines only count toward the percentage"
	case "statement":
		return summary.Statement.Percent, "Go statement coverage", "matches go tool cover -func total"
	default:
		fmt.Fprintf(os.Stderr, "Error: unsupported metric %q\n", metric)
		os.Exit(2)
		return 0, "", ""
	}
}
