package scrape

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestContextErrorForJSONOverridesTypedError(t *testing.T) {
	err := contextErrorForJSON(context.DeadlineExceeded)
	assert.Equal(t, models.ScraperErrorKindUnavailable, err.Kind)
	assert.Equal(t, context.DeadlineExceeded.Error(), err.Message)
	assert.ErrorIs(t, err.Cause, context.DeadlineExceeded)
	assert.True(t, err.Retryable)
	assert.True(t, err.Temporary)
}

func TestHasOutputToken(t *testing.T) {
	assert.True(t, hasOutputToken("stderr", "stderr"))
	assert.True(t, hasOutputToken("stdout,stderr", "stderr"))
	assert.True(t, hasOutputToken("stderr,file.log", "stderr"))
	assert.False(t, hasOutputToken("/var/log/javinizer-stderr.log", "stderr"))
	assert.False(t, hasOutputToken("stdout,file.log", "stderr"))
	assert.False(t, hasOutputToken("", "stderr"))
}

func TestRemoveStdoutFromLogOutput(t *testing.T) {
	assert.Equal(t, "", removeStdoutFromLogOutput(""))
	assert.Equal(t, "", removeStdoutFromLogOutput("stdout"))
	assert.Equal(t, "file.log", removeStdoutFromLogOutput("stdout,file.log"))
	assert.Equal(t, "file.log", removeStdoutFromLogOutput("file.log,stdout"))
	assert.Equal(t, "file.log,stderr", removeStdoutFromLogOutput("stdout,file.log,stderr"))
	assert.Equal(t, "file.log", removeStdoutFromLogOutput("file.log"))
}

func TestScraperErrorToEnvelope_NilError(t *testing.T) {
	e := scraperErrorToEnvelope(nil)
	assert.Equal(t, "unknown", e.Kind)
}

func TestScraperErrorToEnvelope_EmptyKindBecomesUnknown(t *testing.T) {
	e := scraperErrorToEnvelope(&models.ScraperError{Message: "panic"})
	assert.Equal(t, "unknown", e.Kind)
}

func TestScraperErrorToEnvelope_UsesErrorWhenMessageEmpty(t *testing.T) {
	e := scraperErrorToEnvelope(&models.ScraperError{Scraper: "test", StatusCode: 500})
	assert.NotEmpty(t, e.Message)
}

func TestUnknownErrorEnvelope(t *testing.T) {
	wrap := unknownErrorEnvelope("something broke")
	assert.Equal(t, "unknown", wrap.Error.Kind)
	assert.Equal(t, "something broke", wrap.Error.Message)
}

func TestValidateJSONMode_EmptyScraperName(t *testing.T) {
	err := validateJSONMode([]string{""}, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "non-empty")
}
