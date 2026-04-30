package mediainfo

import (
	"encoding/binary"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindBox(t *testing.T) {
	tests := []struct {
		name   string
		data   []byte
		target string
		want   bool
	}{
		{
			"found moov",
			mustBuildBoxes(t, []boxSpec{{"moov", nil}}),
			"moov",
			true,
		},
		{
			"not found",
			mustBuildBoxes(t, []boxSpec{{"moov", nil}}),
			"mdat",
			false,
		},
		{
			"empty data",
			[]byte{},
			"moov",
			false,
		},
		{
			"truncated box",
			[]byte{0x00, 0x00, 0x00, 0x20},
			"moov",
			false,
		},
		{
			"found among multiple",
			mustBuildBoxes(t, []boxSpec{{"ftyp", nil}, {"moov", []byte{0x01}}, {"mdat", nil}}),
			"moov",
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := findBox(tt.data, tt.target)
			if tt.want {
				assert.NotNil(t, result)
			} else {
				assert.Nil(t, result)
			}
		})
	}
}

func TestFindBoxes(t *testing.T) {
	tests := []struct {
		name   string
		data   []byte
		target string
		want   int
	}{
		{
			"single trak",
			mustBuildBoxes(t, []boxSpec{{"trak", []byte{0x01}}}),
			"trak",
			1,
		},
		{
			"multiple traks",
			mustBuildBoxes(t, []boxSpec{{"trak", []byte{0x01}}, {"trak", []byte{0x02}}}),
			"trak",
			2,
		},
		{
			"none found",
			mustBuildBoxes(t, []boxSpec{{"moov", nil}}),
			"trak",
			0,
		},
		{
			"empty data",
			[]byte{},
			"trak",
			0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := findBoxes(tt.data, tt.target)
			assert.Equal(t, tt.want, len(results))
		})
	}
}

func TestParseMvhd(t *testing.T) {
	t.Run("version 0", func(t *testing.T) {
		data := make([]byte, 100)
		data[0] = 0                                    // version 0
		binary.BigEndian.PutUint32(data[12:16], 1000)  // timescale
		binary.BigEndian.PutUint32(data[16:20], 30000) // duration
		ts, dur := parseMvhd(data)
		assert.Equal(t, uint32(1000), ts)
		assert.Equal(t, uint64(30000), dur)
	})

	t.Run("version 1", func(t *testing.T) {
		data := make([]byte, 120)
		data[0] = 1                                    // version 1
		binary.BigEndian.PutUint32(data[20:24], 1000)  // timescale
		binary.BigEndian.PutUint64(data[24:32], 60000) // duration
		ts, dur := parseMvhd(data)
		assert.Equal(t, uint32(1000), ts)
		assert.Equal(t, uint64(60000), dur)
	})

	t.Run("too short", func(t *testing.T) {
		data := make([]byte, 50)
		ts, dur := parseMvhd(data)
		assert.Equal(t, uint32(0), ts)
		assert.Equal(t, uint64(0), dur)
	})
}

func TestParseMdhd(t *testing.T) {
	t.Run("version 0", func(t *testing.T) {
		data := make([]byte, 24)
		data[0] = 0
		binary.BigEndian.PutUint32(data[12:16], 24000)
		binary.BigEndian.PutUint32(data[16:20], 72000)
		ts, dur := parseMdhd(data)
		assert.Equal(t, uint32(24000), ts)
		assert.Equal(t, uint64(72000), dur)
	})

	t.Run("version 1", func(t *testing.T) {
		data := make([]byte, 40)
		data[0] = 1
		binary.BigEndian.PutUint32(data[20:24], 24000)
		binary.BigEndian.PutUint64(data[24:32], 144000)
		ts, dur := parseMdhd(data)
		assert.Equal(t, uint32(24000), ts)
		assert.Equal(t, uint64(144000), dur)
	})

	t.Run("too short", func(t *testing.T) {
		ts, dur := parseMdhd([]byte{0x00, 0x01})
		assert.Equal(t, uint32(0), ts)
		assert.Equal(t, uint64(0), dur)
	})
}

