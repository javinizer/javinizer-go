package models

import "strings"

// FormatActressNameOptions holds configuration for actress name formatting.
type FormatActressNameOptions struct {
	JapaneseNames      bool
	FirstNameOrder     bool
	UnknownActress     string
	UnknownActressMode UnknownActressMode
}

// FormatActressName formats an actress name according to the given options.
// Supports Japanese name preference, configurable first-name order, and unknown actress handling.
func FormatActressName(actress Actress, opts FormatActressNameOptions) string {
	mode := opts.UnknownActressMode
	unknownActress := opts.UnknownActress
	if unknownActress == "" {
		unknownActress = "Unknown"
	}

	if opts.JapaneseNames && actress.JapaneseName != "" {
		return actress.JapaneseName
	}

	if actress.FirstName == "" && actress.LastName == "" {
		if actress.JapaneseName != "" {
			return actress.JapaneseName
		}
		if mode == UnknownActressModeSkip {
			return ""
		}
		return unknownActress
	}

	if opts.FirstNameOrder {
		if actress.FirstName != "" && actress.LastName != "" {
			return actress.FirstName + " " + actress.LastName
		}
		if actress.FirstName != "" {
			return actress.FirstName
		}
		return actress.LastName
	}

	if actress.FirstName != "" && actress.LastName != "" {
		return actress.LastName + " " + actress.FirstName
	}
	if actress.LastName != "" {
		return actress.LastName
	}
	return actress.FirstName
}

// SplitFullName splits a full name into (firstName, lastName).
// For single-word names, firstName is the word and lastName is empty.
// For multi-word names, the first word is firstName and the rest is lastName.
func SplitFullName(fullName string) (firstName, lastName string) {
	fullName = strings.TrimSpace(fullName)
	if fullName == "" {
		return "", ""
	}

	parts := strings.Fields(fullName)
	if len(parts) == 0 {
		return "", ""
	} else if len(parts) == 1 {
		return parts[0], ""
	} else if len(parts) == 2 {
		return parts[0], parts[1]
	} else {
		return parts[0], strings.Join(parts[1:], " ")
	}
}

// NormalizeActressNameKey normalizes an actress name for comparison purposes.
// It trims whitespace, lowercases, and collapses internal whitespace.
func NormalizeActressNameKey(name string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(name))), " ")
}

// formatActressNameSimple builds a display name from actress name components.
// This is the legacy simple format used by FullName() methods.
// Unlike FormatActressName, this function does not handle UnknownActressMode:
// it returns an empty string when all inputs are empty (no "Unknown" fallback).
func formatActressNameSimple(lastName, firstName, japaneseName string) string {
	if lastName != "" && firstName != "" {
		return lastName + " " + firstName
	}
	if firstName != "" {
		return firstName
	}
	if lastName != "" {
		return lastName
	}
	return japaneseName
}
