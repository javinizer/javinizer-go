package mediainfo

import (
	"testing"
)

func TestProberRegistry_Register(t *testing.T) {
	cfg := DefaultMediaInfoConfig()
	registry := NewProberRegistry(cfg)

	// Should have all native probers registered
	expectedCount := 5 // MP4, MKV, MOV, AVI, FLV
	if len(registry.probers) != expectedCount {
		t.Errorf("Expected %d probers, got %d", expectedCount, len(registry.probers))
	}
}

func TestProberRegistry_FindProber(t *testing.T) {
	cfg := DefaultMediaInfoConfig()
	registry := NewProberRegistry(cfg)

	tests := []struct {
		name     string
		header   []byte
		expected string // Expected prober name, or "" if none
	}{
		{
			name:     "MP4 header",
			header:   []byte{0x00, 0x00, 0x00, 0x20, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'},
			expected: "mp4",
		},
		{
			name:     "MKV header",
			header:   []byte{0x1A, 0x45, 0xDF, 0xA3, 0x00, 0x00, 0x00, 0x00},
			expected: "mkv",
		},
		{
			name:     "MOV header (QuickTime)",
			header:   []byte{0x00, 0x00, 0x00, 0x20, 'f', 't', 'y', 'p', 'q', 't', ' ', ' '},
			expected: "mov",
		},
		{
			name:     "AVI header",
			header:   []byte{'R', 'I', 'F', 'F', 0x00, 0x00, 0x00, 0x00, 'A', 'V', 'I', ' '},
			expected: "avi",
		},
		{
			name:     "FLV header",
			header:   []byte{'F', 'L', 'V', 0x01, 0x05, 0x00, 0x00, 0x00},
			expected: "flv",
		},
		{
			name:     "Unknown header",
			header:   []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prober := registry.FindProber(tt.header)
			if tt.expected == "" {
				if prober != nil {
					t.Errorf("Expected no prober, but got %s", prober.Name())
				}
			} else {
				if prober == nil {
					t.Errorf("Expected prober %s, but got nil", tt.expected)
				} else if prober.Name() != tt.expected {
					t.Errorf("Expected prober %s, got %s", tt.expected, prober.Name())
				}
			}
		})
	}
}

