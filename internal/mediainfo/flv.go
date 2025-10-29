package mediainfo

import (
	"fmt"
	"os"
)

// FLVProber implements the Prober interface for FLV containers
// TODO: Implement full FLV parser
type FLVProber struct{}

// NewFLVProber creates a new FLV prober
func NewFLVProber() *FLVProber {
	return &FLVProber{}
}

// Name returns the prober identifier
func (p *FLVProber) Name() string {
	return "flv"
}

// CanProbe checks if this prober can handle the file based on header
func (p *FLVProber) CanProbe(header []byte) bool {
	// FLV: starts with "FLV"
	if len(header) >= 3 {
		return header[0] == 'F' && header[1] == 'L' && header[2] == 'V'
	}
	return false
}

// Probe extracts metadata from the FLV file
func (p *FLVProber) Probe(f *os.File) (*VideoInfo, error) {
	// TODO: Implement full FLV parser in Phase 5
	return nil, fmt.Errorf("FLV parsing not yet implemented (Phase 5)")
}
