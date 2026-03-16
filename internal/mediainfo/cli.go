package mediainfo

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// CLIProber implements the Prober interface using MediaInfo CLI
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

	// Get file path
	filePath := f.Name()

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(p.timeout)*time.Second)
	defer cancel()

	// Execute mediainfo with JSON output
	cmd := exec.CommandContext(ctx, p.path, "--Output=JSON", filePath)
	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("mediainfo command timed out after %d seconds", p.timeout)
		}
		return nil, fmt.Errorf("mediainfo command failed: %w", err)
	}

	// Parse JSON output
	return parseMediaInfoJSON(output)
}

// MediaInfo JSON structure (simplified, only fields we need)
type mediaInfoJSON struct {
	Media struct {
		Track []struct {
			Type string `json:"@type"`

			// General track
			Format         string `json:"Format"`
			Duration       string `json:"Duration"`
			OverallBitRate string `json:"OverallBitRate"`
			FileSize       string `json:"FileSize"`

			// Video track
			Width              string `json:"Width"`
			Height             string `json:"Height"`
			FrameRate          string `json:"FrameRate"`
			Format_Profile     string `json:"Format_Profile"`
			CodecID            string `json:"CodecID"`
			BitRate            string `json:"BitRate"`
			DisplayAspectRatio string `json:"DisplayAspectRatio"`

			// Audio track
			Channels       string `json:"Channels"`
			SamplingRate   string `json:"SamplingRate"`
			BitRate_Mode   string `json:"BitRate_Mode"`
			Format_Version string `json:"Format_Version"`
		} `json:"track"`
	} `json:"media"`
}

// parseMediaInfoJSON parses MediaInfo JSON output
func parseMediaInfoJSON(data []byte) (*VideoInfo, error) {
	var miJSON mediaInfoJSON
	if err := json.Unmarshal(data, &miJSON); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	info := &VideoInfo{}

	// Process tracks
	for _, track := range miJSON.Media.Track {
		switch track.Type {
		case "General":
			// Container format
			info.Container = strings.ToLower(track.Format)

			// Duration
			if track.Duration != "" {
				if duration, err := parseFloat(track.Duration); err == nil {
					info.Duration = duration / 1000.0 // Convert ms to seconds
				}
			}

			// Overall bitrate
			if track.OverallBitRate != "" {
				if bitrate, err := parseInt(track.OverallBitRate); err == nil {
					info.Bitrate = bitrate / 1000 // Convert bps to kbps
				}
			}

		case "Video":
			// Video codec
			codec := strings.ToLower(track.Format)
			if track.Format_Profile != "" {
				codec = fmt.Sprintf("%s_%s", codec, strings.ToLower(track.Format_Profile))
			}
			info.VideoCodec = normalizeCodecName(codec)

			// Dimensions
			if track.Width != "" {
				if width, err := parseInt(track.Width); err == nil {
					info.Width = width
				}
			}
			if track.Height != "" {
				if height, err := parseInt(track.Height); err == nil {
					info.Height = height
				}
			}

			// Frame rate
			if track.FrameRate != "" {
				if frameRate, err := parseFloat(track.FrameRate); err == nil {
					info.FrameRate = frameRate
				}
			}

		case "Audio":
			// Audio codec
			info.AudioCodec = strings.ToLower(track.Format)

			// Channels
			if track.Channels != "" {
				if channels, err := parseInt(track.Channels); err == nil {
					info.AudioChannels = channels
				}
			}

			// Sample rate
			if track.SamplingRate != "" {
				if sampleRate, err := parseInt(track.SamplingRate); err == nil {
					info.SampleRate = sampleRate
				}
			}
		}
	}

	return info, nil
}

// parseInt parses string to int, handling various formats
func parseInt(s string) (int, error) {
	// Remove spaces and common suffixes
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "pixels", "")
	s = strings.ReplaceAll(s, "Hz", "")

	// Handle decimal notation (e.g., "1 920" or "1920.000")
	if strings.Contains(s, ".") {
		f, err := strconv.ParseFloat(s, 64)
		return int(f), err
	}

	return strconv.Atoi(s)
}

// parseFloat parses string to float64
func parseFloat(s string) (float64, error) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, " ", "")
	return strconv.ParseFloat(s, 64)
}

// normalizeCodecName normalizes codec names from MediaInfo
func normalizeCodecName(codec string) string {
	codec = strings.ToLower(codec)
	codec = strings.ReplaceAll(codec, " ", "_")
	codec = strings.ReplaceAll(codec, "/", "_")

	// Common mappings
	switch {
	case strings.Contains(codec, "avc"):
		return "h264"
	case strings.Contains(codec, "hevc"):
		return "h265"
	case strings.Contains(codec, "mpeg-4_visual"):
		return "mpeg4"
	case strings.Contains(codec, "mpeg_video"):
		return "mpeg"
	}

	return codec
}
