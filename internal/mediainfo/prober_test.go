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

// Note: TestAVIProber_CanProbe, TestFLVProber_CanProbe, TestCLIProber_CanProbe,
// and TestDefaultMediaInfoConfig are in their respective dedicated test files:
// avi_test.go, flv_test.go, and cli_test.go
