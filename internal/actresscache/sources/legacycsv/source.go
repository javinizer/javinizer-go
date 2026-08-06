package legacycsvsource

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/javinizer/javinizer-go/internal/actresscache"
)

// sourceName ...
const sourceName = "legacy-jvthumbs"

// source ...
type source struct{}

// New ...
func New() actresscache.Source {
	return &source{}
}

// Name ...
func (s *source) Name() string {
	return sourceName
}

// Collect ...
func (s *source) Collect(ctx context.Context, options actresscache.SourceOptions, emit func(actresscache.Candidate) error) error {
	truncated := false
	if err := ctx.Err(); err != nil {
		return err
	}
	path := ""
	if options.Parameters != nil {
		path = strings.TrimSpace(options.Parameters["legacy.csv"])
		if path == "" {
			path = strings.TrimSpace(options.Parameters["jvthumbs.csv"])
		}
	}
	if path == "" {
		return fmt.Errorf("legacy-jvthumbs source requires --option legacy.csv=PATH")
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open legacy thumbnail CSV: %w", err)
	}
	defer func() { _ = file.Close() }()
	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	header, err := reader.Read()
	if err != nil {
		return fmt.Errorf("read legacy thumbnail CSV header: %w", err)
	}
	columns := findColumns(header)
	if columns.japaneseName < 0 && columns.firstName < 0 && columns.lastName < 0 && columns.fullName < 0 {
		return fmt.Errorf("legacy thumbnail CSV has no actress name columns")
	}
	if columns.thumbURL < 0 {
		return fmt.Errorf("legacy thumbnail CSV has no ThumbUrl column")
	}
	workers := options.Workers
	if workers < 1 {
		workers = 1
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan actresscache.Candidate)
	// wg ...
	var wg sync.WaitGroup
	// errOnce ...
	var errOnce sync.Once
	// emitErr ...
	var emitErr error
	worker := func() {
		defer wg.Done()
		for candidate := range jobs {
			if err := emit(candidate); err != nil {
				errOnce.Do(func() {
					emitErr = err
					cancel()
				})
				return
			}
		}
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go worker()
	}
	fileURL := "legacy-jvthumbs.csv"
	processed := 0
	// readErr ...
	var readErr error
	for rowNumber := 2; ; rowNumber++ {
		if err := ctx.Err(); err != nil {
			readErr = err
			break
		}
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			readErr = fmt.Errorf("read legacy thumbnail CSV row %d: %w", rowNumber, err)
			break
		}
		candidate := candidateFromRow(row, columns, fileURL, rowNumber)
		if options.MarkSeen != nil {
			// Mark BEFORE the thumbnail check: a row whose thumbnail is
			// temporarily blank must keep its prior journal entry alive
			// (marked seen = not staled by the completed-source sweep),
			// otherwise the published cache silently loses the actress.
			options.MarkSeen(candidate.Key)
		}
		if candidate.ThumbURL == "" {
			continue
		}
		if options.ShouldSkip != nil && options.ShouldSkip(candidate.Key) {
			processed++
			if options.Limit > 0 && processed >= options.Limit {
				truncated = true
				break
			}
			continue
		}
		if options.Limit > 0 && processed >= options.Limit {
			truncated = true // windowed enumeration is not exhaustive
			break
		}
		processed++
		select {
		case jobs <- candidate:
		case <-ctx.Done():
			readErr = ctx.Err()
		}
		if readErr != nil {
			break
		}
	}
	close(jobs)
	wg.Wait()
	if emitErr != nil {
		return emitErr
	}
	if readErr == nil && !truncated && options.MarkComplete != nil {
		options.MarkComplete()
	}
	return readErr
}

// csvColumns ...
type csvColumns struct {
	fullName     int
	lastName     int
	firstName    int
	japaneseName int
	thumbURL     int
	alias        int
}

// findColumns ...
func findColumns(header []string) csvColumns {
	columns := csvColumns{fullName: -1, lastName: -1, firstName: -1, japaneseName: -1, thumbURL: -1, alias: -1}
	for index, raw := range header {
		switch normalizeHeader(raw) {
		case "fullname":
			columns.fullName = index
		case "lastname":
			columns.lastName = index
		case "firstname":
			columns.firstName = index
		case "japanesename":
			columns.japaneseName = index
		case "thumburl", "thumbnailurl":
			columns.thumbURL = index
		case "alias", "aliases":
			columns.alias = index
		}
	}
	return columns
}

// normalizeHeader ...
func normalizeHeader(raw string) string {
	raw = strings.TrimPrefix(raw, "\ufeff")
	// b ...
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(raw)) {
		if r == '_' || r == '-' || r == ' ' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// candidateFromRow ...
func candidateFromRow(row []string, columns csvColumns, fileURL string, rowNumber int) actresscache.Candidate {
	get := func(index int) string {
		if index < 0 || index >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[index])
	}
	fullName := get(columns.fullName)
	firstName := get(columns.firstName)
	lastName := get(columns.lastName)
	if firstName == "" && lastName == "" && fullName != "" {
		parts := strings.Fields(fullName)
		if len(parts) == 1 {
			firstName = parts[0]
		} else if len(parts) > 1 {
			lastName = parts[0]
			firstName = strings.Join(parts[1:], " ")
		}
	}
	// Stable identity key: digest ONLY the identity fields. Mutable content
	// (the thumbnail URL) must not mint a new key -- otherwise a changed row
	// whose validation transiently fails gets pruned with its old key absent,
	// and the published cache silently loses the actress instead of keeping
	// the last-good entry. Content changes heal via --refresh.
	fields := []string{fullName, lastName, firstName, get(columns.japaneseName), get(columns.alias)}
	digest := sha256.New()
	for _, field := range fields {
		_, _ = io.WriteString(digest, field)
		_, _ = digest.Write([]byte{0})
	}
	digestText := fmt.Sprintf("%x", digest.Sum(nil)[:12])
	return actresscache.Candidate{
		// Identity key is content-derived ONLY: row positions shift when the
		// CSV gains/reorders rows, and a position-dependent key would orphan
		// the last-good journal entry on any upstream insertion.
		Key:          fmt.Sprintf("%s:%s", sourceName, digestText),
		Source:       sourceName,
		SourceID:     strconv.Itoa(rowNumber),
		SourceURL:    fileURL + "#row=" + strconv.Itoa(rowNumber),
		FirstName:    firstName,
		LastName:     lastName,
		JapaneseName: get(columns.japaneseName),
		Aliases:      splitAliases(get(columns.alias)),
		ThumbURL:     get(columns.thumbURL),
	}
}

// splitAliases ...
func splitAliases(raw string) []string {
	parts := strings.Split(raw, "|")
	aliases := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			aliases = append(aliases, part)
		}
	}
	return aliases
}
