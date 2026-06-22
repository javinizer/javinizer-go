package nfo

import "context"

// mediaAnalyzer extracts video/audio stream information from a video file.
type mediaAnalyzer interface {
	Analyze(ctx context.Context, filePath string) (*streamDetails, error)
}
