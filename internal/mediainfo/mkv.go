package mediainfo

import (
	"fmt"
	"os"
	"strings"

	"github.com/at-wat/ebml-go"
)

// MKVProber implements the Prober interface for MKV/WebM containers
type MKVProber struct{}

// NewMKVProber creates a new MKV prober
func NewMKVProber() *MKVProber {
	return &MKVProber{}
}

// Name returns the prober identifier
func (p *MKVProber) Name() string {
	return "mkv"
}

// CanProbe checks if this prober can handle the file based on header
func (p *MKVProber) CanProbe(header []byte) bool {
	// MKV/WebM: EBML header starts with 0x1A 0x45 0xDF 0xA3
	if len(header) >= 4 {
		return header[0] == 0x1A && header[1] == 0x45 && header[2] == 0xDF && header[3] == 0xA3
	}
	return false
}

// Probe extracts metadata from the MKV file
func (p *MKVProber) Probe(f *os.File) (*VideoInfo, error) {
	return analyzeMKV(f)
}

// Matroska track types
const (
	trackTypeVideo    = 1
	trackTypeAudio    = 2
	trackTypeSubtitle = 17
)

// analyzeMKV extracts metadata from MKV/WebM files
func analyzeMKV(f *os.File) (*VideoInfo, error) {
	info := &VideoInfo{
		Container: "mkv",
	}

	// Get file size for bitrate calculation
	stat, _ := f.Stat()
	fileSize := stat.Size()

	// Parse EBML structure
	var ebmlDoc struct {
		Header struct {
			DocType        string `ebml:"DocType"`
			DocTypeVersion uint64 `ebml:"DocTypeVersion"`
		} `ebml:"EBML"`
		Segment []struct {
			Info struct {
				TimecodeScale uint64  `ebml:"TimecodeScale,omitempty"`
				Duration      float64 `ebml:"Duration,omitempty"`
				Title         string  `ebml:"Title,omitempty"`
			} `ebml:"Info,omitempty"`
			Tracks struct {
				TrackEntry []struct {
					TrackNumber uint64 `ebml:"TrackNumber"`
					TrackType   uint64 `ebml:"TrackType"`
					CodecID     string `ebml:"CodecID,omitempty"`
					Video       struct {
						PixelWidth  uint64 `ebml:"PixelWidth,omitempty"`
						PixelHeight uint64 `ebml:"PixelHeight,omitempty"`
					} `ebml:"Video,omitempty"`
					Audio struct {
						SamplingFrequency float64 `ebml:"SamplingFrequency,omitempty"`
						Channels          uint64  `ebml:"Channels,omitempty"`
					} `ebml:"Audio,omitempty"`
				} `ebml:"TrackEntry"`
			} `ebml:"Tracks,omitempty"`
		} `ebml:"Segment"`
	}

	// Unmarshal EBML
	if err := ebml.Unmarshal(f, &ebmlDoc); err != nil {
		// Try to extract partial information even if full parse fails
		return extractMKVPartial(f, info, fileSize)
	}

	// Extract segment information
	if len(ebmlDoc.Segment) > 0 {
		seg := ebmlDoc.Segment[0]

		// Extract duration
		if seg.Info.Duration > 0 {
			// Duration is in timecode scale units (nanoseconds by default)
			timecodeScale := seg.Info.TimecodeScale
			if timecodeScale == 0 {
				timecodeScale = 1000000 // Default: 1ms
			}
			// Convert to seconds
			info.Duration = seg.Info.Duration * float64(timecodeScale) / 1000000000.0
		}

		// Extract track information
		for _, track := range seg.Tracks.TrackEntry {
			switch track.TrackType {
			case trackTypeVideo:
				// Video track
				info.VideoCodec = mapMKVVideoCodec(track.CodecID)
				info.Width = int(track.Video.PixelWidth)
				info.Height = int(track.Video.PixelHeight)

			case trackTypeAudio:
				// Audio track (take first audio track)
				if info.AudioCodec == "" {
					info.AudioCodec = mapMKVAudioCodec(track.CodecID)
					info.SampleRate = int(track.Audio.SamplingFrequency)
					info.AudioChannels = int(track.Audio.Channels)
					if info.AudioChannels == 0 {
						info.AudioChannels = 2 // Default to stereo if not specified
					}
				}
			}
		}
	}

	// Calculate bitrate
	if info.Duration > 0 && fileSize > 0 {
		info.Bitrate = int((float64(fileSize) * 8) / info.Duration / 1000) // kbps
	}

	// Calculate aspect ratio
	if info.Width > 0 && info.Height > 0 {
		info.AspectRatio = float64(info.Width) / float64(info.Height)
	}

	return info, nil
}

