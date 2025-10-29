package mediainfo

import (
	"os"
)

// MOVProber implements the Prober interface for QuickTime MOV containers
// MOV files use ISO Base Media File Format, same as MP4
type MOVProber struct {
	mp4Prober *MP4Prober
}

// NewMOVProber creates a new MOV prober
func NewMOVProber() *MOVProber {
	return &MOVProber{
		mp4Prober: NewMP4Prober(),
	}
}

// Name returns the prober identifier
func (p *MOVProber) Name() string {
	return "mov"
}

// CanProbe checks if this prober can handle the file based on header
func (p *MOVProber) CanProbe(header []byte) bool {
	// MOV files also use 'ftyp' box but with QuickTime brands
	// Check for ftyp at offset 4-7
	if len(header) < 12 {
		return false
	}

	// Must have ftyp box
	if header[4] != 'f' || header[5] != 't' || header[6] != 'y' || header[7] != 'p' {
		return false
	}

	// Check major brand (bytes 8-11)
	// QuickTime brands: "qt  ", "M4V ", "M4A ", "M4B ", "F4V ", "F4A ", "F4B "
	brand := string(header[8:12])

	// QuickTime classic
	if brand == "qt  " {
		return true
	}

	// Apple iTunes video/audio
	if brand == "M4V " || brand == "M4A " || brand == "M4B " || brand == "M4P " {
		return true
	}

	// Adobe Flash video/audio (Flash uses MOV container)
	if brand == "F4V " || brand == "F4A " || brand == "F4B " {
		return true
	}

	// Some MOV files don't have QuickTime brands but are still MOV
	// These will be handled by MP4Prober since the format is compatible

	return false
}

// Probe extracts metadata from the MOV file
func (p *MOVProber) Probe(f *os.File) (*VideoInfo, error) {
	// MOV uses the same format as MP4, so we can reuse the MP4 parser
	// The mp4ff library handles both MP4 and MOV containers
	info, err := analyzeMP4(f)
	if err != nil {
		return nil, err
	}

	// Update container type to "mov"
	info.Container = "mov"

	// Map QuickTime-specific codecs to friendly names
	info.VideoCodec = mapMOVVideoCodec(info.VideoCodec)
	info.AudioCodec = mapMOVAudioCodec(info.AudioCodec)

	return info, nil
}

// mapMOVVideoCodec maps QuickTime video codec FourCCs to friendly names
func mapMOVVideoCodec(codec string) string {
	// Handle QuickTime-specific codecs
	switch codec {
	case "apch":
		return "prores_422_hq"
	case "apcn":
		return "prores_422"
	case "apcs":
		return "prores_422_lt"
	case "apco":
		return "prores_422_proxy"
	case "ap4h":
		return "prores_4444"
	case "ap4x":
		return "prores_4444_xq"
	case "dvcp", "dvpp":
		return "dvcpro"
	case "dvc ", "dvsd":
		return "dv"
	case "dvh5", "dvh6":
		return "dvcpro_hd"
	case "mjp2":
		return "jpeg2000"
	case "jpeg":
		return "mjpeg"
	case "png ":
		return "png"
	case "rle ":
		return "quicktime_rle"
	case "rpza":
		return "quicktime_rpza"
	case "smc ":
		return "quicktime_smc"
	case "SVQ1":
		return "sorenson_video_1"
	case "SVQ3":
		return "sorenson_video_3"
	case "mp4v":
		return "mpeg4"
	default:
		return codec // Return as-is if no mapping found
	}
}

// mapMOVAudioCodec maps QuickTime audio codec FourCCs to friendly names
func mapMOVAudioCodec(codec string) string {
	// Handle QuickTime-specific codecs
	switch codec {
	case "sowt":
		return "pcm_s16le" // Little-endian PCM
	case "twos":
		return "pcm_s16be" // Big-endian PCM
	case "in24":
		return "pcm_s24le"
	case "in32":
		return "pcm_s32le"
	case "fl32":
		return "pcm_f32le"
	case "fl64":
		return "pcm_f64le"
	case "ulaw":
		return "pcm_mulaw"
	case "alaw":
		return "pcm_alaw"
	case "ima4":
		return "adpcm_ima_qt"
	case "MAC3":
		return "mace3"
	case "MAC6":
		return "mace6"
	case "Qclp":
		return "qcelp"
	case "QDM2":
		return "qdm2"
	case "QDMC":
		return "qdmc"
	case "alac":
		return "alac"
	default:
		return codec // Return as-is if no mapping found
	}
}
