package nfo

import (
	"context"

	"github.com/javinizer/javinizer-go/internal/mediainfo"
)

// defaultMediaAnalyzer wraps mediainfo.Analyze and converts the result to StreamDetails.
type defaultMediaAnalyzer struct{}

func (defaultMediaAnalyzer) Analyze(ctx context.Context, filePath string) (*streamDetails, error) {
	info, err := mediainfo.Analyze(ctx, filePath)
	if err != nil {
		return nil, err
	}

	details := &streamDetails{}

	if info.Width > 0 && info.Height > 0 {
		vs := videoStream{
			Codec:  info.VideoCodec,
			Aspect: info.AspectRatio,
			Width:  info.Width,
			Height: info.Height,
		}
		if info.Duration > 0 {
			vs.DurationInSeconds = int(info.Duration)
		}
		details.Video = []videoStream{vs}
	}

	if info.AudioCodec != "" {
		details.Audio = []audioStream{{
			Codec:    info.AudioCodec,
			Channels: info.AudioChannels,
		}}
	}

	if len(details.Video) == 0 && len(details.Audio) == 0 {
		return nil, nil
	}

	return details, nil
}
