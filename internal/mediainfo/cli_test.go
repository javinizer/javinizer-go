package mediainfo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCLIProber_Name(t *testing.T) {
	prober := NewCLIProber(nil)
	assert.Equal(t, "mediainfo-cli", prober.Name())
}

func TestCLIProber_CanProbe(t *testing.T) {
	tests := []struct {
		name     string
		enabled  bool
		header   []byte
		expected bool
	}{
		{
			name:     "Enabled - any header",
			enabled:  true,
			header:   []byte{0x00, 0x01, 0x02},
			expected: true,
		},
		{
			name:     "Disabled - any header",
			enabled:  false,
			header:   []byte{0x00, 0x01, 0x02},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &MediaInfoConfig{
				CLIEnabled: tt.enabled,
				CLIPath:    "mediainfo",
				CLITimeout: 30,
			}
			prober := NewCLIProber(cfg)
			result := prober.CanProbe(tt.header)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCLIProber_Probe_Disabled(t *testing.T) {
	cfg := &MediaInfoConfig{
		CLIEnabled: false,
		CLIPath:    "mediainfo",
		CLITimeout: 30,
	}
	prober := NewCLIProber(cfg)

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.mp4")
	err := os.WriteFile(tmpFile, []byte("dummy"), 0644)
	require.NoError(t, err)

	f, err := os.Open(tmpFile)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	_, err = prober.Probe(f)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

func TestNewCLIProber_NilConfig(t *testing.T) {
	prober := NewCLIProber(nil)
	assert.NotNil(t, prober)
	// Should use defaults from DefaultMediaInfoConfig()
	assert.False(t, prober.enabled) // Default is disabled
}

func TestParseMediaInfoJSON_ValidVideo(t *testing.T) {
	jsonData := `{
		"media": {
			"track": [
				{
					"@type": "General",
					"Format": "MPEG-4",
					"Duration": "120000.000",
					"OverallBitRate": "5000000"
				},
				{
					"@type": "Video",
					"Format": "AVC",
					"Width": "1920",
					"Height": "1080",
					"FrameRate": "29.970",
					"Format_Profile": "High"
				},
				{
					"@type": "Audio",
					"Format": "AAC",
					"Channels": "2",
					"SamplingRate": "48000"
				}
			]
		}
	}`

	info, err := parseMediaInfoJSON([]byte(jsonData))

	require.NoError(t, err)
	assert.Equal(t, "mpeg-4", info.Container)
	assert.InDelta(t, 120.0, info.Duration, 0.1)
	assert.Equal(t, 5000, info.Bitrate) // 5Mbps -> 5000 kbps
	assert.Equal(t, "h264", info.VideoCodec)
	assert.Equal(t, 1920, info.Width)
	assert.Equal(t, 1080, info.Height)
	assert.InDelta(t, 29.970, info.FrameRate, 0.001)
	assert.Equal(t, "aac", info.AudioCodec)
	assert.Equal(t, 2, info.AudioChannels)
	assert.Equal(t, 48000, info.SampleRate)
}

func TestParseMediaInfoJSON_InvalidJSON(t *testing.T) {
	invalidJSON := `{invalid json}`

	_, err := parseMediaInfoJSON([]byte(invalidJSON))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse JSON")
}

func TestParseMediaInfoJSON_EmptyTracks(t *testing.T) {
	jsonData := `{
		"media": {
			"track": []
		}
	}`

	info, err := parseMediaInfoJSON([]byte(jsonData))

	require.NoError(t, err)
	assert.NotNil(t, info)
	// All fields should be zero/empty
	assert.Equal(t, "", info.Container)
	assert.Equal(t, 0.0, info.Duration)
}

func TestParseMediaInfoJSON_OnlyGeneral(t *testing.T) {
	jsonData := `{
		"media": {
			"track": [
				{
					"@type": "General",
					"Format": "Matroska",
					"Duration": "3600000.000",
					"OverallBitRate": "2500000"
				}
			]
		}
	}`

	info, err := parseMediaInfoJSON([]byte(jsonData))

	require.NoError(t, err)
	assert.Equal(t, "matroska", info.Container)
	assert.InDelta(t, 3600.0, info.Duration, 0.1)
	assert.Equal(t, 2500, info.Bitrate)
	// No video/audio tracks
	assert.Equal(t, "", info.VideoCodec)
	assert.Equal(t, "", info.AudioCodec)
}

func TestParseMediaInfoJSON_MultipleVideoTracks(t *testing.T) {
	jsonData := `{
		"media": {
			"track": [
				{
					"@type": "General",
					"Format": "AVI"
				},
				{
					"@type": "Video",
					"Format": "AVC",
					"Width": "1920",
					"Height": "1080"
				},
				{
					"@type": "Video",
					"Format": "HEVC",
					"Width": "3840",
					"Height": "2160"
				}
			]
		}
	}`

	info, err := parseMediaInfoJSON([]byte(jsonData))

	require.NoError(t, err)
	// Current implementation uses last video track (HEVC), not first
	// This is consistent with how MediaInfo JSON typically lists tracks
	assert.Equal(t, "h265", info.VideoCodec)
	assert.Equal(t, 3840, info.Width)
	assert.Equal(t, 2160, info.Height)
}

func TestParseInt(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    int
		expectError bool
	}{
		{"Simple integer", "1920", 1920, false},
		{"Integer with spaces", "1 920", 1920, false},
		{"Integer with pixels suffix", "1920 pixels", 1920, false},
		{"Integer with Hz suffix", "48000 Hz", 48000, false},
		{"Decimal notation", "1920.000", 1920, false},
		{"Float with spaces", "48 000.500", 48000, false},
		{"Negative number", "-10", -10, false},
		{"Invalid string", "invalid", 0, true},
		{"Empty string", "", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseInt(tt.input)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestParseFloat(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    float64
		expectError bool
	}{
		{"Simple float", "29.97", 29.97, false},
		{"Integer as float", "30", 30.0, false},
		{"Float with spaces", "29 . 97", 29.97, false},
		{"Scientific notation", "1.5e3", 1500.0, false},
		{"Negative float", "-10.5", -10.5, false},
		{"Invalid string", "invalid", 0.0, true},
		{"Empty string", "", 0.0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseFloat(tt.input)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.InDelta(t, tt.expected, result, 0.01)
			}
		})
	}
}

func TestNormalizeCodecName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"AVC to h264", "AVC", "h264"},
		{"avc lowercase", "avc", "h264"},
		{"AVC with profile", "AVC_High", "h264"},
		{"HEVC to h265", "HEVC", "h265"},
		{"hevc lowercase", "hevc", "h265"},
		{"HEVC with profile", "HEVC_Main", "h265"},
		{"MPEG-4 Visual", "MPEG-4 Visual", "mpeg4"},
		{"MPEG Video", "MPEG Video", "mpeg"},
		{"Spaces to underscores", "Screen Video 2", "screen_video_2"},
		{"Slashes to underscores", "MPEG-4/ISO", "mpeg-4_iso"},
		{"Already normalized", "vp9", "vp9"},
		{"Mixed case", "VP9", "vp9"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeCodecName(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseMediaInfoJSON_MissingFields(t *testing.T) {
	// Test graceful handling when fields are missing
	jsonData := `{
		"media": {
			"track": [
				{
					"@type": "Video",
					"Format": "AVC"
				}
			]
		}
	}`

	info, err := parseMediaInfoJSON([]byte(jsonData))

	require.NoError(t, err)
	assert.Equal(t, "h264", info.VideoCodec)
	// Missing fields should be zero/empty, not cause errors
	assert.Equal(t, 0, info.Width)
	assert.Equal(t, 0, info.Height)
	assert.Equal(t, 0.0, info.FrameRate)
}

func TestParseMediaInfoJSON_InvalidNumberFormats(t *testing.T) {
	// Test resilience to invalid number formats
	jsonData := `{
		"media": {
			"track": [
				{
					"@type": "Video",
					"Format": "AVC",
					"Width": "invalid",
					"Height": "also_invalid",
					"FrameRate": "not_a_number"
				}
			]
		}
	}`

	info, err := parseMediaInfoJSON([]byte(jsonData))

	require.NoError(t, err)
	// Invalid numbers should result in zero values
	assert.Equal(t, 0, info.Width)
	assert.Equal(t, 0, info.Height)
	assert.Equal(t, 0.0, info.FrameRate)
}

func TestParseMediaInfoJSON_VideoCodecWithProfile(t *testing.T) {
	jsonData := `{
		"media": {
			"track": [
				{
					"@type": "Video",
					"Format": "AVC",
					"Format_Profile": "High@L4.1",
					"Width": "1920",
					"Height": "1080"
				}
			]
		}
	}`

	info, err := parseMediaInfoJSON([]byte(jsonData))

	require.NoError(t, err)
	// Should normalize AVC with profile
	assert.Equal(t, "h264", info.VideoCodec)
}

func TestParseMediaInfoJSON_AudioOnlyFile(t *testing.T) {
	jsonData := `{
		"media": {
			"track": [
				{
					"@type": "General",
					"Format": "MPEG Audio",
					"Duration": "180000.000"
				},
				{
					"@type": "Audio",
					"Format": "MPEG Audio",
					"Channels": "2",
					"SamplingRate": "44100",
					"BitRate": "320000"
				}
			]
		}
	}`

	info, err := parseMediaInfoJSON([]byte(jsonData))

	require.NoError(t, err)
	assert.Equal(t, "mpeg audio", info.Container)
	assert.InDelta(t, 180.0, info.Duration, 0.1)
	assert.Equal(t, "mpeg audio", info.AudioCodec)
	assert.Equal(t, 2, info.AudioChannels)
	assert.Equal(t, 44100, info.SampleRate)
	// No video track
	assert.Equal(t, "", info.VideoCodec)
	assert.Equal(t, 0, info.Width)
	assert.Equal(t, 0, info.Height)
}

func TestDefaultMediaInfoConfig(t *testing.T) {
	cfg := DefaultMediaInfoConfig()

	require.NotNil(t, cfg)
	assert.False(t, cfg.CLIEnabled)
	assert.Equal(t, "mediainfo", cfg.CLIPath)
	assert.Equal(t, 30, cfg.CLITimeout)
}

func TestParseInt_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{"Zero", "0", 0},
		{"Leading zeros", "00123", 123},
		{"Negative zero", "-0", 0},
		{"Large number", "999999999", 999999999},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseInt(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseFloat_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected float64
	}{
		{"Zero", "0.0", 0.0},
		{"Very small", "0.001", 0.001},
		{"Very large", "999999.99", 999999.99},
		{"Negative small", "-0.001", -0.001},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseFloat(tt.input)
			require.NoError(t, err)
			assert.InDelta(t, tt.expected, result, 0.0001)
		})
	}
}
