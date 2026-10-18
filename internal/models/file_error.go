package models

// File-result error codes for the organize credit gate. These are distinct
// from scraper failures: "blocked" means the metadata source denied access.
const (
	FileErrorCodeCreditConflict          = "credit_conflict"
	FileErrorCodeCreditStatusUnavailable = "credit_status_unavailable"
)
