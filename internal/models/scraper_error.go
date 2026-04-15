package models

import (
	"errors"
	"fmt"
	"strings"
)

// ScraperErrorKind classifies scraper failures in a structured way.
type ScraperErrorKind string

const (
	ScraperErrorKindUnknown     ScraperErrorKind = "unknown"
	ScraperErrorKindNotFound    ScraperErrorKind = "not_found"
	ScraperErrorKindUnavailable ScraperErrorKind = "unavailable"
	ScraperErrorKindRateLimited ScraperErrorKind = "rate_limited"
	ScraperErrorKindBlocked     ScraperErrorKind = "blocked"
)

// ScraperError is a typed scraper failure that worker/UI layers can classify
// without brittle string parsing.
type ScraperError struct {
	Scraper    string
	Kind       ScraperErrorKind
	StatusCode int
	Message    string
	Temporary  bool
	Retryable  bool
	Cause      error
}

func (e *ScraperError) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Message) != "" {
		return e.Message
	}
	if e.StatusCode > 0 {
		if e.Scraper != "" {
			return fmt.Sprintf("%s returned status code %d", e.Scraper, e.StatusCode)
		}
		return fmt.Sprintf("scraper returned status code %d", e.StatusCode)
	}
	if e.Scraper != "" {
		return fmt.Sprintf("%s scraper error", e.Scraper)
	}
	return "scraper error"
}

func (e *ScraperError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// AsScraperError extracts a ScraperError from any wrapped error chain.
func AsScraperError(err error) (*ScraperError, bool) {
	if err == nil {
		return nil, false
	}
	var scraperErr *ScraperError
	if errors.As(err, &scraperErr) && scraperErr != nil {
		return scraperErr, true
	}
	return nil, false
}

// NewScraperNotFoundError builds a typed "not found" scraper error.
func NewScraperNotFoundError(scraper, message string) *ScraperError {
	return &ScraperError{
		Scraper:   scraper,
		Kind:      ScraperErrorKindNotFound,
		Message:   strings.TrimSpace(message),
		Temporary: false,
		Retryable: false,
	}
}

// NewScraperStatusError builds a typed scraper error from an HTTP status code.
func NewScraperStatusError(scraper string, statusCode int, message string) *ScraperError {
	kind, temporary, retryable := classifyScraperStatus(statusCode)
	return &ScraperError{
		Scraper:    scraper,
		Kind:       kind,
		StatusCode: statusCode,
		Message:    strings.TrimSpace(message),
		Temporary:  temporary,
		Retryable:  retryable,
	}
}

// NewScraperChallengeError builds a typed blocked error for anti-bot challenge pages
// (for example Cloudflare challenge interstitials served with HTTP 200).
func NewScraperChallengeError(scraper, message string) *ScraperError {
	message = strings.TrimSpace(message)
	if message == "" {
		message = fmt.Sprintf("%s returned a Cloudflare challenge page (request blocked)", scraper)
	}
	return &ScraperError{
		Scraper:   scraper,
		Kind:      ScraperErrorKindBlocked,
		Message:   message,
		Temporary: true,
		Retryable: true,
	}
}

func classifyScraperStatus(statusCode int) (ScraperErrorKind, bool, bool) {
	switch {
	case statusCode == 404:
		return ScraperErrorKindNotFound, false, false
	case statusCode == 429:
		return ScraperErrorKindRateLimited, true, true
	case statusCode == 403 || statusCode == 451:
		return ScraperErrorKindBlocked, false, false
	case statusCode >= 500 && statusCode <= 599:
		return ScraperErrorKindUnavailable, true, true
	default:
		return ScraperErrorKindUnknown, false, false
	}
}

type ScraperHTTPError struct {
	Scraper    string
	StatusCode int
	Message    string
}

func (e *ScraperHTTPError) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Message) != "" {
		return e.Message
	}
	if e.StatusCode > 0 {
		if e.Scraper != "" {
			return fmt.Sprintf("%s returned HTTP %d", e.Scraper, e.StatusCode)
		}
		return fmt.Sprintf("HTTP %d", e.StatusCode)
	}
	return "scraper HTTP error"
}

func NewScraperHTTPError(scraper string, statusCode int, message string) *ScraperHTTPError {
	return &ScraperHTTPError{
		Scraper:    scraper,
		StatusCode: statusCode,
		Message:    strings.TrimSpace(message),
	}
}
