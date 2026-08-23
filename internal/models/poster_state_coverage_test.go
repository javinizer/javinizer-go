package models

import (
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCropBoundsValidFingerprintAndNonFiniteBranches(t *testing.T) {
	base := CropBounds{X: 0.1, Y: 0.1, Width: 0.5, Height: 0.5}
	for name, bounds := range map[string]CropBounds{
		"aspect NaN":          func() CropBounds { b := base; b.SourceAspect = math.NaN(); return b }(),
		"aspect infinity":     func() CropBounds { b := base; b.SourceAspect = math.Inf(1); return b }(),
		"short fingerprint":   func() CropBounds { b := base; b.SourceFingerprint = "short"; return b }(),
		"non-hex fingerprint": func() CropBounds { b := base; b.SourceFingerprint = strings.Repeat("z", 64); return b }(),
	} {
		t.Run(name, func(t *testing.T) {
			assert.False(t, bounds.Valid())
		})
	}
	valid := base
	valid.SourceFingerprint = strings.Repeat("a", 64)
	assert.True(t, valid.Valid())
}
