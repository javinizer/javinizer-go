package workflow

import (
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
)

// ResolvePreset validates and normalizes a preset string. Returns the
// lowercase normalized preset, or an error for invalid values. Returns
// ("", nil) for empty input. Per ADR-0045: validation and resolution are
// a single pass — callers use this instead of ValidatePreset.
func ResolvePreset(preset string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(preset))
	switch normalized {
	case "", "conservative", "gap-fill", "aggressive":
		return normalized, nil
	default:
		return "", fmt.Errorf("%w %q (expected one of: conservative, gap-fill, aggressive)", ErrInvalidPreset, preset)
	}
}

// ResolveOperationMode parses an operation mode string into an OperationMode.
// Returns OperationModeOrganize for empty input (default). Returns error for invalid values.
// Per ADR-0030: callers resolve at the factory boundary before constructing commands.
func ResolveOperationMode(mode string) (operationmode.OperationMode, error) {
	if mode == "" {
		return operationmode.OperationModeOrganize, nil
	}
	parsed, err := operationmode.ParseOperationMode(mode)
	if err != nil {
		return operationmode.OperationModeOrganize, fmt.Errorf("invalid operation mode %q: %w", mode, err)
	}
	return parsed, nil
}

// ResolveLinkMode parses a link mode string and returns the validated organizer.LinkMode.
// Returns LinkModeNone for empty input (no override). Returns error for invalid values.
// Per ADR-0030: callers resolve at the factory boundary before constructing commands.
func ResolveLinkMode(mode string) (organizer.LinkMode, error) {
	return organizer.ParseLinkMode(mode)
}

// ResolvedSeamStrings holds the typed, validated results of resolving all
// string-typed seam parameters. Per ADR-0030: Preset is resolved at the
// boundary — downstream code receives fully-resolved typed values with no
// preset field remaining.
type ResolvedSeamStrings struct {
	OperationMode  operationmode.OperationMode
	LinkMode       organizer.LinkMode
	ScalarStrategy nfo.MergeStrategy
	ArrayStrategy  bool
}

// SeamStringsInput collects the raw string-typed seam parameters from a
// caller (API request, CLI flags, TUI options). Not every field is required
// — empty strings mean "use the default" and are handled by the underlying
// Resolve* functions.
type SeamStringsInput struct {
	OperationMode  string
	LinkMode       string
	Preset         string
	ScalarStrategy string
	ArrayStrategy  string
}

// ResolveSeamStrings resolves all non-empty seam string inputs to their typed
// equivalents and returns the result. This is the single shared function that
// all boundaries (API, TUI, CLI) should call instead of calling individual
// Resolve* functions inline.
//
// Per ADR-0045: validation and resolution are now a single pass. The Resolve*
// and Parse* functions already return errors for invalid input, so a separate
// validation pass was redundant — it validated every field twice on the happy
// path. Errors are accumulated and returned as a single combined error.
func ResolveSeamStrings(in SeamStringsInput) (*ResolvedSeamStrings, error) {
	var errs []string

	resolvedOpMode, err := ResolveOperationMode(in.OperationMode)
	if err != nil {
		errs = append(errs, err.Error())
	}

	resolvedLinkMode, err := ResolveLinkMode(in.LinkMode)
	if err != nil {
		errs = append(errs, err.Error())
	}

	resolvedScalar, err := nfo.ParseScalarStrategy(in.ScalarStrategy)
	if err != nil {
		errs = append(errs, fmt.Errorf("invalid scalar strategy %q: %w", in.ScalarStrategy, err).Error())
	}
	if in.ScalarStrategy == "" {
		resolvedScalar = nfo.PreferNFO
	}

	resolvedArray, err := nfo.ParseArrayStrategy(in.ArrayStrategy)
	if err != nil {
		errs = append(errs, fmt.Errorf("invalid array strategy %q: %w", in.ArrayStrategy, err).Error())
	}
	if in.ArrayStrategy == "" {
		resolvedArray = true // default: merge
	}

	if in.Preset != "" {
		if _, err := ResolvePreset(in.Preset); err != nil {
			errs = append(errs, err.Error())
		}
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(errs, "; "))
	}

	// Per ADR-0030: resolve preset at the boundary. If preset is specified,
	// it overrides the individual strategy values.
	if in.Preset != "" {
		resolvedScalar, resolvedArray, err = nfo.ApplyPresetTyped(in.Preset, resolvedScalar, resolvedArray)
		if err != nil {
			return nil, err
		}
	}

	return &ResolvedSeamStrings{
		OperationMode:  resolvedOpMode,
		LinkMode:       resolvedLinkMode,
		ScalarStrategy: resolvedScalar,
		ArrayStrategy:  resolvedArray,
	}, nil
}
