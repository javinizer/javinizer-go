package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache/offline"
	"github.com/stretchr/testify/require"
)

func TestPublishFixtureGenerationRejectsInputWithoutActressBlock(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing-store")
	// A real input with no actress COPY block must fail conversion before
	// opening the destination, even when the destination is also invalid.
	digest, err := publishFixtureGeneration(t.Context(), strings.NewReader("SELECT 1;\n"), dir, time.Now().UTC(), offline.DefaultLimits())
	require.EqualError(t, err, "missing actress COPY block")
	require.Empty(t, digest)
	require.NoDirExists(t, dir)
}