// extractMKVPartial attempts to extract partial information if full EBML parse fails
func extractMKVPartial(f *os.File, info *VideoInfo, fileSize int64) (*VideoInfo, error) {
	// Reset file pointer
	_, _ = f.Seek(0, 0)

	// If full parse failed, we can't easily do partial parsing with ebml-go
	// Return basic error - users should use properly formatted MKV files
	// In future, could implement manual EBML element parsing here

	if info.Width == 0 && info.Height == 0 {
		return nil, fmt.Errorf("failed to extract MKV metadata: file may be corrupted or using unsupported features")
	}

	// Calculate bitrate if we have partial data
	if info.Duration > 0 && fileSize > 0 {
		info.Bitrate = int((float64(fileSize) * 8) / info.Duration / 1000)
	}

	// Calculate aspect ratio
	if info.Width > 0 && info.Height > 0 {
		info.AspectRatio = float64(info.Width) / float64(info.Height)
	}

	return info, nil
}

// mapMKVVideoCodec maps Matroska video codec ID to human-readable name
func mapMKVVideoCodec(codecID string) string {
	// Matroska codec IDs are like "V_MPEG4/ISO/AVC", "V_MPEGH/ISO/HEVC", etc.
	codecID = strings.ToUpper(codecID)

	if strings.Contains(codecID, "AVC") || strings.Contains(codecID, "H264") {
		return "h264"
	}
	if strings.Contains(codecID, "HEVC") || strings.Contains(codecID, "H265") {
		return "hevc"
	}
	if strings.Contains(codecID, "VP9") {
		return "vp9"
	}
	if strings.Contains(codecID, "VP8") {
		return "vp8"
	}
	if strings.Contains(codecID, "AV1") {
		return "av1"
	}
	if strings.Contains(codecID, "MPEG4") {
		return "mpeg4"
	}
	if strings.Contains(codecID, "THEORA") {
		return "theora"
	}

	// Return the codec ID itself if we can't map it
	return strings.TrimPrefix(codecID, "V_")
}

// mapMKVAudioCodec maps Matroska audio codec ID to human-readable name
func mapMKVAudioCodec(codecID string) string {
	// Matroska audio codec IDs are like "A_AAC", "A_MPEG/L3" (MP3), "A_AC3", etc.
	codecID = strings.ToUpper(codecID)

	if strings.Contains(codecID, "AAC") {
		return "aac"
	}
	if strings.Contains(codecID, "MP3") || strings.Contains(codecID, "MPEG/L3") {
		return "mp3"
	}
	if strings.Contains(codecID, "AC3") && !strings.Contains(codecID, "EAC3") {
		return "ac3"
	}
	if strings.Contains(codecID, "EAC3") || strings.Contains(codecID, "E-AC-3") {
		return "eac3"
	}
	if strings.Contains(codecID, "DTS") {
		return "dts"
	}
	if strings.Contains(codecID, "OPUS") {
		return "opus"
	}
	if strings.Contains(codecID, "VORBIS") {
		return "vorbis"
	}
	if strings.Contains(codecID, "FLAC") {
		return "flac"
	}
	if strings.Contains(codecID, "PCM") {
		return "pcm"
	}

	// Return the codec ID itself if we can't map it
	return strings.TrimPrefix(codecID, "A_")
}
