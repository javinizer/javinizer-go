package downloader

import (
	"errors"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/models"
)

// ErrPosterRecropRequired classifies refusal independently of its retained cause.
var ErrPosterRecropRequired = errors.New("poster requires a fresh crop")

// PosterRecropRequiredCode is the stable asynchronous result error_code.
const PosterRecropRequiredCode = "poster_recrop_required"

// PosterRecropReason distinguishes missing proof from changed or unreadable bytes.
type PosterRecropReason string

// Identity refusal reasons are stable log and Go classification values.
const (
	SourceFingerprintMissing  PosterRecropReason = "source_fingerprint_missing"
	SourceFingerprintInvalid  PosterRecropReason = "source_fingerprint_invalid"
	SourceFingerprintMismatch PosterRecropReason = "source_fingerprint_mismatch"
	SourceIdentityUnavailable PosterRecropReason = "source_identity_unavailable"
)

// PosterRecropRequiredError carries a copy of attempted intent, never shared review state.
type PosterRecropRequiredError struct {
	Reason PosterRecropReason
	Bounds models.CropBounds
	Cause  error
}

func (e *PosterRecropRequiredError) Error() string {
	message := fmt.Sprintf("poster requires a fresh crop: %s", e.Reason)
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", message, e.Cause)
	}
	return message
}

// Is retains recrop classification while Unwrap exposes the measurement cause.
func (e *PosterRecropRequiredError) Is(target error) bool {
	return target == ErrPosterRecropRequired
}

func (e *PosterRecropRequiredError) Unwrap() error {
	return e.Cause
}
