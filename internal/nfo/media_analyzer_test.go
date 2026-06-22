package nfo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubMediaAnalyzer struct {
	Details *streamDetails
	Err     error
}

func (s *stubMediaAnalyzer) Analyze(_ context.Context, _ string) (*streamDetails, error) {
	return s.Details, s.Err
}

func TestExtractStreamDetails_WithStubAnalyzer(t *testing.T) {
	expectedDetails := &streamDetails{
		Video: []videoStream{
			{Codec: "h264", Width: 1920, Height: 1080, DurationInSeconds: 3600},
		},
		Audio: []audioStream{
			{Codec: "aac", Channels: 2},
		},
	}

	stub := &stubMediaAnalyzer{Details: expectedDetails}
	gen := newGeneratorWithAnalyzer(nil, &Config{IncludeStreamDetails: true}, stub)

	result := gen.extractStreamDetails(context.Background(), "/fake/path.mp4")
	require.NotNil(t, result)
	assert.Equal(t, expectedDetails, result)
}

func TestExtractStreamDetails_WithStubAnalyzerError(t *testing.T) {
	stub := &stubMediaAnalyzer{Err: assert.AnError}
	gen := newGeneratorWithAnalyzer(nil, &Config{IncludeStreamDetails: true}, stub)

	result := gen.extractStreamDetails(context.Background(), "/fake/path.mp4")
	assert.Nil(t, result)
}

func TestExtractStreamDetails_WithStubAnalyzerNilDetails(t *testing.T) {
	stub := &stubMediaAnalyzer{Details: nil}
	gen := newGeneratorWithAnalyzer(nil, &Config{IncludeStreamDetails: true}, stub)

	result := gen.extractStreamDetails(context.Background(), "/fake/path.mp4")
	assert.Nil(t, result)
}

func TestNewGenerator_NilMediaAnalyzer_DefaultsToDefault(t *testing.T) {
	gen := NewGenerator(nil, defaultConfig())
	require.NotNil(t, gen)
	_, ok := gen.mediaAnalyzer.(defaultMediaAnalyzer)
	assert.True(t, ok, "expected defaultMediaAnalyzer when nil is passed")
}

func TestExtractStreamDetails_CancelledContext_WithStub(t *testing.T) {
	stub := &stubMediaAnalyzer{Details: &streamDetails{Video: []videoStream{{Codec: "h264"}}}}
	gen := newGeneratorWithAnalyzer(nil, &Config{IncludeStreamDetails: true}, stub)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := gen.extractStreamDetails(ctx, "/fake/path.mp4")
	assert.Nil(t, result)
}
