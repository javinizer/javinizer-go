package mediainfo

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
)

// FLVProber implements the Prober interface for FLV containers
type FLVProber struct{}

// NewFLVProber creates a new FLV prober
func NewFLVProber() *FLVProber {
	return &FLVProber{}
}

// Name returns the prober identifier
func (p *FLVProber) Name() string {
	return "flv"
}

// CanProbe checks if this prober can handle the file based on header
func (p *FLVProber) CanProbe(header []byte) bool {
	// FLV: starts with "FLV"
	if len(header) >= 3 {
		return header[0] == 'F' && header[1] == 'L' && header[2] == 'V'
	}
	return false
}

// Probe extracts metadata from the FLV file
func (p *FLVProber) Probe(f *os.File) (*VideoInfo, error) {
	return analyzeFLV(f)
}

// FLV file header
type flvHeader struct {
	Signature   [3]byte // "FLV"
	Version     uint8
	TypeFlags   uint8 // 1 = video, 4 = audio, 5 = audio+video
	DataOffset  uint32
	PrevTagSize uint32 // Always 0 for first tag
}

// FLV tag header
type flvTagHeader struct {
	TagType   uint8  // 8 = audio, 9 = video, 18 = script data
	DataSize  uint32 // 24-bit
	Timestamp uint32 // 24-bit + 8-bit extension
	StreamID  uint32 // 24-bit, always 0
}

// analyzeFLV parses FLV container
func analyzeFLV(f *os.File) (*VideoInfo, error) {
	_, err := f.Seek(0, io.SeekStart)
	if err != nil {
		return nil, fmt.Errorf("seek failed: %w", err)
	}

	info := &VideoInfo{
		Container: "flv",
	}

	// Read FLV header
	var header flvHeader
	if err := binary.Read(f, binary.BigEndian, &header.Signature); err != nil {
		return nil, fmt.Errorf("failed to read signature: %w", err)
	}

	if string(header.Signature[:]) != "FLV" {
		return nil, fmt.Errorf("invalid FLV signature")
	}

	if err := binary.Read(f, binary.BigEndian, &header.Version); err != nil {
		return nil, fmt.Errorf("failed to read version: %w", err)
	}

	if err := binary.Read(f, binary.BigEndian, &header.TypeFlags); err != nil {
		return nil, fmt.Errorf("failed to read type flags: %w", err)
	}

	if err := binary.Read(f, binary.BigEndian, &header.DataOffset); err != nil {
		return nil, fmt.Errorf("failed to read data offset: %w", err)
	}

	// Seek to data offset
	_, _ = f.Seek(int64(header.DataOffset), io.SeekStart)

	// Read first PreviousTagSize (always 0)
	if err := binary.Read(f, binary.BigEndian, &header.PrevTagSize); err != nil {
		return nil, fmt.Errorf("failed to read previous tag size: %w", err)
	}

	// Parse tags to find metadata
	maxTags := 100 // Limit tag parsing for performance
	tagCount := 0
	foundMetadata := false
	var lastTimestamp uint32

	for tagCount < maxTags {
		tag, err := readFLVTag(f)
		if err != nil {
			if err == io.EOF {
				break
			}
			// Skip malformed tags
			break
		}

		tagCount++
		lastTimestamp = tag.Timestamp

		switch tag.TagType {
		case 18: // Script data (metadata)
			if !foundMetadata {
				startPos, _ := f.Seek(0, io.SeekCurrent)
				metadata, err := parseAMF0(f, tag.DataSize)
				if err == nil {
					applyFLVMetadata(info, metadata)
					foundMetadata = true
				}
				// Calculate how many bytes were consumed and skip only the remainder
				currentPos, _ := f.Seek(0, io.SeekCurrent)
				bytesConsumed := currentPos - startPos
				bytesRemaining := int64(tag.DataSize) - bytesConsumed
				if bytesRemaining > 0 {
					_, _ = f.Seek(bytesRemaining, io.SeekCurrent)
				}
			} else {
				// Skip tag data if metadata already found
				_, _ = f.Seek(int64(tag.DataSize), io.SeekCurrent)
			}

		case 9: // Video
			if info.VideoCodec == "" {
				codecID, _ := readByte(f)
				// FLV video tag format: bits 7-4 = frame type, bits 3-0 = codec ID
				info.VideoCodec = mapFLVVideoCodec(codecID & 0x0F)
				// Seek back 1 byte
				_, _ = f.Seek(-1, io.SeekCurrent)
			}
			// Skip tag data
			_, _ = f.Seek(int64(tag.DataSize), io.SeekCurrent)

		case 8: // Audio
			if info.AudioCodec == "" {
				soundFormat, _ := readByte(f)
				info.AudioCodec = mapFLVAudioCodec(soundFormat >> 4)

				// Extract audio details from flags
				soundRate := (soundFormat >> 2) & 0x03
				// soundSize := (soundFormat >> 1) & 0x01 // 0 = 8-bit, 1 = 16-bit (not used)
				soundType := soundFormat & 0x01

				info.SampleRate = mapFLVSampleRate(soundRate)
				info.AudioChannels = int(soundType) + 1 // 0 = mono, 1 = stereo

				// Seek back 1 byte
				_, _ = f.Seek(-1, io.SeekCurrent)
			}
			// Skip tag data
			_, _ = f.Seek(int64(tag.DataSize), io.SeekCurrent)

		default:
			// Skip unknown tag types
			_, _ = f.Seek(int64(tag.DataSize), io.SeekCurrent)
		}

		// Read PreviousTagSize for next tag
		var prevTagSize uint32
		if err := binary.Read(f, binary.BigEndian, &prevTagSize); err != nil {
			break
		}

		// Stop early if we have all the info we need
		if foundMetadata && info.VideoCodec != "" && info.AudioCodec != "" {
			break
		}
	}

	// Calculate duration from last timestamp if not found in metadata
	if info.Duration == 0 && lastTimestamp > 0 {
		info.Duration = float64(lastTimestamp) / 1000.0
	}

	// Get file size for bitrate calculation
	fileInfo, err := f.Stat()
	if err == nil && info.Duration > 0 {
		info.Bitrate = int(float64(fileInfo.Size()*8) / info.Duration / 1000) // kbps
	}

	return info, nil
}

