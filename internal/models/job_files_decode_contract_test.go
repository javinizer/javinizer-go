package models_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestJobParseFilesEmptyPreservesDestination(t *testing.T) {
	job := models.Job{ID: "empty-files", Status: models.JobStatusRunning, Results: `{"retained":true}`}
	before := job
	destination := []string{"/owned/keep-first.mp4", "/owned/keep-second.mp4"}
	want := append([]string(nil), destination...)

	require.NoError(t, job.ParseFiles(&destination))
	require.Equal(t, want, destination, "an absent payload is a no-op, not an empty replacement list")
	require.Equal(t, before, job)
}

func TestJobParseFilesSyntaxFailurePreservesDestination(t *testing.T) {
	job := models.Job{ID: "truncated-files", Files: `["/owned/replacement.mp4",`, Excluded: `{"retained":true}`}
	before := job
	destination := []string{"/owned/keep-first.mp4", "/owned/keep-second.mp4"}
	want := append([]string(nil), destination...)

	err := job.ParseFiles(&destination)
	var syntaxErr *json.SyntaxError
	require.ErrorAs(t, err, &syntaxErr)
	require.ErrorIs(t, err, syntaxErr, "the contextual diagnostic must retain the original JSON error")
	require.EqualError(t, err, "failed to parse files for job truncated-files: "+syntaxErr.Error())
	// encoding/json validates syntax before decoding. This assertion is
	// deliberately not a general atomicity promise for JSON type errors.
	require.Equal(t, want, destination)
	require.Equal(t, before, job)
}

func TestJobParseFilesInvalidDestinationCanRetry(t *testing.T) {
	job := models.Job{
		ID: "retry-files", Status: models.JobStatusCompleted,
		Files: `["/媒体/映画.mp4","/媒体/予告.mp4","C:\\動画\\作品.mp4"]`,
	}
	before := job
	var invalidDestination *[]string
	err := job.ParseFiles(invalidDestination)
	var invalidErr *json.InvalidUnmarshalError
	require.ErrorAs(t, err, &invalidErr)
	require.ErrorIs(t, err, invalidErr)
	require.Equal(t, reflect.TypeOf(invalidDestination), invalidErr.Type)
	require.EqualError(t, err, "failed to parse files for job retry-files: "+invalidErr.Error())
	require.Nil(t, invalidDestination)
	require.Equal(t, before, job)

	var destination []string
	require.NoError(t, job.ParseFiles(&destination))
	require.Equal(t, []string{"/媒体/映画.mp4", "/媒体/予告.mp4", `C:\動画\作品.mp4`}, destination)
	require.Equal(t, before, job, "decoding must not rewrite the stored payload or other job fields")
}
