package mediainfo

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/javinizer/javinizer-go/internal/logging"
)

// FileReader combines io.Reader, io.ReaderAt and io.Seeker to abstract file access
// for native probers. *os.File satisfies this interface, but tests can
// provide custom implementations without filesystem dependencies.
type FileReader interface {
	io.Reader
	io.ReaderAt
	io.Seeker
}

// fileReaderStat extends FileReader with a Stat method for file size.
// *os.File satisfies this interface.
type fileReaderStat interface {
	FileReader
	Stat() (os.FileInfo, error)
	Name() string
}

// prober defines the interface for video metadata extraction
type prober interface {
	// Name returns the prober identifier (e.g., "mp4", "mkv", "avi")
	Name() string

	// canProbe checks if this prober can handle the file
	// Returns true if file header matches this format
	canProbe(header []byte) bool

	// Probe extracts metadata from the file
	// Returns VideoInfo with as much information as possible
	Probe(ctx context.Context, f FileReader) (*VideoInfo, error)
}

// proberRegistry manages available probers and fallback logic
type proberRegistry struct {
	probers    []prober
	cliProber  *cliProber // Optional CLI fallback
	cliEnabled bool
}

// newProberRegistry creates registry with all native parsers
func newProberRegistry(cfg *mediaInfoConfig) *proberRegistry {
	registry := &proberRegistry{
		probers:    make([]prober, 0),
		cliEnabled: cfg != nil && cfg.CLIEnabled,
	}

	// Register native probers
	// NOTE: MOV must come before MP4 since both use ftyp box
	// MOV checks for QuickTime-specific brands, MP4 is more generic
	registry.Register(newMOVProber())
	registry.Register(newMP4Prober())
	registry.Register(newMKVProber())
	registry.Register(newAVIProber())

	// Register CLI fallback if enabled
	if registry.cliEnabled && cfg != nil {
		registry.cliProber = newCLIProber(cfg)
	}

	return registry
}

// Register adds a prober to the registry
func (r *proberRegistry) Register(p prober) {
	r.probers = append(r.probers, p)
}

// findProber returns appropriate prober for file header
// Returns nil if no prober can handle the format
func (r *proberRegistry) findProber(header []byte) prober {
	for _, prober := range r.probers {
		if prober.canProbe(header) {
			return prober
		}
	}
	return nil
}

// probeWithFallback tries native parser first, falls back to CLI if available
func (r *proberRegistry) probeWithFallback(ctx context.Context, f FileReader) (*VideoInfo, error) {
	// Read header for detection using ReadAt (does not affect Seek position)
	header := make([]byte, 16)
	n, err := f.ReadAt(header, 0)
	if err != nil || n < 16 {
		return nil, fmt.Errorf("failed to read file header: %w", err)
	}

	// Reset file pointer for actual parsing
	_, err = f.Seek(0, io.SeekStart)
	if err != nil {
		return nil, fmt.Errorf("failed to reset file pointer: %w", err)
	}

	// Try native prober
	prober := r.findProber(header)
	var nativeErr error
	if prober != nil {
		info, err := prober.Probe(ctx, f)
		if err == nil {
			return info, nil
		}
		// Preserve the native parser failure so it can be returned when there is
		// no CLI fallback; otherwise truncated/invalid MP4/MKV parse failures would
		// be masked as "unsupported container format".
		nativeErr = fmt.Errorf("%s parser failed: %w", prober.Name(), err)
		// Log native parser failure, but continue to CLI fallback
		// Errors are expected for some edge cases
		logging.Debugf("mediainfo: %s native parser failed: %v", prober.Name(), err)
	}

	// Fallback to CLI if enabled
	if r.cliProber != nil {
		// Reset file pointer again for CLI
		_, _ = f.Seek(0, io.SeekStart)
		return r.cliProber.Probe(ctx, f)
	}

	if nativeErr != nil {
		return nil, nativeErr
	}

	// No prober found and no CLI fallback
	container := detectContainer(header)
	return nil, fmt.Errorf("unsupported container format: %s (no native parser or CLI fallback)", container)
}

// mediaInfoConfig holds configuration for MediaInfo functionality
type mediaInfoConfig struct {
	CLIEnabled bool   // Enable MediaInfo CLI fallback (default: false)
	CLIPath    string // Path to mediainfo binary (default: "mediainfo")
	CLITimeout int    // Timeout in seconds (default: 30)
}

// defaultMediaInfoConfig returns default configuration
func defaultMediaInfoConfig() *mediaInfoConfig {
	return &mediaInfoConfig{
		CLIEnabled: false,
		CLIPath:    "mediainfo",
		CLITimeout: 30,
	}
}