func TestParseHdlr(t *testing.T) {
	t.Run("valid vide", func(t *testing.T) {
		data := make([]byte, 12)
		copy(data[8:12], "vide")
		assert.Equal(t, "vide", parseHdlr(data))
	})

	t.Run("valid soun", func(t *testing.T) {
		data := make([]byte, 12)
		copy(data[8:12], "soun")
		assert.Equal(t, "soun", parseHdlr(data))
	})

	t.Run("too short", func(t *testing.T) {
		assert.Equal(t, "", parseHdlr([]byte{0x00, 0x01}))
	})
}

func TestParseStts(t *testing.T) {
	t.Run("single entry", func(t *testing.T) {
		data := make([]byte, 16)
		binary.BigEndian.PutUint32(data[4:8], 1)     // entry count
		binary.BigEndian.PutUint32(data[8:12], 1000) // sample count
		binary.BigEndian.PutUint32(data[12:16], 1)   // sample delta
		totalSamples, totalDelta := parseStts(data)
		assert.Equal(t, uint32(1000), totalSamples)
		assert.Equal(t, uint64(1000), totalDelta)
	})

	t.Run("multiple entries", func(t *testing.T) {
		data := make([]byte, 24)
		binary.BigEndian.PutUint32(data[4:8], 2)     // entry count
		binary.BigEndian.PutUint32(data[8:12], 500)  // sample count 1
		binary.BigEndian.PutUint32(data[12:16], 2)   // sample delta 1
		binary.BigEndian.PutUint32(data[16:20], 500) // sample count 2
		binary.BigEndian.PutUint32(data[20:24], 4)   // sample delta 2
		totalSamples, totalDelta := parseStts(data)
		assert.Equal(t, uint32(1000), totalSamples)
		assert.Equal(t, uint64(3000), totalDelta)
	})

	t.Run("empty", func(t *testing.T) {
		totalSamples, totalDelta := parseStts([]byte{})
		assert.Equal(t, uint32(0), totalSamples)
		assert.Equal(t, uint64(0), totalDelta)
	})
}

func TestParseStsdVideo(t *testing.T) {
	t.Run("h264 1920x1080", func(t *testing.T) {
		data := make([]byte, 94)
		binary.BigEndian.PutUint32(data[0:4], 0) // version+flags
		binary.BigEndian.PutUint32(data[4:8], 1) // entry count
		off := 8
		binary.BigEndian.PutUint32(data[off:off+4], 78)       // entry size
		copy(data[off+4:off+8], "avc1")                       // codec
		binary.BigEndian.PutUint16(data[off+32:off+34], 1920) // width
		binary.BigEndian.PutUint16(data[off+34:off+36], 1080) // height
		codec, w, h := parseStsdVideo(data)
		assert.Equal(t, "avc1", codec)
		assert.Equal(t, uint16(1920), w)
		assert.Equal(t, uint16(1080), h)
	})

	t.Run("too short for dimensions", func(t *testing.T) {
		data := make([]byte, 20)
		binary.BigEndian.PutUint32(data[4:8], 1)
		copy(data[12:16], "avc1")
		codec, w, h := parseStsdVideo(data)
		assert.Equal(t, "avc1", codec)
		assert.Equal(t, uint16(0), w)
		assert.Equal(t, uint16(0), h)
	})

	t.Run("too short", func(t *testing.T) {
		codec, w, h := parseStsdVideo([]byte{})
		assert.Equal(t, "", codec)
		assert.Equal(t, uint16(0), w)
		assert.Equal(t, uint16(0), h)
	})

	t.Run("zero entries", func(t *testing.T) {
		data := make([]byte, 8)
		binary.BigEndian.PutUint32(data[4:8], 0)
		codec, w, h := parseStsdVideo(data)
		assert.Equal(t, "", codec)
		assert.Equal(t, uint16(0), w)
		assert.Equal(t, uint16(0), h)
	})
}

func TestParseStsdAudio(t *testing.T) {
	t.Run("aac stereo 48kHz", func(t *testing.T) {
		data := make([]byte, 50)
		binary.BigEndian.PutUint32(data[4:8], 1) // entry count
		off := 8
		binary.BigEndian.PutUint32(data[off:off+4], 30) // entry size
		copy(data[off+4:off+8], "mp4a")
		binary.BigEndian.PutUint16(data[off+24:off+26], 2)     // channels
		binary.BigEndian.PutUint16(data[off+32:off+34], 48000) // sample rate
		codec, ch, sr := parseStsdAudio(data)
		assert.Equal(t, "mp4a", codec)
		assert.Equal(t, uint16(2), ch)
		assert.Equal(t, uint16(48000), sr)
	})

	t.Run("too short", func(t *testing.T) {
		codec, ch, sr := parseStsdAudio([]byte{})
		assert.Equal(t, "", codec)
		assert.Equal(t, uint16(0), ch)
		assert.Equal(t, uint16(0), sr)
	})
}

