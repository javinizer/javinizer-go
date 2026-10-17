package organizer

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestPreservesOriginalFilenames(t *testing.T) {
	for _, tc := range []struct {
		name        string
		cfg         *Config
		force, want bool
	}{
		{name: "missing config"},
		{name: "preserve", cfg: &Config{}, want: true},
		{name: "configured rename", cfg: &Config{RenameFile: true}},
		{name: "forced rename", cfg: &Config{}, force: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := &Organizer{config: tc.cfg}
			assert.Equal(t, tc.want, o.PreservesOriginalFilenames(tc.force))
		})
	}
}
