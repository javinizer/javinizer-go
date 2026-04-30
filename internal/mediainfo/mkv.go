package mediainfo

import (
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	codecH264  = "h264"
	codecHEVC  = "hevc"
	codecH265  = "h265"
	codecVP9   = "vp9"
	codecMPEG4 = "mpeg4"
	codecAAC   = "aac"
	codecMP3   = "mp3"
	codecOPUS  = "opus"
)

type MKVProber struct{}

func NewMKVProber() *MKVProber {
	return &MKVProber{}
}

func (p *MKVProber) Name() string {
	return "mkv"
}

func (p *MKVProber) CanProbe(header []byte) bool {
	if len(header) >= 4 {
		return header[0] == 0x1A && header[1] == 0x45 && header[2] == 0xDF && header[3] == 0xA3
	}
	return false
}

func (p *MKVProber) Probe(f *os.File) (*VideoInfo, error) {
	return analyzeMKV(f)
}

const (
	trackTypeVideo    = 1
	trackTypeAudio    = 2
	trackTypeSubtitle = 17
)

const (
	elemEBML          uint32 = 0x1A45DFA3
	elemSegment       uint32 = 0x18538067
	elemInfo          uint32 = 0x1549A966
	elemTimecodeScale uint32 = 0x2AD7B1
	elemDuration      uint32 = 0x4489
	elemTracks        uint32 = 0x1654AE6B
	elemTrackEntry    uint32 = 0xAE
	elemTrackNumber   uint32 = 0xD7
	elemTrackType     uint32 = 0x83
	elemCodecID       uint32 = 0x86
	elemVideo         uint32 = 0xE0
	elemPixelWidth    uint32 = 0xB0
	elemPixelHeight   uint32 = 0xBA
	elemAudio         uint32 = 0xE1
	elemSamplingFreq  uint32 = 0xB5
	elemChannels      uint32 = 0x9F
	elemCluster       uint32 = 0x1F43B675
	elemCues          uint32 = 0x1C53BB6B
)

func analyzeMKV(f *os.File) (*VideoInfo, error) {
	info := &VideoInfo{
		Container: "mkv",
	}

	stat, _ := f.Stat()
	fileSize := stat.Size()

	_, _ = f.Seek(0, io.SeekStart)

	er := newEBMLReader(f)

	for {
		elem, err := er.readElement()
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			break
		}

		switch elem.id {
		case elemEBML:
			parseEBMLHeader(er, elem.size)

		case elemSegment:
			parseSegment(er, elem.size, info)

		case elemCluster, elemCues:
			if elem.size > 0 {
				if err := er.skipBytes(elem.size); err != nil {
					goto done
				}
			}

		default:
			if elem.size > 0 && elem.data == nil {
				if err := er.skipBytes(elem.size); err != nil {
					goto done
				}
			}
		}
	}

done:
	if info.Duration > 0 && fileSize > 0 {
		info.Bitrate = int((float64(fileSize) * 8) / info.Duration / 1000)
	}

	if info.Width > 0 && info.Height > 0 {
		info.AspectRatio = float64(info.Width) / float64(info.Height)
	}

	if info.VideoCodec == "" && info.Width == 0 && info.Height == 0 && info.Duration == 0 {
		return nil, fmt.Errorf("failed to extract MKV metadata: no usable data found")
	}

	return info, nil
}

func parseEBMLHeader(er *ebmlReader, size int64) {
	if size <= 0 {
		return
	}

	limitReader := io.LimitReader(er.r, size)
	subER := newEBMLReader(limitReader)

	for {
		elem, err := subER.readElement()
		if err != nil {
			return
		}
		_ = elem
	}
}

func parseSegment(er *ebmlReader, size int64, info *VideoInfo) {
	var limitReader io.Reader
	if size > 0 {
		limitReader = io.LimitReader(er.r, size)
	} else {
		limitReader = er.r
	}

	subER := newEBMLReader(limitReader)

	for {
		elem, err := subER.readElement()
		if err != nil {
			return
		}

		switch elem.id {
		case elemInfo:
			parseSegmentInfo(subER, elem.size, info)

		case elemTracks:
			parseTracks(subER, elem.size, info)

		case elemCluster, elemCues:
			if elem.size > 0 {
				if err := subER.skipBytes(elem.size); err != nil {
					return
				}
			}

		default:
			if elem.size > 0 && elem.data == nil {
				if err := subER.skipBytes(elem.size); err != nil {
					return
				}
			}
		}
	}
}

func parseSegmentInfo(er *ebmlReader, size int64, info *VideoInfo) {
	if size <= 0 {
		return
	}

	limitReader := io.LimitReader(er.r, size)
	subER := newEBMLReader(limitReader)

	var timecodeScale uint64 = 1000000

	for {
		elem, err := subER.readElement()
		if err != nil {
			break
		}

		switch elem.id {
		case elemTimecodeScale:
			if elem.data != nil {
				timecodeScale = parseUint(elem.data)
			}
		case elemDuration:
			if elem.data != nil {
				duration := parseFloatEBML(elem.data)
				if duration > 0 {
					info.Duration = duration * float64(timecodeScale) / 1000000000.0
				}
			}
		default:
			if elem.size > 0 && elem.data == nil {
				if err := subER.skipBytes(elem.size); err != nil {
					return
				}
			}
		}
	}
}

