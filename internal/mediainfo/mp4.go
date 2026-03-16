package mediainfo

import (
	"fmt"
	"os"

	"github.com/Eyevinn/mp4ff/mp4"
)

// MP4Prober implements the Prober interface for MP4 containers
type MP4Prober struct{}

// NewMP4Prober creates a new MP4 prober
func NewMP4Prober() *MP4Prober {
	return &MP4Prober{}
}

// Name returns the prober identifier
func (p *MP4Prober) Name() string {
	return "mp4"
}

// CanProbe checks if this prober can handle the file based on header
func (p *MP4Prober) CanProbe(header []byte) bool {
	// MP4/MOV: contains "ftyp" in first 12 bytes (at offset 4-7)
	if len(header) >= 8 {
		return header[4] == 'f' && header[5] == 't' && header[6] == 'y' && header[7] == 'p'
	}
	return false
}

// Probe extracts metadata from the MP4 file
func (p *MP4Prober) Probe(f *os.File) (*VideoInfo, error) {
	return analyzeMP4(f)
}

// analyzeMP4 extracts metadata from MP4/MOV files
func analyzeMP4(f *os.File) (*VideoInfo, error) {
	info := &VideoInfo{
		Container: "mp4",
	}

	// Parse MP4 file
	mp4File, err := mp4.DecodeFile(f)
	if err != nil {
		return nil, fmt.Errorf("failed to parse MP4 file: %w", err)
	}

	// Get file size for bitrate calculation
	stat, _ := f.Stat()
	fileSize := stat.Size()

	// Extract movie-level duration (in movie timescale)
	var movieDuration uint64
	var movieTimescale uint64
	if mp4File.Moov != nil && mp4File.Moov.Mvhd != nil {
		movieDuration = mp4File.Moov.Mvhd.Duration
		movieTimescale = uint64(mp4File.Moov.Mvhd.Timescale)
	}

	// Iterate through tracks to find video and audio
	if mp4File.Moov != nil {
		for _, trak := range mp4File.Moov.Traks {
			if trak.Mdia == nil || trak.Mdia.Hdlr == nil {
				continue
			}

			handlerType := trak.Mdia.Hdlr.HandlerType

			// Video track
			if handlerType == "vide" {
				if err := extractMP4VideoInfo(trak, info); err == nil {
					// Calculate duration from track if not set
					if trak.Mdia.Mdhd != nil && info.Duration == 0 {
						trackDuration := trak.Mdia.Mdhd.Duration
						trackTimescale := trak.Mdia.Mdhd.Timescale
						if trackTimescale > 0 {
							info.Duration = float64(trackDuration) / float64(trackTimescale)
						}
					}
				}
			}

			// Audio track
			if handlerType == "soun" {
				_ = extractMP4AudioInfo(trak, info)
			}
		}
	}

	// Fallback to movie-level duration if track duration not found
	if info.Duration == 0 && movieTimescale > 0 {
		info.Duration = float64(movieDuration) / float64(movieTimescale)
	}

	// Calculate bitrate (file size in bits / duration in seconds)
	if info.Duration > 0 && fileSize > 0 {
		info.Bitrate = int((float64(fileSize) * 8) / info.Duration / 1000) // kbps
	}

	// Calculate aspect ratio
	if info.Width > 0 && info.Height > 0 {
		info.AspectRatio = float64(info.Width) / float64(info.Height)
	}

	return info, nil
}

// extractMP4VideoInfo extracts video information from a video track
func extractMP4VideoInfo(trak *mp4.TrakBox, info *VideoInfo) error {
	// Get video sample description
	if trak.Mdia == nil || trak.Mdia.Minf == nil || trak.Mdia.Minf.Stbl == nil || trak.Mdia.Minf.Stbl.Stsd == nil {
		return fmt.Errorf("missing sample description")
	}

	stsd := trak.Mdia.Minf.Stbl.Stsd

	// Check for visual sample entries
	for _, child := range stsd.Children {
		switch entry := child.(type) {
		case *mp4.VisualSampleEntryBox:
			// Extract codec
			info.VideoCodec = mapMP4VideoCodec(entry.Type())

			// Extract dimensions
			info.Width = int(entry.Width)
			info.Height = int(entry.Height)

			// Check for avcC (H.264) or hvcC (H.265) configuration
			for _, avcChild := range entry.Children {
				switch avcChild.Type() {
				case "avcC":
					info.VideoCodec = "h264"
				case "hvcC":
					info.VideoCodec = "hevc"
				case "vpcC":
					info.VideoCodec = "vp9"
				}
			}

			return nil
		}
	}

	return fmt.Errorf("no visual sample entry found")
}

// extractMP4AudioInfo extracts audio information from an audio track
func extractMP4AudioInfo(trak *mp4.TrakBox, info *VideoInfo) error {
	// Get audio sample description
	if trak.Mdia == nil || trak.Mdia.Minf == nil || trak.Mdia.Minf.Stbl == nil || trak.Mdia.Minf.Stbl.Stsd == nil {
		return fmt.Errorf("missing sample description")
	}

	stsd := trak.Mdia.Minf.Stbl.Stsd

	// Check for audio sample entries
	for _, child := range stsd.Children {
		switch entry := child.(type) {
		case *mp4.AudioSampleEntryBox:
			// Extract codec
			info.AudioCodec = mapMP4AudioCodec(entry.Type())

			// Extract audio properties
			info.AudioChannels = int(entry.ChannelCount)
			info.SampleRate = int(entry.SampleRate)

			// Check for esds (Elementary Stream Descriptor) for more details
			for _, audioChild := range entry.Children {
				if audioChild.Type() == "esds" {
					// AAC codec
					info.AudioCodec = "aac"
				}
			}

			return nil
		}
	}

	return fmt.Errorf("no audio sample entry found")
}

// mapMP4VideoCodec maps MP4 codec FourCC to human-readable codec name
func mapMP4VideoCodec(fourcc string) string {
	switch fourcc {
	case "avc1", "avc3":
		return "h264"
	case "hvc1", "hev1":
		return "hevc"
	case "vp09":
		return "vp9"
	case "vp08":
		return "vp8"
	case "av01":
		return "av1"
	case "mp4v":
		return "mpeg4"
	default:
		return fourcc
	}
}

// mapMP4AudioCodec maps MP4 audio codec FourCC to human-readable name
func mapMP4AudioCodec(fourcc string) string {
	switch fourcc {
	case "mp4a":
		return "aac" // Most common for mp4a
	case ".mp3", "mp3 ":
		return "mp3"
	case "ac-3":
		return "ac3"
	case "ec-3":
		return "eac3"
	case "opus":
		return "opus"
	case "fLaC":
		return "flac"
	default:
		return fourcc
	}
}