// readFLVTag reads an FLV tag header
func readFLVTag(f *os.File) (*flvTagHeader, error) {
	tag := &flvTagHeader{}

	// Read tag type
	if err := binary.Read(f, binary.BigEndian, &tag.TagType); err != nil {
		return nil, err
	}

	// Read data size (24-bit)
	var dataSizeBytes [3]byte
	if _, err := f.Read(dataSizeBytes[:]); err != nil {
		return nil, err
	}
	tag.DataSize = uint32(dataSizeBytes[0])<<16 | uint32(dataSizeBytes[1])<<8 | uint32(dataSizeBytes[2])

	// Read timestamp (24-bit + 8-bit extension)
	var timestampBytes [3]byte
	if _, err := f.Read(timestampBytes[:]); err != nil {
		return nil, err
	}
	var timestampExt uint8
	if err := binary.Read(f, binary.BigEndian, &timestampExt); err != nil {
		return nil, err
	}
	tag.Timestamp = uint32(timestampExt)<<24 | uint32(timestampBytes[0])<<16 | uint32(timestampBytes[1])<<8 | uint32(timestampBytes[2])

	// Read stream ID (24-bit, always 0)
	var streamIDBytes [3]byte
	if _, err := f.Read(streamIDBytes[:]); err != nil {
		return nil, err
	}
	tag.StreamID = uint32(streamIDBytes[0])<<16 | uint32(streamIDBytes[1])<<8 | uint32(streamIDBytes[2])

	return tag, nil
}

// readByte reads a single byte
func readByte(f *os.File) (byte, error) {
	var b byte
	err := binary.Read(f, binary.BigEndian, &b)
	return b, err
}

// parseAMF0 parses AMF0 metadata (simplified parser for onMetaData)
func parseAMF0(f *os.File, dataSize uint32) (map[string]interface{}, error) {
	startPos, _ := f.Seek(0, io.SeekCurrent)
	endPos := startPos + int64(dataSize)

	metadata := make(map[string]interface{})

	// Skip first AMF0 value (usually "onMetaData" string)
	amfType, _ := readByte(f)
	if amfType == 0x02 { // String type
		var strLen uint16
		if err := binary.Read(f, binary.BigEndian, &strLen); err != nil {
			return metadata, fmt.Errorf("failed to read AMF string length: %w", err)
		}
		if _, err := f.Seek(int64(strLen), io.SeekCurrent); err != nil {
			return metadata, fmt.Errorf("failed to skip AMF string: %w", err)
		}
	}

	// Read ECMA array or object
	amfType, _ = readByte(f)
	switch amfType {
	case 0x08: // ECMA array
		var arrayLen uint32
		if err := binary.Read(f, binary.BigEndian, &arrayLen); err != nil {
			return metadata, fmt.Errorf("failed to read AMF array length: %w", err)
		}
	case 0x03: // Object
		// Continue parsing
	default:
		return metadata, fmt.Errorf("unexpected AMF type: %d", amfType)
	}

	// Parse properties
	for {
		currentPos, _ := f.Seek(0, io.SeekCurrent)
		if currentPos >= endPos {
			break
		}

		// Read property name
		var nameLen uint16
		if err := binary.Read(f, binary.BigEndian, &nameLen); err != nil {
			break
		}

		if nameLen == 0 {
			// End of object marker
			var endMarker uint8
			if err := binary.Read(f, binary.BigEndian, &endMarker); err != nil {
				return metadata, fmt.Errorf("failed to read AMF end marker: %w", err)
			}
			if endMarker == 0x09 {
				break
			}
		}

		nameBytes := make([]byte, nameLen)
		if _, err := f.Read(nameBytes); err != nil {
			return metadata, fmt.Errorf("failed to read AMF property name: %w", err)
		}
		name := string(nameBytes)

		// Read property value
		var valueType uint8
		if err := binary.Read(f, binary.BigEndian, &valueType); err != nil {
			return metadata, fmt.Errorf("failed to read AMF value type: %w", err)
		}

		switch valueType {
		case 0x00: // Number (float64)
			var value float64
			_ = binary.Read(f, binary.BigEndian, &value)
			metadata[name] = value

		case 0x01: // Boolean
			var value uint8
			_ = binary.Read(f, binary.BigEndian, &value)
			metadata[name] = value != 0

		case 0x02: // String
			var strLen uint16
			if err := binary.Read(f, binary.BigEndian, &strLen); err != nil {
				return metadata, fmt.Errorf("failed to read AMF string value length: %w", err)
			}
			strBytes := make([]byte, strLen)
			if _, err := f.Read(strBytes); err != nil {
				return metadata, fmt.Errorf("failed to read AMF string value: %w", err)
			}
			metadata[name] = string(strBytes)

		case 0x08: // ECMA array - skip entire array
			var arrayLen uint32
			_ = binary.Read(f, binary.BigEndian, &arrayLen)
			// Skip to end marker (would need recursive parsing)
			// For now, seek to endPos to avoid corruption
			_, _ = f.Seek(endPos, io.SeekStart)
			return metadata, nil

		case 0x0A: // Strict array
			var arrayLen uint32
			_ = binary.Read(f, binary.BigEndian, &arrayLen)
			// Skip array elements - would need recursive parsing
			_, _ = f.Seek(endPos, io.SeekStart)
			return metadata, nil

		case 0x0B: // Date
			// Date is 8 bytes (double) + 2 bytes (timezone)
			_, _ = f.Seek(10, io.SeekCurrent)

		case 0x03: // Object
			// Skip nested object - would need recursive parsing
			_, _ = f.Seek(endPos, io.SeekStart)
			return metadata, nil

		default:
			// Unknown type - seek to end to avoid corruption
			_, _ = f.Seek(endPos, io.SeekStart)
			return metadata, nil
		}
	}

	return metadata, nil
}

