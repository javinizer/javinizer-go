package mediainfo

import (
	"fmt"
	"os"
)

// AVIProber implements the Prober interface for AVI containers
// TODO: Implement full RIFF parser
type AVIProber struct{}

// NewAVIProber creates a new AVI prober
func NewAVIProber() *AVIProber {
	return &AVIProber{}
}

// Name returns the prober identifier
func (p *AVIProber) Name() string {
	return "avi"
}

// CanProbe checks if this prober can handle the file based on header
func (p *AVIProber) CanProbe(header []byte) bool {
	// AVI: starts with "RIFF" and contains "AVI " at offset 8
	if len(header) >= 12 {
		return header[0] == 'R' && header[1] == 'I' && header[2] == 'F' && header[3] == 'F' &&
			header[8] == 'A' && header[9] == 'V' && header[10] == 'I'
	}
	return false
}

// Probe extracts metadata from the AVI file
func (p *AVIProber) Probe(f *os.File) (*VideoInfo, error) {
	// TODO: Implement full RIFF parser in Phase 4
	return nil, fmt.Errorf("AVI parsing not yet implemented (Phase 4)")
}
