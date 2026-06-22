package models

// DeepLMode represents the API mode for DeepL translation.
//
// Note: This type remains in models because moving it to internal/translation
// would create a circular dependency (translation → models → translation).
// The scraperconfig extraction pattern (where models aliases scraperconfig types)
// cannot be applied here because translation imports models, preventing models
// from importing translation.
type DeepLMode string

const (
	// DeepLModeFree uses the free DeepL API endpoint.
	DeepLModeFree DeepLMode = "free"
	// DeepLModePro uses the professional DeepL API endpoint.
	DeepLModePro DeepLMode = "pro"
)

// GoogleMode represents the API mode for Google Translate.
//
// Note: This type remains in models for the same circular-dependency reason
// as DeepLMode above.
type GoogleMode string

const (
	// GoogleModeFree uses the free Google Translate API.
	GoogleModeFree GoogleMode = "free"
	// GoogleModePaid uses the paid Google Cloud Translation API.
	GoogleModePaid GoogleMode = "paid"
)
