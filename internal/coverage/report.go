package coverage

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// Summary contains both line-based and statement-based coverage totals.
type Summary struct {
	Line      LineSummary
	Statement StatementSummary
}

// LineSummary models Codecov-style hit/partial/miss line coverage.
type LineSummary struct {
	Total   int
	Hit     int
	Partial int
	Miss    int
	Percent float64
}

// StatementSummary mirrors go tool cover's statement coverage view.
type StatementSummary struct {
	Total   int
	Covered int
	Percent float64
}

type blockKey struct {
	file                string
	startLine, startCol int
	endLine, endCol     int
}

type block struct {
	file               string
	startLine, endLine int
	statements, count  int
}

type lineState struct {
	hasCovered   bool
	hasUncovered bool
}

// AnalyzeProfile reads a Go cover profile and returns both Codecov-style line
// coverage and statement coverage, deduplicating repeated blocks produced by
// multi-package aggregators such as go-acc.
func AnalyzeProfile(path string) (Summary, error) {
	file, err := os.Open(path)
	if err != nil {
		return Summary{}, fmt.Errorf("open profile: %w", err)
	}
	defer file.Close()

	return Analyze(file)
}

// Analyze parses a Go cover profile from r.
func Analyze(r io.Reader) (Summary, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return Summary{}, fmt.Errorf("read profile header: %w", err)
		}
		return Summary{}, fmt.Errorf("empty coverage profile")
	}

	header := strings.TrimSpace(scanner.Text())
	if !strings.HasPrefix(header, "mode:") {
		return Summary{}, fmt.Errorf("invalid coverage profile header: %q", header)
	}

	merged := make(map[blockKey]block)
	for lineNo := 2; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parsed, key, err := parseBlock(line)
		if err != nil {
			return Summary{}, fmt.Errorf("parse profile line %d: %w", lineNo, err)
		}

		if existing, ok := merged[key]; ok {
			existing.count += parsed.count
			merged[key] = existing
			continue
		}
		merged[key] = parsed
	}
	if err := scanner.Err(); err != nil {
		return Summary{}, fmt.Errorf("read coverage profile: %w", err)
	}

	var summary Summary
	fileLines := make(map[string]map[int]*lineState)

	for _, entry := range merged {
		summary.Statement.Total += entry.statements
		if entry.count > 0 {
			summary.Statement.Covered += entry.statements
		}

		lines := fileLines[entry.file]
		if lines == nil {
			lines = make(map[int]*lineState)
			fileLines[entry.file] = lines
		}

		for line := entry.startLine; line <= entry.endLine; line++ {
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

	for _, lines := range fileLines {
		for _, state := range lines {
			summary.Line.Total++
			switch {
			case state.hasCovered && state.hasUncovered:
				summary.Line.Partial++
			case state.hasCovered:
				summary.Line.Hit++
			default:
				summary.Line.Miss++
			}
		}
	}

	summary.Line.Percent = percentage(summary.Line.Hit, summary.Line.Total)
	summary.Statement.Percent = percentage(summary.Statement.Covered, summary.Statement.Total)

	return summary, nil
}

func parseBlock(line string) (block, blockKey, error) {
	fields := strings.Fields(line)
	if len(fields) != 3 {
		return block{}, blockKey{}, fmt.Errorf("expected 3 fields, got %d", len(fields))
	}

	location := fields[0]
	statements, err := strconv.Atoi(fields[1])
	if err != nil {
		return block{}, blockKey{}, fmt.Errorf("invalid statement count %q: %w", fields[1], err)
	}

	count, err := strconv.Atoi(fields[2])
	if err != nil {
		return block{}, blockKey{}, fmt.Errorf("invalid execution count %q: %w", fields[2], err)
	}

	colon := strings.LastIndex(location, ":")
	if colon == -1 {
		return block{}, blockKey{}, fmt.Errorf("missing file separator in %q", location)
	}

	file := location[:colon]
	span := location[colon+1:]
	rangeParts := strings.Split(span, ",")
	if len(rangeParts) != 2 {
		return block{}, blockKey{}, fmt.Errorf("invalid span %q", span)
	}

	startLine, startCol, err := parsePosition(rangeParts[0])
	if err != nil {
		return block{}, blockKey{}, fmt.Errorf("invalid start position %q: %w", rangeParts[0], err)
	}

	endLine, endCol, err := parsePosition(rangeParts[1])
	if err != nil {
		return block{}, blockKey{}, fmt.Errorf("invalid end position %q: %w", rangeParts[1], err)
	}

	entry := block{
		file:       file,
		startLine:  startLine,
		endLine:    endLine,
		statements: statements,
		count:      count,
	}
	key := blockKey{
		file:      file,
		startLine: startLine,
		startCol:  startCol,
		endLine:   endLine,
		endCol:    endCol,
	}

	return entry, key, nil
}

func parsePosition(value string) (int, int, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expected line.column")
	}

	line, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, err
	}

	column, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, err
	}

	return line, column, nil
}

func percentage(covered, total int) float64 {
	if total == 0 {
		return 100
	}

	return float64(covered) * 100 / float64(total)
}
