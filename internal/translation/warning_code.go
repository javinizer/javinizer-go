package translation

import (
	"context"
	"errors"
	"fmt"
)

// TranslationWarningCode is the machine-readable classification of a
// translation failure or degradation. It is serialized as a plain string on
// ScrapeResult, OrchestrationState, and API responses. The taxonomy is
// deliberately parallel to models.ScraperErrorKind: "unavailable" means
// "no usable response" in both, while translation "forbidden" (403,
// API-key/authz semantics) differs from scrape "blocked" (IP/robot blocking).
type TranslationWarningCode string

// The closed translation warning-code taxonomy: rate_limited (429),
// unauthorized (401), forbidden (403), request_error (other 4xx),
// service_error (5xx), unavailable (no usable response: transport, parse,
// count mismatch, timeout), degraded (partial per-field fallback), and
// unknown (fallback for any unclassified error).
const (
	TranslationWarningRateLimited  TranslationWarningCode = "rate_limited"
	TranslationWarningUnauthorized TranslationWarningCode = "unauthorized"
	TranslationWarningForbidden    TranslationWarningCode = "forbidden"
	TranslationWarningRequestError TranslationWarningCode = "request_error"
	TranslationWarningServiceError TranslationWarningCode = "service_error"
	TranslationWarningUnavailable  TranslationWarningCode = "unavailable"
	TranslationWarningDegraded     TranslationWarningCode = "degraded"
	TranslationWarningUnknown      TranslationWarningCode = "unknown"
)

// classifyTranslationWarning maps a provider failure to a stable warning code
// and a human-readable message. The live request context is checked FIRST:
// providers wrap transport errors into static typed errors that discard the
// context cause, so ctx.Err() is the only authoritative source for
// deadline/cancellation. A deadline classifies as unavailable with a
// "translation timed out" message; a cancellation is suppressed (empty code,
// user-initiated abort attaches no per-file warning). With a live context the
// provider kind/status mapping applies; unknown is the fallback for any
// unclassified error. Messages never contain Go error sentinels, raw provider
// responses, request URLs, API keys, or source text.
func classifyTranslationWarning(ctx context.Context, provider, mode string, err error) (TranslationWarningCode, string) {
	if ctxErr := ctx.Err(); ctxErr != nil {
		if errors.Is(ctxErr, context.DeadlineExceeded) {
			return TranslationWarningUnavailable, fmt.Sprintf("Translation (%s): translation timed out", providerWarningLabel(provider, mode))
		}
		return "", ""
	}

	label := providerWarningLabel(provider, mode)
	var te *translationError
	if errors.As(err, &te) && te.Kind == TranslationErrorHTTPStatus {
		switch {
		case te.StatusCode == 429:
			return TranslationWarningRateLimited, fmt.Sprintf("Translation (%s): rate limited - retry later, switch provider, or configure paid mode/API key", label)
		case te.StatusCode == 401:
			return TranslationWarningUnauthorized, fmt.Sprintf("Translation (%s): unauthorized - check API key", label)
		case te.StatusCode == 403:
			return TranslationWarningForbidden, fmt.Sprintf("Translation (%s): access denied - check API key", label)
		case te.StatusCode >= 500:
			return TranslationWarningServiceError, fmt.Sprintf("Translation (%s): external service error", label)
		case te.StatusCode >= 400:
			return TranslationWarningRequestError, fmt.Sprintf("Translation (%s): request error", label)
		}
	}
	if errors.As(err, &te) {
		return TranslationWarningUnavailable, fmt.Sprintf("Translation (%s): provider unavailable or unusable response", label)
	}
	return TranslationWarningUnknown, fmt.Sprintf("Translation (%s): internal error", label)
}

// providerWarningLabel builds the human-readable provider+mode label used in
// warning messages (e.g. "Google Translate (free)"). Only Google carries a
// mode suffix today; other providers use their normalized name as-is.
func providerWarningLabel(provider, mode string) string {
	name := provider
	if provider == "google" {
		name = "Google Translate"
	}
	if mode != "" {
		return fmt.Sprintf("%s (%s)", name, mode)
	}
	return name
}

// safeErrorDetail renders a log-safe classification of a pipeline error: the
// typed kind plus HTTP status when present, never the raw error (which can
// embed request URLs or provider payloads).
func safeErrorDetail(err error) string {
	var te *translationError
	if errors.As(err, &te) {
		if te.Kind == TranslationErrorHTTPStatus && te.StatusCode > 0 {
			return fmt.Sprintf("%s status=%d", te.Kind, te.StatusCode)
		}
		return string(te.Kind)
	}
	return "untyped error"
}

// WarningStatusCode extracts the HTTP status from a translation pipeline error
// when (and only when) the warning classification was derived from that status.
// It returns ok=false when err is nil, untyped, typed-but-not-HTTP, or when the
// request context is already done (in which case classification came from the
// context, not from a status). Used to attach the optional status_code field to
// the structured per-movie warning log.
func WarningStatusCode(ctx context.Context, err error) (int, bool) {
	if err == nil || (ctx != nil && ctx.Err() != nil) {
		return 0, false
	}
	var te *translationError
	if errors.As(err, &te) && te.Kind == TranslationErrorHTTPStatus && te.StatusCode > 0 {
		return te.StatusCode, true
	}
	return 0, false
}
