package translation

import "fmt"

// translationErrorKind categorizes the type of failure in a translation operation.
// External consumers can use errors.As to extract a *translationError and
// switch on Kind to handle specific error scenarios.
type translationErrorKind string

const (
	// TranslationErrorParse indicates the provider response could not be parsed.
	// This typically means the response format is unexpected, malformed, or
	// the content does not match the expected structure. StatusCode is not set
	// for this kind.
	TranslationErrorParse translationErrorKind = "parse_error"

	// TranslationErrorCountMismatch indicates the provider returned a different
	// number of translations than the number of input texts. This can happen
	// with LLM-based providers that drop or merge entries. StatusCode is not
	// set for this kind.
	TranslationErrorCountMismatch translationErrorKind = "count_mismatch"

	// TranslationErrorHTTPStatus indicates the provider returned a non-2xx HTTP
	// status code. The StatusCode field contains the HTTP response status.
	// Common statuses: 400 (bad request), 403 (access denied), 429 (rate limited),
	// 500+ (server error).
	TranslationErrorHTTPStatus translationErrorKind = "http_status"

	// TranslationErrorProvider indicates a generic provider-level failure that
	// does not fit other categories. This includes nil provider results,
	// connection failures, and unexpected transport errors. StatusCode is not
	// set for this kind.
	TranslationErrorProvider translationErrorKind = "provider_error"
)

// translationError represents a typed error from the translation subsystem.
// External consumers can use errors.As to extract this type and inspect
// Kind, StatusCode, and Cause for structured error handling.
type translationError struct {
	Kind translationErrorKind
	// StatusCode holds the HTTP status code when Kind is TranslationErrorHTTPStatus.
	// For all other kinds, StatusCode is 0 and should be ignored.
	StatusCode int
	Message    string
	Cause      error
}

func (e *translationError) Error() string {
	if e == nil {
		return ""
	}
	msg := e.Message
	if msg == "" {
		msg = string(e.Kind)
	}
	if e.StatusCode > 0 {
		return fmt.Sprintf("%s (status %d)", msg, e.StatusCode)
	}
	return msg
}

func (e *translationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}
