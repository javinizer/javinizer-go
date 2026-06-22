package models

// UnknownActressMode determines how to handle unknown actresses during metadata processing.
//
// Note: This type remains in models because moving it to internal/aggregator
// would create a circular dependency (aggregator → config → models → aggregator).
// The scraperconfig extraction pattern cannot be applied here because
// aggregator imports models, preventing models from importing aggregator.
type UnknownActressMode string

const (
	// UnknownActressModeSkip skips unknown actresses.
	UnknownActressModeSkip UnknownActressMode = "skip"
	// UnknownActressModeFallback uses fallback processing for unknown actresses.
	UnknownActressModeFallback UnknownActressMode = "fallback"
)
