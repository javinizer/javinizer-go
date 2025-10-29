package mediainfo

import (
	"fmt"
	"os"
)

// CLIProber implements the Prober interface using MediaInfo CLI
// TODO: Implement CLI execution and JSON parsing
type CLIProber struct {
	enabled bool
	path    string
	timeout int
}

// NewCLIProber creates a new CLI prober
func NewCLIProber(cfg *MediaInfoConfig) *CLIProber {
	if cfg == nil {
		cfg = DefaultMediaInfoConfig()
	}
	return &CLIProber{
		enabled: cfg.CLIEnabled,
		path:    cfg.CLIPath,
		timeout: cfg.CLITimeout,
	}
}

// Name returns the prober identifier
func (p *CLIProber) Name() string {
	return "mediainfo-cli"
}

// CanProbe checks if this prober can handle the file based on header
func (p *CLIProber) CanProbe(header []byte) bool {
	// CLI can probe anything if enabled
	return p.enabled
}

// Probe extracts metadata from the file using MediaInfo CLI
func (p *CLIProber) Probe(f *os.File) (*VideoInfo, error) {
	if !p.enabled {
		return nil, fmt.Errorf("MediaInfo CLI is disabled")
	}
	// TODO: Implement CLI execution and JSON parsing in Phase 6
	return nil, fmt.Errorf("MediaInfo CLI not yet implemented (Phase 6)")
}
