package mediainfo

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// AVIProber implements the Prober interface for AVI containers
type AVIProber struct{}

// NewAVIProber creates a new AVI prober
func NewAVIProber() *AVIProber {
	return &AVIProber{}
}

// Name returns the prober identifier
func (p *AVIProber) Name() string {
	return "avi"
}

// CanProbe checks if this prober can handle the file based on header
func (p *AVIProber) CanProbe(header []byte) bool {
	// AVI: starts with "RIFF" and contains "AVI " at offset 8
	if len(header) >= 12 {
		return header[0] == 'R' && header[1] == 'I' && header[2] == 'F' && header[3] == 'F' &&
			header[8] == 'A' && header[9] == 'V' && header[10] == 'I'
	}
	return false
}

// Probe extracts metadata from the AVI file
func (p *AVIProber) Probe(f *os.File) (*VideoInfo, error) {
	return analyzeAVI(f)
}

// RIFF chunk header
type riffChunk struct {
	FourCC [4]byte
	Size   uint32
}

// AVI Main Header (avih chunk)
type aviMainHeader struct {
	MicroSecPerFrame    uint32
	MaxBytesPerSec      uint32
	PaddingGranularity  uint32
	Flags               uint32
	TotalFrames         uint32
	InitialFrames       uint32
	Streams             uint32
	SuggestedBufferSize uint32
	Width               uint32
	Height              uint32
	Reserved            [4]uint32
}

// AVI Stream Header (strh chunk)
type aviStreamHeader struct {
	Type                [4]byte
	Handler             [4]byte
	Flags               uint32
	Priority            uint16
	Language            uint16
	InitialFrames       uint32
	Scale               uint32
	Rate                uint32
	Start               uint32
	Length              uint32
	SuggestedBufferSize uint32
	Quality             uint32
	SampleSize          uint32
	Frame               [4]uint16
}

// analyzeAVI parses AVI/RIFF container
func analyzeAVI(f *os.File) (*VideoInfo, error) {
	_, err := f.Seek(0, io.SeekStart)
	if err != nil {
		return nil, fmt.Errorf("seek failed: %w", err)
	}

	info := &VideoInfo{
		Container: "avi",
	}

	// Read RIFF header
	var riffHeader riffChunk
	if err := binary.Read(f, binary.LittleEndian, &riffHeader); err != nil {
		return nil, fmt.Errorf("failed to read RIFF header: %w", err)
	}

	// Verify RIFF signature
	if string(riffHeader.FourCC[:]) != "RIFF" {
		return nil, fmt.Errorf("invalid RIFF signature")
	}

	// Read form type (should be "AVI ")
	var formType [4]byte
	if err := binary.Read(f, binary.LittleEndian, &formType); err != nil {
		return nil, fmt.Errorf("failed to read form type: %w", err)
	}

	if string(formType[:]) != "AVI " {
		return nil, fmt.Errorf("not an AVI file")
	}

	// Parse chunks
	videoStreamFound := false
	audioStreamFound := false

	for {
		var chunk riffChunk
		if err := binary.Read(f, binary.LittleEndian, &chunk); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("failed to read chunk: %w", err)
		}

		fourCC := string(chunk.FourCC[:])
		currentPos, _ := f.Seek(0, io.SeekCurrent)

		switch fourCC {
		case "LIST":
			// Read list type
			var listType [4]byte
			if err := binary.Read(f, binary.LittleEndian, &listType); err != nil {
				return nil, fmt.Errorf("failed to read list type: %w", err)
			}

			listTypeStr := string(listType[:])

			switch listTypeStr {
			case "hdrl":
				// Header list - contains avih
				if err := parseHdrlList(f, info, currentPos+4, chunk.Size-4); err != nil {
					return nil, err
				}
			case "strl":
				// Stream list - contains strh and strf
				streamInfo, err := parseStrlList(f, currentPos+4, chunk.Size-4)
				if err != nil {
					return nil, err
				}

				if streamInfo.isVideo && !videoStreamFound {
					info.VideoCodec = streamInfo.codec
					info.Width = streamInfo.width
					info.Height = streamInfo.height
					info.FrameRate = streamInfo.frameRate
					videoStreamFound = true
				} else if streamInfo.isAudio && !audioStreamFound {
					info.AudioCodec = streamInfo.codec
					info.AudioChannels = streamInfo.audioChannels
					info.SampleRate = streamInfo.audioSampleRate
					// Note: AudioBitrate is not stored in VideoInfo, only overall Bitrate
					audioStreamFound = true
				}

				// Seek to end of this LIST chunk
				if _, err := f.Seek(currentPos+int64(chunk.Size), io.SeekStart); err != nil {
					return nil, fmt.Errorf("failed to seek to end of stream list: %w", err)
				}
			default:
				// Skip other LIST types
				if _, err := f.Seek(currentPos+int64(chunk.Size), io.SeekStart); err != nil {
					return nil, fmt.Errorf("failed to seek over list chunk: %w", err)
				}
			}

		case "avih":
			// Main AVI header
			var mainHeader aviMainHeader
			if err := binary.Read(f, binary.LittleEndian, &mainHeader); err != nil {
				return nil, fmt.Errorf("failed to read avih: %w", err)
			}

			info.Width = int(mainHeader.Width)
			info.Height = int(mainHeader.Height)
			// Cast to uint64 before multiplication to avoid overflow for long videos
			info.Duration = float64(uint64(mainHeader.TotalFrames)*uint64(mainHeader.MicroSecPerFrame)) / 1000000.0

			if mainHeader.MicroSecPerFrame > 0 {
				info.FrameRate = 1000000.0 / float64(mainHeader.MicroSecPerFrame)
			}

		default:
			// Skip unknown chunks
			if _, err := f.Seek(currentPos+int64(chunk.Size), io.SeekStart); err != nil {
				return nil, fmt.Errorf("failed to seek over unknown chunk: %w", err)
			}
		}

		// Align to word boundary (RIFF chunks are word-aligned)
		if chunk.Size%2 != 0 {
			if _, err := f.Seek(1, io.SeekCurrent); err != nil {
				return nil, fmt.Errorf("failed to align to word boundary: %w", err)
			}
		}
	}

	// Get file size for bitrate calculation
	fileInfo, err := f.Stat()
	if err == nil && info.Duration > 0 {
		info.Bitrate = int(float64(fileInfo.Size()*8) / info.Duration / 1000) // kbps
	}

	return info, nil
}

