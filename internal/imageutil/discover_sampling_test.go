package imageutil

import (
	"bufio"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type dumpEntry struct {
	contentID    string
	dvdID        string
	coverPath    string
	galleryFirst string
	galleryLast  string
	galleryCount int
	siteID       string
	serviceCode  string
}

func parseDumpEntries(path string) ([]dumpEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []dumpEntry
	inCopyBlock := false
	scanner := bufio.NewScanner(f)
	// Increase buffer for large lines
	scanner.Buffer(make([]byte, 0), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "COPY public.derived_video ") {
			inCopyBlock = true
			continue
		}
		if inCopyBlock && line == "\\." {
			break
		}
		if !inCopyBlock {
			continue
		}

		fields := strings.Split(line, "\t")
		if len(fields) < 20 {
			continue
		}

		contentID := fields[0]
		dvdID := fields[1]
		coverPath := fields[12]    // jacket_full_url
		galleryFirst := fields[14] // gallery_full_first
		galleryLast := fields[15]  // gallery_full_last
		siteID := fields[18]
		serviceCode := fields[19]

		// Only site_id=2 (awsimgsrc/pics) digital videos
		if siteID != "2" {
			continue
		}
		if serviceCode != "digital" {
			continue
		}
		// Must have a gallery (at least first screenshot)
		if galleryFirst == "" || galleryFirst == "\\N" {
			continue
		}
		// Must have a cover
		if coverPath == "" || coverPath == "\\N" {
			continue
		}

		// Parse gallery count from first/last
		galleryCount := 1
		if galleryLast != "" && galleryLast != "\\N" {
			// Extract the number from e.g., "digital/video/1ipx00535/1ipx00535jp-5"
			parts := strings.Split(galleryLast, "-")
			if len(parts) >= 2 {
				if n, err := strconv.Atoi(parts[len(parts)-1]); err == nil {
					galleryCount = n
				}
			}
		}

		entries = append(entries, dumpEntry{
			contentID:    contentID,
			dvdID:        dvdID,
			coverPath:    coverPath,
			galleryFirst: galleryFirst,
			galleryLast:  galleryLast,
			galleryCount: galleryCount,
			siteID:       siteID,
			serviceCode:  serviceCode,
		})
	}

	return entries, scanner.Err()
}

func TestDiscoverScreenshotsSampling(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping external sampling test (requires r18dev database dump and HTTP requests)")
	}
	if os.Getenv("JAVINIZER_RUN_LIVE_API_TESTS") != "1" {
		t.Skip("Set JAVINIZER_RUN_LIVE_API_TESTS=1 to sample the live DMM CDN")
	}
	dumpPath := "/tmp/r18dev_dump.sql"
	entries, err := parseDumpEntries(dumpPath)
	if err != nil {
		t.Skipf("Dump file not available: %v", err)
	}
	if len(entries) == 0 {
		t.Skip("No entries parsed from dump")
	}
	t.Logf("Parsed %d entries with gallery data from dump", len(entries))

	// Sample 100 random entries
	rng := rand.New(rand.NewSource(42))
	sampleSize := 100
	if len(entries) < sampleSize {
		sampleSize = len(entries)
	}
	rng.Shuffle(len(entries), func(i, j int) { entries[i], entries[j] = entries[j], entries[i] })
	sample := entries[:sampleSize]

	client := &http.Client{Timeout: 15 * time.Second}

	type result struct {
		entry           dumpEntry
		discovered      int
		expected        int
		coverURL        string
		firstDiscovered string
		firstExpected   string
		status          string
	}

	var results []result
	matched, mismatched, noCover, noDiscover := 0, 0, 0, 0

	for _, e := range sample {
		coverURL := "https://pics.dmm.co.jp/" + e.coverPath + ".jpg"
		expected := e.galleryCount
		expectedFirst := "https://pics.dmm.co.jp/" + e.galleryFirst + ".jpg"

		// Check if cover URL is valid format
		if !strings.Contains(coverURL, "/digital/video/") || !strings.HasSuffix(coverURL, "pl.jpg") {
			results = append(results, result{
				entry:    e,
				status:   "SKIP_NONSTANDARD_COVER",
				coverURL: coverURL,
			})
			noCover++
			continue
		}

		discovered := DiscoverScreenshots(coverURL, client)
		var firstDiscovered string
		if len(discovered) > 0 {
			firstDiscovered = discovered[0]
		}

		r := result{
			entry:           e,
			discovered:      len(discovered),
			expected:        expected,
			coverURL:        coverURL,
			firstDiscovered: firstDiscovered,
			firstExpected:   expectedFirst,
		}

		if len(discovered) == 0 {
			r.status = "NO_DISCOVER"
			noDiscover++
		} else if len(discovered) == expected {
			r.status = "MATCH"
			matched++
		} else {
			r.status = "MISMATCH"
			mismatched++
		}

		results = append(results, r)
	}

	t.Logf("\n=== SAMPLING RESULTS ===")
	t.Logf("Total sampled: %d", len(results))
	t.Logf("MATCH (discovered == expected): %d", matched)
	t.Logf("MISMATCH (discovered != expected): %d", mismatched)
	t.Logf("NO_DISCOVER (discovered 0): %d", noDiscover)
	t.Logf("SKIP_NONSTANDARD_COVER: %d", noCover)

	// Log mismatches and no-discover details
	for _, r := range results {
		if r.status == "MISMATCH" {
			t.Logf("  MISMATCH %s: discovered=%d expected=%d first=%s expected_first=%s",
				r.entry.contentID, r.discovered, r.expected, r.firstDiscovered, r.firstExpected)
		}
		if r.status == "NO_DISCOVER" {
			t.Logf("  NO_DISCOVER %s (%s): cover=%s expected=%d",
				r.entry.contentID, r.entry.dvdID, r.coverURL, r.expected)
		}
	}

	// We expect at least 70% match rate for digital/video entries
	if matched+noCover < 70 {
		t.Errorf("Match rate too low: %d/%d matched, %d no_cover, %d mismatch, %d no_discover",
			matched, sampleSize, noCover, mismatched, noDiscover)
	}

	// Assert no total failures on standard covers
	assert.LessOrEqual(t, noDiscover+noCover, sampleSize/2, "Too many failures")
}