func TestIsFieldBasedDelta(t *testing.T) {
	tests := []struct {
		name      string
		avgDelta  float64
		timescale float64
		want      bool
	}{
		{"59.94fps", 1001.0, 60000.0, true},
		{"60fps", 1000.0, 60000.0, true},
		{"50fps", 960.0, 48000.0, true},
		{"29.97fps", 2002.0, 60000.0, false},
		{"24fps", 1001.0, 24000.0, false},
		{"25fps", 960.0, 24000.0, false},
		{"30fps", 1001.0, 30000.0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isFieldBasedDelta(tt.avgDelta, tt.timescale))
		})
	}
}

func TestReadBox_ExtendedSize(t *testing.T) {
	tmpDir := t.TempDir()
	path := tmpDir + "/extended.mp4"
	f, err := os.Create(path)
	require.NoError(t, err)

	box := make([]byte, 16)
	binary.BigEndian.PutUint32(box[0:4], 1) // size=1 means extended
	copy(box[4:8], "moov")
	binary.BigEndian.PutUint64(box[8:16], 16) // extended size = 16 (just the header)
	_, err = f.Write(box)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	f, err = os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	mr := newMP4BoxReader(f)
	b, err := mr.readBox()
	require.NoError(t, err)
	assert.Equal(t, "moov", b.boxType)
	assert.Equal(t, uint64(16), b.size)
	assert.Equal(t, 16, b.headerSize)
}

func TestReadBox_SizeZero(t *testing.T) {
	tmpDir := t.TempDir()
	path := tmpDir + "/size0.mp4"
	f, err := os.Create(path)
	require.NoError(t, err)

	box := make([]byte, 12)
	binary.BigEndian.PutUint32(box[0:4], 0) // size=0 means box extends to EOF
	copy(box[4:8], "mdat")
	box[8] = 0xAA
	box[9] = 0xBB
	box[10] = 0xCC
	box[11] = 0xDD
	_, err = f.Write(box)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	f, err = os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	mr := newMP4BoxReader(f)
	b, err := mr.readBox()
	require.NoError(t, err)
	assert.Equal(t, "mdat", b.boxType)
	assert.True(t, b.size >= 12)
}

func TestSkipBox(t *testing.T) {
	t.Run("data already loaded", func(t *testing.T) {
		box := &mp4Box{boxType: "test", size: 100, headerSize: 8, data: make([]byte, 92)}
		mr := &mp4BoxReader{}
		assert.NoError(t, mr.skipBox(box))
	})

	t.Run("data nil requires seek", func(t *testing.T) {
		tmpDir := t.TempDir()
		path := tmpDir + "/skip.mp4"
		f, err := os.Create(path)
		require.NoError(t, err)
		_, err = f.Write(make([]byte, 200))
		require.NoError(t, err)
		require.NoError(t, f.Close())

		f, err = os.Open(path)
		require.NoError(t, err)
		defer func() { _ = f.Close() }()

		mr := newMP4BoxReader(f)
		box := &mp4Box{boxType: "mdat", size: 100, headerSize: 8, data: nil}
		assert.NoError(t, mr.skipBox(box))
	})
}

type boxSpec struct {
	boxType string
	data    []byte
}

func mustBuildBoxes(t *testing.T, specs []boxSpec) []byte {
	t.Helper()
	var buf []byte
	for _, s := range specs {
		size := uint32(8 + len(s.data))
		header := make([]byte, 8)
		binary.BigEndian.PutUint32(header[0:4], size)
		copy(header[4:8], s.boxType)
		buf = append(buf, header...)
		buf = append(buf, s.data...)
	}
	return buf
}

