package temp

import (
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
)

func TestPathWithinDirPR260(t *testing.T) {
	dir := filepath.Join("root", "posters")
	inside := filepath.Join(dir, "image.jpg")
	outside := filepath.Join("root", "outside.jpg")
	require.True(t, pathWithinDir(dir, inside))
	require.False(t, pathWithinDir(dir, outside))

	path, ok := resolvePosterPath(dir, "job", "image.jpg")
	require.True(t, ok)
	require.Equal(t, filepath.Join(dir, "job", "image.jpg"), path)
	_, ok = resolvePosterPath(dir, "..", "image.jpg")
	require.False(t, ok)
}

func TestPosterHandlersRejectUnsafeSegmentsPR260(t *testing.T) {
	croppedRecorder := httptest.NewRecorder()
	croppedContext, _ := gin.CreateTestContext(croppedRecorder)
	croppedContext.Params = gin.Params{{Key: "filename", Value: `unsafe\\name.jpg`}}
	serveCroppedPoster()(croppedContext)
	require.Equal(t, 404, croppedRecorder.Code)

	cfg := config.DefaultConfig(nil, nil)
	deps := newTestDeps(cfg)
	tempRecorder := httptest.NewRecorder()
	tempContext, _ := gin.CreateTestContext(tempRecorder)
	tempContext.Params = gin.Params{{Key: "jobId", Value: `unsafe\\job`}, {Key: "filename", Value: "image.jpg"}}
	serveTempPoster(testkit.GetTestRuntime(deps))(tempContext)
	require.Equal(t, 404, tempRecorder.Code)
}
