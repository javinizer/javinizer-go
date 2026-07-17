package scrape

import "github.com/javinizer/javinizer-go/internal/models"

// jsonErrorEnvelope is the CLI-side wire DTO for scraper errors in JSON output mode.
// It deliberately does NOT use the internal ScraperError struct (which has a Cause
// error field that should not be serialized). Only the needed fields are mapped.
type jsonErrorEnvelope struct {
	Kind       string `json:"kind"`
	Message    string `json:"message"`
	StatusCode int    `json:"status_code"`
	Retryable  bool   `json:"retryable"`
	Temporary  bool   `json:"temporary"`
}

// jsonErrorWrapper wraps the envelope as {"error": {...}}.
type jsonErrorWrapper struct {
	Error jsonErrorEnvelope `json:"error"`
}

// scraperErrorToEnvelope maps a *models.ScraperError to the CLI-side JSON DTO.
// Uses Error() for the message when Message is empty, since cause/status-generated
// errors may have an empty Message field but a meaningful Error() string.
func scraperErrorToEnvelope(err *models.ScraperError) jsonErrorEnvelope {
	if err == nil {
		return jsonErrorEnvelope{Kind: string(models.ScraperErrorKindUnknown)}
	}
	msg := err.Message
	if msg == "" {
		msg = err.Error()
	}
	kind := err.Kind
	if kind == "" {
		kind = models.ScraperErrorKindUnknown
	}
	return jsonErrorEnvelope{
		Kind:       string(kind),
		Message:    msg,
		StatusCode: err.StatusCode,
		Retryable:  err.Retryable,
		Temporary:  err.Temporary,
	}
}

// unknownErrorEnvelope creates a generic unknown error envelope.
func unknownErrorEnvelope(msg string) jsonErrorWrapper {
	return jsonErrorWrapper{
		Error: jsonErrorEnvelope{
			Kind:    string(models.ScraperErrorKindUnknown),
			Message: msg,
		},
	}
}
