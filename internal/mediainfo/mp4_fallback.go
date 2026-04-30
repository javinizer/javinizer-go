package mediainfo

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

type mp4Box struct {
	boxType    string
	size       uint64
	headerSize int
	data       []byte
}

type mp4BoxReader struct {
	r io.ReadSeeker
}

func newMP4BoxReader(f *os.File) *mp4BoxReader {
	return &mp4BoxReader{r: f}
}

func (mr *mp4BoxReader) readBox() (*mp4Box, error) {
	var header [8]byte
	if _, err := io.ReadFull(mr.r, header[:]); err != nil {
		return nil, err
	}

	headerSize := 8
	size := uint64(binary.BigEndian.Uint32(header[0:4]))
	boxType := string(header[4:8])

	switch size {
	case 1:
		var extSize [8]byte
		if _, err := io.ReadFull(mr.r, extSize[:]); err != nil {
			return nil, err
		}
		size = binary.BigEndian.Uint64(extSize[:])
		headerSize = 16
	case 0:
		stat, err := mr.r.Seek(0, io.SeekCurrent)
		if err != nil {
			return nil, err
		}
		end, _ := mr.r.Seek(0, io.SeekEnd)
		_, _ = mr.r.Seek(stat, io.SeekStart)
		size = uint64(end) - uint64(stat) + uint64(headerSize)
	}

	dataSize := size - uint64(headerSize)
	readLimit := uint64(10 * 1024 * 1024)
	if boxType == "moov" {
		readLimit = 64 * 1024 * 1024
	}
	if dataSize > readLimit {
		return &mp4Box{boxType: boxType, size: size, headerSize: headerSize}, nil
	}

	data := make([]byte, dataSize)
	if _, err := io.ReadFull(mr.r, data); err != nil {
		return nil, err
	}

	return &mp4Box{boxType: boxType, size: size, headerSize: headerSize, data: data}, nil
}

func (mr *mp4BoxReader) skipBox(box *mp4Box) error {
	if box.data != nil {
		return nil
	}
	dataSize := int64(box.size) - int64(box.headerSize)
	if dataSize <= 0 {
		return nil
	}
	_, err := mr.r.Seek(dataSize, io.SeekCurrent)
	return err
}

func findBox(data []byte, target string) []byte {
	off := 0
	for off+8 <= len(data) {
		size := int(binary.BigEndian.Uint32(data[off : off+4]))
		btype := string(data[off+4 : off+8])
		if size < 8 || off+size > len(data) {
			return nil
		}
		if btype == target {
			return data[off+8 : off+size]
		}
		off += size
	}
	return nil
}

func findBoxes(data []byte, target string) [][]byte {
	var results [][]byte
	off := 0
	for off+8 <= len(data) {
		size := int(binary.BigEndian.Uint32(data[off : off+4]))
		btype := string(data[off+4 : off+8])
		if size < 8 || off+size > len(data) {
			break
		}
		if btype == target {
			results = append(results, data[off+8:off+size])
		}
		off += size
	}
	return results
}

func parseMvhd(data []byte) (timescale uint32, duration uint64) {
	if len(data) < 100 {
		return 0, 0
	}
	version := data[0]
	if version == 0 {
		timescale = binary.BigEndian.Uint32(data[12:16])
		duration = uint64(binary.BigEndian.Uint32(data[16:20]))
	} else {
		timescale = binary.BigEndian.Uint32(data[20:24])
		duration = binary.BigEndian.Uint64(data[24:32])
	}
	return
}

func parseMdhd(data []byte) (timescale uint32, duration uint64) {
	if len(data) < 24 {
		return 0, 0
	}
	version := data[0]
	if version == 0 {
		timescale = binary.BigEndian.Uint32(data[12:16])
		duration = uint64(binary.BigEndian.Uint32(data[16:20]))
	} else {
		timescale = binary.BigEndian.Uint32(data[20:24])
		duration = binary.BigEndian.Uint64(data[24:32])
	}
	return
}

func parseHdlr(data []byte) string {
	if len(data) < 12 {
		return ""
	}
	return string(data[8:12])
}

func parseStts(data []byte) (totalSamples uint32, totalDelta uint64) {
	if len(data) < 8 {
		return 0, 0
	}
	entryCount := int(binary.BigEndian.Uint32(data[4:8]))
	off := 8
	for i := 0; i < entryCount && off+8 <= len(data); i++ {
		count := binary.BigEndian.Uint32(data[off : off+4])
		delta := binary.BigEndian.Uint32(data[off+4 : off+8])
		totalSamples += count
		totalDelta += uint64(count) * uint64(delta)
		off += 8
	}
	return
}

func parseStsdVideo(data []byte) (codec string, width, height uint16) {
	if len(data) < 8 {
		return "", 0, 0
	}
	entryCount := binary.BigEndian.Uint32(data[4:8])
	if entryCount == 0 {
		return "", 0, 0
	}
	off := 8
	if off+8 > len(data) {
		return "", 0, 0
	}
	codec = string(data[off+4 : off+8])
	if off+32 > len(data) {
		return codec, 0, 0
	}
	width = binary.BigEndian.Uint16(data[off+32 : off+34])
	height = binary.BigEndian.Uint16(data[off+34 : off+36])
	return codec, width, height
}