func TestMP4Prober_CanProbe(t *testing.T) {
	prober := NewMP4Prober()

	tests := []struct {
		name     string
		header   []byte
		expected bool
	}{
		{
			name:     "Valid MP4 header",
			header:   []byte{0x00, 0x00, 0x00, 0x20, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'},
			expected: true,
		},
		{
			name:     "Valid MP4 header (minimal)",
			header:   []byte{0x00, 0x00, 0x00, 0x20, 'f', 't', 'y', 'p'},
			expected: true,
		},
		{
			name:     "Invalid - too short",
			header:   []byte{0x00, 0x00, 0x00, 0x20, 'f', 't', 'y'},
			expected: false,
		},
		{
			name:     "Invalid - wrong signature",
			header:   []byte{0x00, 0x00, 0x00, 0x20, 'x', 'x', 'x', 'x'},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := prober.CanProbe(tt.header)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestMKVProber_CanProbe(t *testing.T) {
	prober := NewMKVProber()

	tests := []struct {
		name     string
		header   []byte
		expected bool
	}{
		{
			name:     "Valid MKV header",
			header:   []byte{0x1A, 0x45, 0xDF, 0xA3, 0x00, 0x00, 0x00, 0x00},
			expected: true,
		},
		{
			name:     "Valid MKV header (minimal)",
			header:   []byte{0x1A, 0x45, 0xDF, 0xA3},
			expected: true,
		},
		{
			name:     "Invalid - too short",
			header:   []byte{0x1A, 0x45, 0xDF},
			expected: false,
		},
		{
			name:     "Invalid - wrong signature",
			header:   []byte{0x00, 0x00, 0x00, 0x00},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := prober.CanProbe(tt.header)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestMOVProber_CanProbe(t *testing.T) {
	prober := NewMOVProber()

	tests := []struct {
		name     string
		header   []byte
		expected bool
	}{
		{
			name:     "QuickTime brand (qt  )",
			header:   []byte{0x00, 0x00, 0x00, 0x20, 'f', 't', 'y', 'p', 'q', 't', ' ', ' '},
			expected: true,
		},
		{
			name:     "Apple iTunes video (M4V )",
			header:   []byte{0x00, 0x00, 0x00, 0x20, 'f', 't', 'y', 'p', 'M', '4', 'V', ' '},
			expected: true,
		},
		{
			name:     "Apple iTunes audio (M4A )",
			header:   []byte{0x00, 0x00, 0x00, 0x20, 'f', 't', 'y', 'p', 'M', '4', 'A', ' '},
			expected: true,
		},
		{
			name:     "Apple iTunes book (M4B )",
			header:   []byte{0x00, 0x00, 0x00, 0x20, 'f', 't', 'y', 'p', 'M', '4', 'B', ' '},
			expected: true,
		},
		{
			name:     "Flash video (F4V )",
			header:   []byte{0x00, 0x00, 0x00, 0x20, 'f', 't', 'y', 'p', 'F', '4', 'V', ' '},
			expected: true,
		},
		{
			name:     "Invalid - regular MP4 brand (isom)",
			header:   []byte{0x00, 0x00, 0x00, 0x20, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'},
			expected: false,
		},
		{
			name:     "Invalid - no ftyp box",
			header:   []byte{0x00, 0x00, 0x00, 0x20, 'x', 'x', 'x', 'x', 'q', 't', ' ', ' '},
			expected: false,
		},
		{
			name:     "Invalid - too short",
			header:   []byte{0x00, 0x00, 0x00, 0x20, 'f', 't', 'y', 'p', 'q', 't'},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := prober.CanProbe(tt.header)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v for header %v", tt.expected, result, tt.header)
			}
		})
	}
}

func TestAVIProber_CanProbe(t *testing.T) {
	prober := NewAVIProber()

	tests := []struct {
		name     string
		header   []byte
		expected bool
	}{
		{
			name:     "Valid AVI header",
			header:   []byte{'R', 'I', 'F', 'F', 0x00, 0x00, 0x00, 0x00, 'A', 'V', 'I', ' '},
			expected: true,
		},
		{
			name:     "Invalid - RIFF but not AVI",
			header:   []byte{'R', 'I', 'F', 'F', 0x00, 0x00, 0x00, 0x00, 'W', 'A', 'V', 'E'},
			expected: false,
		},
		{
			name:     "Invalid - too short",
			header:   []byte{'R', 'I', 'F', 'F', 0x00, 0x00},
			expected: false,
		},
		{
			name:     "Invalid - wrong signature",
			header:   []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 'A', 'V', 'I', ' '},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := prober.CanProbe(tt.header)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestFLVProber_CanProbe(t *testing.T) {
	prober := NewFLVProber()

	tests := []struct {
		name     string
		header   []byte
		expected bool
	}{
		{
			name:     "Valid FLV header",
			header:   []byte{'F', 'L', 'V', 0x01, 0x05},
			expected: true,
		},
		{
			name:     "Valid FLV header (minimal)",
			header:   []byte{'F', 'L', 'V'},
			expected: true,
		},
		{
			name:     "Invalid - too short",
			header:   []byte{'F', 'L'},
			expected: false,
		},
		{
			name:     "Invalid - wrong signature",
			header:   []byte{'X', 'X', 'X'},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := prober.CanProbe(tt.header)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestCLIProber_CanProbe(t *testing.T) {
	// CLI enabled
	enabledProber := NewCLIProber(&MediaInfoConfig{
		CLIEnabled: true,
		CLIPath:    "mediainfo",
		CLITimeout: 30,
	})

	if !enabledProber.CanProbe([]byte{0x00}) {
		t.Error("CLI prober should accept any header when enabled")
	}

	// CLI disabled
	disabledProber := NewCLIProber(&MediaInfoConfig{
		CLIEnabled: false,
		CLIPath:    "mediainfo",
		CLITimeout: 30,
	})

	if disabledProber.CanProbe([]byte{0x00}) {
		t.Error("CLI prober should reject when disabled")
	}
}

func TestDefaultMediaInfoConfig(t *testing.T) {
	cfg := DefaultMediaInfoConfig()

	if cfg.CLIEnabled {
		t.Error("CLI should be disabled by default")
	}

	if cfg.CLIPath != "mediainfo" {
		t.Errorf("Expected default CLI path 'mediainfo', got '%s'", cfg.CLIPath)
	}

	if cfg.CLITimeout != 30 {
		t.Errorf("Expected default timeout 30, got %d", cfg.CLITimeout)
	}
}

func TestMapMOVVideoCodec(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"apch", "prores_422_hq"},
		{"apcn", "prores_422"},
		{"apcs", "prores_422_lt"},
		{"apco", "prores_422_proxy"},
		{"ap4h", "prores_4444"},
		{"ap4x", "prores_4444_xq"},
		{"dvcp", "dvcpro"},
		{"dvc ", "dv"},
		{"mjp2", "jpeg2000"},
		{"jpeg", "mjpeg"},
		{"SVQ1", "sorenson_video_1"},
		{"SVQ3", "sorenson_video_3"},
		{"mp4v", "mpeg4"},
		{"unknown", "unknown"}, // Pass-through
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := mapMOVVideoCodec(tt.input)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestMapMOVAudioCodec(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"sowt", "pcm_s16le"},
		{"twos", "pcm_s16be"},
		{"in24", "pcm_s24le"},
		{"fl32", "pcm_f32le"},
		{"ulaw", "pcm_mulaw"},
		{"alaw", "pcm_alaw"},
		{"ima4", "adpcm_ima_qt"},
		{"alac", "alac"},
		{"QDM2", "qdm2"},
		{"unknown", "unknown"}, // Pass-through
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := mapMOVAudioCodec(tt.input)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}