func TestAnalyzeMP4Fallback_Minimal(t *testing.T) {
	tmpDir := t.TempDir()
	path := tmpDir + "/minimal.mp4"
	f, err := os.Create(path)
	require.NoError(t, err)

	var buf []byte

	ftyp := make([]byte, 20)
	binary.BigEndian.PutUint32(ftyp[0:4], 20)
	copy(ftyp[4:8], "ftyp")
	copy(ftyp[8:12], "isom")
	buf = append(buf, ftyp...)

	moov := buildFallbackMoov(t)
	buf = append(buf, moov...)

	_, err = f.Write(buf)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	f, err = os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	info, err := analyzeMP4Fallback(f)
	require.NoError(t, err)
	assert.Equal(t, "mp4", info.Container)
	assert.Equal(t, "h264", info.VideoCodec)
	assert.Equal(t, 1920, info.Width)
	assert.Equal(t, 1080, info.Height)
	assert.Equal(t, "aac", info.AudioCodec)
	assert.Equal(t, 2, info.AudioChannels)
	assert.Equal(t, 48000, info.SampleRate)
	assert.Greater(t, info.Duration, 0.0)
}

func buildFallbackMoov(t *testing.T) []byte {
	t.Helper()

	mvhdData := make([]byte, 100)
	mvhdData[0] = 0
	binary.BigEndian.PutUint32(mvhdData[12:16], 24000)
	binary.BigEndian.PutUint32(mvhdData[16:20], 72000)
	mvhdBox := buildBox("mvhd", mvhdData)

	videoTrak := buildFallbackTrak(t, "vide", 24000, 72000, 1920, 1080, "avc1", 0, 0, "")
	audioTrak := buildFallbackTrak(t, "soun", 24000, 72000, 0, 0, "", 2, 48000, "mp4a")

	return buildBox("moov", append(mvhdBox, append(videoTrak, audioTrak...)...))
}

func buildFallbackTrak(t *testing.T, handler string, timescale uint32, duration uint64, w, h uint16, vcodec string, channels uint16, sampleRate uint16, acodec string) []byte {
	t.Helper()

	hdlrData := make([]byte, 24)
	copy(hdlrData[8:12], handler)
	hdlrBox := buildBox("hdlr", hdlrData)

	mdhdData := make([]byte, 24)
	mdhdData[0] = 0
	binary.BigEndian.PutUint32(mdhdData[12:16], timescale)
	binary.BigEndian.PutUint32(mdhdData[16:20], uint32(duration))
	mdhdBox := buildBox("mdhd", mdhdData)

	var stsdBox []byte
	if handler == "vide" {
		entry := make([]byte, 86)
		binary.BigEndian.PutUint32(entry[0:4], uint32(len(entry)))
		copy(entry[4:8], vcodec)
		binary.BigEndian.PutUint16(entry[32:34], w)
		binary.BigEndian.PutUint16(entry[34:36], h)
		versionFlags := []byte{0, 0, 0, 0}
		entryCount := []byte{0, 0, 0, 1}
		stsdBox = buildBox("stsd", append(versionFlags, append(entryCount, entry...)...))
	} else {
		entry := make([]byte, 36)
		binary.BigEndian.PutUint32(entry[0:4], uint32(len(entry)))
		copy(entry[4:8], acodec)
		binary.BigEndian.PutUint16(entry[24:26], channels)
		binary.BigEndian.PutUint16(entry[32:34], sampleRate)
		versionFlags := []byte{0, 0, 0, 0}
		entryCount := []byte{0, 0, 0, 1}
		stsdBox = buildBox("stsd", append(versionFlags, append(entryCount, entry...)...))
	}

	sttsData := make([]byte, 16)
	binary.BigEndian.PutUint32(sttsData[4:8], 1)
	binary.BigEndian.PutUint32(sttsData[8:12], 1000)
	binary.BigEndian.PutUint32(sttsData[12:16], 1)
	sttsBox := buildBox("stts", sttsData)

	stblBox := buildBox("stbl", append(stsdBox, sttsBox...))
	minfBox := buildBox("minf", stblBox)
	mdiaBox := buildBox("mdia", append(mdhdBox, append(hdlrBox, minfBox...)...))
	return buildBox("trak", mdiaBox)
}

func buildBox(boxType string, data []byte) []byte {
	size := uint32(8 + len(data))
	header := make([]byte, 8)
	binary.BigEndian.PutUint32(header[0:4], size)
	copy(header[4:8], boxType)
	return append(header, data...)
}