// applyFLVMetadata applies metadata to VideoInfo
func applyFLVMetadata(info *VideoInfo, metadata map[string]interface{}) {
	if width, ok := metadata["width"].(float64); ok {
		info.Width = int(width)
	}
	if height, ok := metadata["height"].(float64); ok {
		info.Height = int(height)
	}
	if duration, ok := metadata["duration"].(float64); ok {
		info.Duration = duration
	}
	if frameRate, ok := metadata["framerate"].(float64); ok {
		info.FrameRate = frameRate
	}
	if videoCodec, ok := metadata["videocodecid"].(float64); ok {
		codec := mapFLVVideoCodec(uint8(videoCodec))
		if codec != "unknown" {
			info.VideoCodec = codec
		}
	}
	if audioCodec, ok := metadata["audiocodecid"].(float64); ok {
		codec := mapFLVAudioCodec(uint8(audioCodec))
		if codec != "unknown" {
			info.AudioCodec = codec
		}
	}
	if sampleRate, ok := metadata["audiosamplerate"].(float64); ok {
		info.SampleRate = int(sampleRate)
	}
	if channels, ok := metadata["audiochannels"].(float64); ok {
		info.AudioChannels = int(channels)
	}
}

// mapFLVVideoCodec maps FLV video codec ID to friendly name
func mapFLVVideoCodec(codecID uint8) string {
	switch codecID {
	case 2:
		return "h263" // Sorenson H.263
	case 3:
		return "screen_video" // Screen video
	case 4:
		return "vp6"
	case 5:
		return "vp6_alpha"
	case 6:
		return "screen_video_v2"
	case 7:
		return "h264" // AVC
	case 12:
		return "h265" // HEVC (newer FLV extensions)
	default:
		return "unknown"
	}
}

// mapFLVAudioCodec maps FLV audio codec ID to friendly name
func mapFLVAudioCodec(codecID uint8) string {
	switch codecID {
	case 0:
		return "pcm" // Linear PCM, platform endian
	case 1:
		return "adpcm"
	case 2:
		return "mp3"
	case 3:
		return "pcm_le" // Linear PCM, little endian
	case 4:
		return "nellymoser_16khz"
	case 5:
		return "nellymoser_8khz"
	case 6:
		return "nellymoser"
	case 7:
		return "g711_alaw"
	case 8:
		return "g711_mulaw"
	case 10:
		return "aac"
	case 11:
		return "speex"
	case 14:
		return "mp3_8khz"
	case 15:
		return "device_specific"
	default:
		return "unknown"
	}
}

// mapFLVSampleRate maps FLV sample rate code to actual rate
func mapFLVSampleRate(code uint8) int {
	switch code {
	case 0:
		return 5512
	case 1:
		return 11025
	case 2:
		return 22050
	case 3:
		return 44100
	default:
		return 0
	}
}

// Helper to check if float is NaN
func isNaN(f float64) bool {
	return math.IsNaN(f)
}