// streamInfo holds parsed stream information
type streamInfo struct {
	isVideo         bool
	isAudio         bool
	codec           string
	width           int
	height          int
	frameRate       float64
	audioChannels   int
	audioSampleRate int
	audioBitrate    int
}

// parseHdrlList parses the hdrl LIST chunk
func parseHdrlList(f *os.File, info *VideoInfo, startPos int64, size uint32) error {
	endPos := startPos + int64(size)

	for {
		currentPos, _ := f.Seek(0, io.SeekCurrent)
		if currentPos >= endPos {
			break
		}

		var chunk riffChunk
		if err := binary.Read(f, binary.LittleEndian, &chunk); err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("failed to read chunk in hdrl: %w", err)
		}

		fourCC := string(chunk.FourCC[:])

		if fourCC == "avih" {
			var mainHeader aviMainHeader
			if err := binary.Read(f, binary.LittleEndian, &mainHeader); err != nil {
				return fmt.Errorf("failed to read avih: %w", err)
			}

			info.Width = int(mainHeader.Width)
			info.Height = int(mainHeader.Height)
			// Cast to uint64 before multiplication to avoid overflow for long videos
			info.Duration = float64(uint64(mainHeader.TotalFrames)*uint64(mainHeader.MicroSecPerFrame)) / 1000000.0

			if mainHeader.MicroSecPerFrame > 0 {
				info.FrameRate = 1000000.0 / float64(mainHeader.MicroSecPerFrame)
			}
		} else {
			// Skip chunk
			currentPos, _ := f.Seek(0, io.SeekCurrent)
			_, _ = f.Seek(currentPos+int64(chunk.Size), io.SeekStart)
		}

		// Word alignment
		if chunk.Size%2 != 0 {
			_, _ = f.Seek(1, io.SeekCurrent)
		}
	}

	return nil
}

