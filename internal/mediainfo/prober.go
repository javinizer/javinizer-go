package mediainfo

import (
	"fmt"
	"os"
)

// Prober defines the interface for video metadata extraction
type Prober interface {
	// Name returns the prober identifier (e.g., "mp4", "mkv", "avi")
	Name() string

	// CanProbe checks if this prober can handle the file
	// Returns true if file header matches this format
	CanProbe(header []byte) bool

	// Probe extracts metadata from the file
	// Returns VideoInfo with as much information as possible
	Probe(f *os.File) (*VideoInfo, error)
}

// ProberRegistry manages available probers and fallback logic
type ProberRegistry struct {
	probers    []Prober
	cliProber  *CLIProber // Optional CLI fallback
	cliEnabled bool
}

// NewProberRegistry creates registry with all native parsers
func NewProberRegistry(cfg *MediaInfoConfig) *ProberRegistry {
	registry := &ProberRegistry{
		probers:    make([]Prober, 0),
		cliEnabled: cfg != nil && cfg.CLIEnabled,
	}

	// Register native probers
	// NOTE: MOV must come before MP4 since both use ftyp box
	// MOV checks for QuickTime-specific brands, MP4 is more generic
	registry.Register(NewMOVProber())
	registry.Register(NewMP4Prober())
	registry.Register(NewMKVProber())
	registry.Register(NewAVIProber())
	registry.Register(NewFLVProber())

	// Register CLI fallback if enabled
	if registry.cliEnabled && cfg != nil {
		registry.cliProber = NewCLIProber(cfg)
	}

	return registry
}

// Register adds a prober to the registry
func (r *ProberRegistry) Register(p Prober) {
	r.probers = append(r.probers, p)
}

// FindProber returns appropriate prober for file header
// Returns nil if no prober can handle the format
func (r *ProberRegistry) FindProber(header []byte) Prober {
	for _, prober := range r.probers {
		if prober.CanProbe(header) {
			return prober
		}
	}
	return nil
}

// ProbeWithFallback tries native parser first, falls back to CLI if available
func (r *ProberRegistry) ProbeWithFallback(f *os.File) (*VideoInfo, error) {
	// Read header for detection
	header := make([]byte, 16)
	n, err := f.Read(header)
	if err != nil || n < 16 {
		return nil, fmt.Errorf("failed to read file header: %w", err)
	}

	// Reset file pointer for actual parsing
	_, err = f.Seek(0, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to reset file pointer: %w", err)
	}

	// Try native prober
	prober := r.FindProber(header)
	if prober != nil {
		info, err := prober.Probe(f)
		if err == nil {
			return info, nil
		}
		// Log native parser failure, but continue to CLI fallback
		// Errors are expected for some edge cases
	}

	// Fallback to CLI if enabled
	if r.cliProber != nil {
		// Reset file pointer again for CLI
		_, _ = f.Seek(0, 0)
		return r.cliProber.Probe(f)
	}

	// No prober found and no CLI fallback
	container := detectContainer(header)
	return nil, fmt.Errorf("unsupported container format: %s (no native parser or CLI fallback)", container)
}

// MediaInfoConfig holds configuration for MediaInfo functionality
type MediaInfoConfig struct {
	CLIEnabled bool   // Enable MediaInfo CLI fallback (default: false)
	CLIPath    string // Path to mediainfo binary (default: "mediainfo")
	CLITimeout int    // Timeout in seconds (default: 30)
}

// DefaultMediaInfoConfig returns default configuration
func DefaultMediaInfoConfig() *MediaInfoConfig {
	return &MediaInfoConfig{
		CLIEnabled: false,
		CLIPath:    "mediainfo",
		CLITimeout: 30,
	}
}