func parseTracks(er *ebmlReader, size int64, info *VideoInfo) {
	if size <= 0 {
		return
	}

	limitReader := io.LimitReader(er.r, size)
	subER := newEBMLReader(limitReader)

	for {
		elem, err := subER.readElement()
		if err != nil {
			return
		}

		switch elem.id {
		case elemTrackEntry:
			parseTrackEntry(subER, elem.size, info)

		default:
			if elem.size > 0 && elem.data == nil {
				if err := subER.skipBytes(elem.size); err != nil {
					return
				}
			}
		}
	}
}

func parseTrackEntry(er *ebmlReader, size int64, info *VideoInfo) {
	if size <= 0 {
		return
	}

	limitReader := io.LimitReader(er.r, size)
	subER := newEBMLReader(limitReader)

	var trackType uint64
	var codecID string
	var pixelWidth, pixelHeight uint64
	var samplingFreq float64
	var channels uint64

	for {
		elem, err := subER.readElement()
		if err != nil {
			break
		}

		switch elem.id {
		case elemTrackType:
			if elem.data != nil {
				trackType = parseUint(elem.data)
			}
		case elemCodecID:
			if elem.data != nil {
				codecID = parseString(elem.data)
			}
		case elemVideo:
			parseVideoElement(subER, elem.size, &pixelWidth, &pixelHeight)
		case elemAudio:
			parseAudioElement(subER, elem.size, &samplingFreq, &channels)
		default:
			if elem.size > 0 && elem.data == nil {
				if err := subER.skipBytes(elem.size); err != nil {
					break
				}
			}
		}
	}

	switch trackType {
	case trackTypeVideo:
		info.VideoCodec = mapMKVVideoCodec(codecID)
		info.Width = int(pixelWidth)
		info.Height = int(pixelHeight)
	case trackTypeAudio:
		if info.AudioCodec == "" {
			info.AudioCodec = mapMKVAudioCodec(codecID)
			info.SampleRate = int(samplingFreq)
			info.AudioChannels = int(channels)
			if info.AudioChannels == 0 {
				info.AudioChannels = 2
			}
		}
	}
}

func parseVideoElement(er *ebmlReader, size int64, pixelWidth, pixelHeight *uint64) {
	if size <= 0 {
		return
	}

	limitReader := io.LimitReader(er.r, size)
	subER := newEBMLReader(limitReader)

	for {
		elem, err := subER.readElement()
		if err != nil {
			return
		}

		switch elem.id {
		case elemPixelWidth:
			if elem.data != nil {
				*pixelWidth = parseUint(elem.data)
			}
		case elemPixelHeight:
			if elem.data != nil {
				*pixelHeight = parseUint(elem.data)
			}
		default:
			if elem.size > 0 && elem.data == nil {
				if err := subER.skipBytes(elem.size); err != nil {
					return
				}
			}
		}
	}
}

func parseAudioElement(er *ebmlReader, size int64, samplingFreq *float64, channels *uint64) {
	if size <= 0 {
		return
	}

	limitReader := io.LimitReader(er.r, size)
	subER := newEBMLReader(limitReader)

	for {
		elem, err := subER.readElement()
		if err != nil {
			return
		}

		switch elem.id {
		case elemSamplingFreq:
			if elem.data != nil {
				*samplingFreq = parseFloatEBML(elem.data)
			}
		case elemChannels:
			if elem.data != nil {
				*channels = parseUint(elem.data)
			}
		default:
			if elem.size > 0 && elem.data == nil {
				if err := subER.skipBytes(elem.size); err != nil {
					return
				}
			}
		}
	}
}

func mapMKVVideoCodec(codecID string) string {
	codecID = strings.ToUpper(codecID)

	if strings.Contains(codecID, "AVC") || strings.Contains(codecID, "H264") {
		return codecH264
	}
	if strings.Contains(codecID, "HEVC") || strings.Contains(codecID, "H265") {
		return codecHEVC
	}
	if strings.Contains(codecID, "VP9") {
		return codecVP9
	}
	if strings.Contains(codecID, "VP8") {
		return "vp8"
	}
	if strings.Contains(codecID, "AV1") {
		return "av1"
	}
	if strings.Contains(codecID, "MPEG4") {
		return codecMPEG4
	}
	if strings.Contains(codecID, "THEORA") {
		return "theora"
	}

	return strings.TrimPrefix(codecID, "V_")
}

func mapMKVAudioCodec(codecID string) string {
	codecID = strings.ToUpper(codecID)

	if strings.Contains(codecID, "AAC") {
		return codecAAC
	}
	if strings.Contains(codecID, "MP3") || strings.Contains(codecID, "MPEG/L3") {
		return codecMP3
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
		return codecOPUS
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
	if strings.Contains(codecID, "MS/ACM") || strings.Contains(codecID, "WMA") {
		return "wma"
	}

	return strings.TrimPrefix(codecID, "A_")
}