func parseStsdAudio(data []byte) (codec string, channels, sampleRate uint16) {
	if len(data) < 8 {
		return "", 0, 0
	}
	entryCount := binary.BigEndian.Uint32(data[4:8])
	if entryCount == 0 {
		return "", 0, 0
	}
	off := 8
	if off+8 > len(data) {
		return "", 0, 0
	}
	codec = string(data[off+4 : off+8])
	if off+28 > len(data) {
		return codec, 0, 0
	}
	channels = binary.BigEndian.Uint16(data[off+24 : off+26])
	sampleRate = binary.BigEndian.Uint16(data[off+32 : off+34])
	return codec, channels, sampleRate
}

func analyzeMP4Fallback(f *os.File) (*VideoInfo, error) {
	info := &VideoInfo{
		Container: "mp4",
	}

	stat, _ := f.Stat()
	fileSize := stat.Size()

	_, _ = f.Seek(0, io.SeekStart)
	mr := newMP4BoxReader(f)

	var movieTimescale uint32
	var movieDuration uint64

	for {
		box, err := mr.readBox()
		if err != nil {
			break
		}

		switch box.boxType {
		case "moov":
			if box.data == nil {
				_ = mr.skipBox(box)
				continue
			}

			mvhdData := findBox(box.data, "mvhd")
			if mvhdData != nil {
				movieTimescale, movieDuration = parseMvhd(mvhdData)
			}

			for _, trakData := range findBoxes(box.data, "trak") {
				mdiaData := findBox(trakData, "mdia")
				if mdiaData == nil {
					continue
				}

				hdlrData := findBox(mdiaData, "hdlr")
				if hdlrData == nil {
					continue
				}
				handlerType := parseHdlr(hdlrData)

				var trackTimescale uint32
				var trackDuration uint64
				mdhdData := findBox(mdiaData, "mdhd")
				if mdhdData != nil {
					trackTimescale, trackDuration = parseMdhd(mdhdData)
				}

				minfData := findBox(mdiaData, "minf")
				if minfData == nil {
					continue
				}
				stblData := findBox(minfData, "stbl")
				if stblData == nil {
					continue
				}

				switch handlerType {
				case "vide":
					stsdData := findBox(stblData, "stsd")
					if stsdData != nil {
						codec, w, h := parseStsdVideo(stsdData)
						if info.VideoCodec == "" {
							info.VideoCodec = mapMP4VideoCodec(codec)
							info.Width = int(w)
							info.Height = int(h)
						}
					}

					sttsData := findBox(stblData, "stts")
					if sttsData != nil {
						totalSamples, totalDelta := parseStts(sttsData)
						if totalDelta > 0 && trackTimescale > 0 {
							fps := float64(totalSamples) * float64(trackTimescale) / float64(totalDelta)
							avgDelta := float64(totalDelta) / float64(totalSamples)
							if isFieldBasedDelta(avgDelta, float64(trackTimescale)) {
								fps /= 2
							}
							if info.FrameRate == 0 {
								info.FrameRate = fps
							}
						}
					}

					if trackTimescale > 0 && trackDuration > 0 {
						info.Duration = float64(trackDuration) / float64(trackTimescale)
					}

				case "soun":
					if info.AudioCodec == "" {
						stsdData := findBox(stblData, "stsd")
						if stsdData != nil {
							codec, channels, sampleRate := parseStsdAudio(stsdData)
							info.AudioCodec = mapMP4AudioCodec(codec)
							info.AudioChannels = int(channels)
							info.SampleRate = int(sampleRate)
							if info.AudioChannels == 0 {
								info.AudioChannels = 2
							}
						}
					}
				}
			}

		case "mdat":
			_ = mr.skipBox(box)

		default:
			if box.data == nil {
				_ = mr.skipBox(box)
			}
		}
	}

	if info.Duration == 0 && movieTimescale > 0 && movieDuration > 0 {
		info.Duration = float64(movieDuration) / float64(movieTimescale)
	}

	if info.Duration > 0 && fileSize > 0 {
		info.Bitrate = int((float64(fileSize) * 8) / info.Duration / 1000)
	}

	if info.Width > 0 && info.Height > 0 {
		info.AspectRatio = float64(info.Width) / float64(info.Height)
	}

	if info.VideoCodec == "" && info.AudioCodec == "" && info.Duration == 0 {
		return nil, fmt.Errorf("failed to extract MP4/MOV metadata: no usable data found")
	}

	return info, nil
}

func isFieldBasedDelta(avgDelta, timescale float64) bool {
	fieldRates := []struct {
		fps       float64
		tolerance float64
	}{
		{59.94, 0.5},
		{60.0, 0.5},
		{50.0, 0.5},
	}

	for _, fr := range fieldRates {
		expectedFieldDelta := timescale / fr.fps
		if avgDelta > expectedFieldDelta-fr.tolerance && avgDelta < expectedFieldDelta+fr.tolerance {
			return true
		}
	}

	return false
}
