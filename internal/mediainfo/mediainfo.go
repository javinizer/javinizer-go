package mediainfo

import (
	"fmt"
	"os"
)

// VideoInfo contains metadata extracted from a video file
type VideoInfo struct {
	// Video properties
	VideoCodec  string  // "h264", "hevc", "vp9", etc.
	Width       int     // Video width in pixels (e.g., 1920)
	Height      int     // Video height in pixels (e.g., 1080)
	Duration    float64 // Duration in seconds
	Bitrate     int     // Bitrate in kbps (computed from file size and duration)
	AspectRatio float64 // Aspect ratio (computed from width/height)
	FrameRate   float64 // Frames per second

	// Audio properties
	AudioCodec    string // "aac", "mp3", "ac3", etc.
	AudioChannels int    // Number of audio channels (2 = stereo, 6 = 5.1)
	SampleRate    int    // Audio sample rate (e.g., 48000, 44100)

	// Container
	Container string // "mp4", "mkv", "avi", etc.
}

// Analyze extracts metadata from a video file using the ProberRegistry
// Supports: MP4, MKV, MOV, AVI, FLV
// Falls back to MediaInfo CLI if enabled and native parsers fail
// Returns partial info if some fields are unavailable
func Analyze(filePath string) (*VideoInfo, error) {
	return AnalyzeWithConfig(filePath, nil)
}

// AnalyzeWithConfig extracts metadata using custom configuration
func AnalyzeWithConfig(filePath string, cfg *MediaInfoConfig) (*VideoInfo, error) {
	// Open file
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer func() { _ = f.Close() }()

	// Initialize registry if needed
	if cfg == nil {
		cfg = DefaultMediaInfoConfig()
	}
	registry := NewProberRegistry(cfg)

	// Use registry to probe with fallback
	info, err := registry.ProbeWithFallback(f)
	if err != nil {
		return nil, err
	}

	// Compute aspect ratio if dimensions available
	if info.Width > 0 && info.Height > 0 {
		info.AspectRatio = float64(info.Width) / float64(info.Height)
	}

	return info, nil
}

// detectContainer detects the container format from file header
func detectContainer(header []byte) string {
	// MP4/MOV: contains "ftyp" in first 12 bytes
	if len(header) >= 8 {
		// Check for ftyp box (byte 4-7)
		if header[4] == 'f' && header[5] == 't' && header[6] == 'y' && header[7] == 'p' {
			return "mp4"
		}
	}

	// MKV/WebM: EBML header starts with 0x1A 0x45 0xDF 0xA3
	if len(header) >= 4 {
		if header[0] == 0x1A && header[1] == 0x45 && header[2] == 0xDF && header[3] == 0xA3 {
			return "mkv"
		}
	}

	// FLV: starts with "FLV"
	if len(header) >= 3 {
		if header[0] == 'F' && header[1] == 'L' && header[2] == 'V' {
			return "flv"
		}
	}

	// AVI: starts with "RIFF" and contains "AVI " at offset 8
	if len(header) >= 12 {
		if header[0] == 'R' && header[1] == 'I' && header[2] == 'F' && header[3] == 'F' &&
			header[8] == 'A' && header[9] == 'V' && header[10] == 'I' {
			return "avi"
		}
	}

	return "unknown"
}

// GetResolution returns human-readable resolution string
// Examples: "4K", "1080p", "720p", "SD"
func (v *VideoInfo) GetResolution() string {
	if v.Height >= 2160 {
		return "4K"
	} else if v.Height >= 1080 {
		return "1080p"
	} else if v.Height >= 720 {
		return "720p"
	} else if v.Height >= 480 {
		return "480p"
	}
	return "SD"
}

// GetAudioChannelDescription returns human-readable audio channel description
// Examples: "Stereo", "5.1", "7.1"
func (v *VideoInfo) GetAudioChannelDescription() string {
	switch v.AudioChannels {
	case 1:
		return "Mono"
	case 2:
		return "Stereo"
	case 6:
		return "5.1"
	case 8:
		return "7.1"
	default:
		return fmt.Sprintf("%d channels", v.AudioChannels)
	}
}