// parseStrlList parses a stream list (strl)
func parseStrlList(f *os.File, startPos int64, size uint32) (*streamInfo, error) {
	endPos := startPos + int64(size)
	stream := &streamInfo{}

	for {
		currentPos, _ := f.Seek(0, io.SeekCurrent)
		if currentPos >= endPos {
			break
		}

		var chunk riffChunk
		if err := binary.Read(f, binary.LittleEndian, &chunk); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("failed to read chunk in strl: %w", err)
		}

		fourCC := string(chunk.FourCC[:])
		chunkDataPos, _ := f.Seek(0, io.SeekCurrent)

		switch fourCC {
		case "strh":
			var streamHeader aviStreamHeader
			if err := binary.Read(f, binary.LittleEndian, &streamHeader); err != nil {
				return nil, fmt.Errorf("failed to read strh: %w", err)
			}

			streamType := string(streamHeader.Type[:])
			switch streamType {
			case "vids":
				stream.isVideo = true
				stream.codec = mapAVIVideoCodec(string(streamHeader.Handler[:]))

				// Calculate frame rate from rate/scale
				if streamHeader.Scale > 0 {
					stream.frameRate = float64(streamHeader.Rate) / float64(streamHeader.Scale)
				}
			case "auds":
				stream.isAudio = true
			}

		case "strf":
			if stream.isVideo {
				// BITMAPINFOHEADER for video
				var bitmapHeader struct {
					Size          uint32
					Width         int32
					Height        int32
					Planes        uint16
					BitCount      uint16
					Compression   [4]byte
					SizeImage     uint32
					XPelsPerMeter int32
					YPelsPerMeter int32
					ClrUsed       uint32
					ClrImportant  uint32
				}

				if err := binary.Read(f, binary.LittleEndian, &bitmapHeader); err != nil {
					return nil, fmt.Errorf("failed to read BITMAPINFOHEADER: %w", err)
				}

				stream.width = int(bitmapHeader.Width)
				// Height can be negative for top-down frames, take absolute value
				if bitmapHeader.Height < 0 {
					stream.height = int(-bitmapHeader.Height)
				} else {
					stream.height = int(bitmapHeader.Height)
				}

				// Update codec from compression field if available
				compressionCodec := mapAVIVideoCodec(string(bitmapHeader.Compression[:]))
				if compressionCodec != "unknown" {
					stream.codec = compressionCodec
				}

			} else if stream.isAudio {
				// WAVEFORMATEX for audio
				var waveFormat struct {
					FormatTag      uint16
					Channels       uint16
					SamplesPerSec  uint32
					AvgBytesPerSec uint32
					BlockAlign     uint16
					BitsPerSample  uint16
				}

				if err := binary.Read(f, binary.LittleEndian, &waveFormat); err != nil {
					return nil, fmt.Errorf("failed to read WAVEFORMATEX: %w", err)
				}

				stream.audioChannels = int(waveFormat.Channels)
				stream.audioSampleRate = int(waveFormat.SamplesPerSec)
				stream.audioBitrate = int(waveFormat.AvgBytesPerSec * 8 / 1000) // kbps
				stream.codec = mapAVIAudioCodec(waveFormat.FormatTag)
			}

			// Seek to end of chunk
			_, _ = f.Seek(chunkDataPos+int64(chunk.Size), io.SeekStart)

		default:
			// Skip unknown chunks
			_, _ = f.Seek(chunkDataPos+int64(chunk.Size), io.SeekStart)
		}

		// Word alignment
		if chunk.Size%2 != 0 {
			_, _ = f.Seek(1, io.SeekCurrent)
		}
	}

	return stream, nil
}

// mapAVIVideoCodec maps AVI video FourCC to friendly codec name
func mapAVIVideoCodec(fourCC string) string {
	// Clean up FourCC (remove null bytes and trim)
	codec := ""
	for i := 0; i < len(fourCC) && fourCC[i] != 0; i++ {
		codec += string(fourCC[i])
	}

	switch codec {
	case "H264", "h264", "X264", "x264", "AVC1", "avc1":
		return "h264"
	case "H265", "h265", "HEVC", "hevc", "HVC1", "hvc1":
		return "h265"
	case "XVID", "xvid":
		return "xvid"
	case "DIVX", "divx", "DX50":
		return "divx"
	case "MP42", "mp42", "MPG4", "mpg4":
		return "mpeg4"
	case "MP43", "mp43":
		return "mpeg4_v3"
	case "WMV1", "wmv1":
		return "wmv1"
	case "WMV2", "wmv2":
		return "wmv2"
	case "WMV3", "wmv3":
		return "wmv3"
	case "VP80", "vp80", "VP8 ", "vp8 ":
		return "vp8"
	case "VP90", "vp90", "VP9 ", "vp9 ":
		return "vp9"
	case "MJPG", "mjpg", "JPEG", "jpeg":
		return "mjpeg"
	case "dvsd", "DVSD":
		return "dv"
	case "FFV1", "ffv1":
		return "ffv1"
	default:
		if codec == "" {
			return "unknown"
		}
		return codec // Return as-is if no mapping
	}
}

// mapAVIAudioCodec maps WAVE format tag to friendly codec name
func mapAVIAudioCodec(formatTag uint16) string {
	switch formatTag {
	case 0x0001:
		return "pcm"
	case 0x0002:
		return "adpcm"
	case 0x0003:
		return "pcm_float"
	case 0x0050:
		return "mp2"
	case 0x0055:
		return "mp3"
	case 0x0161:
		return "wmav1"
	case 0x0162:
		return "wmav2"
	case 0x0163:
		return "wmav3"
	case 0x2000:
		return "ac3"
	case 0x2001:
		return "dts"
	case 0x00FF, 0xFFFE:
		return "aac"
	case 0x0674:
		return "vorbis"
	case 0x6750:
		return "opus"
	case 0xF1AC:
		return "flac"
	default:
		return "unknown"
	}
}
