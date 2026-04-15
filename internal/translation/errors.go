package translation

import "fmt"

type TranslationErrorKind string

const (
	TranslationErrorParse         TranslationErrorKind = "parse_error"
	TranslationErrorCountMismatch TranslationErrorKind = "count_mismatch"
	TranslationErrorHTTPStatus    TranslationErrorKind = "http_status"
	TranslationErrorProvider      TranslationErrorKind = "provider_error"
)

type TranslationError struct {
	Kind       TranslationErrorKind
	StatusCode int
	Message    string
	Cause      error
}

func (e *TranslationError) Error() string {
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

func (e *TranslationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}
