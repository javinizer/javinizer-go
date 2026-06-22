package organizer

import (
	"fmt"
	"strings"
)

type LinkMode string

const (
	LinkModeNone LinkMode = ""
	LinkModeHard LinkMode = "hard"
	LinkModeSoft LinkMode = "soft"
)

func ParseLinkMode(raw string) (LinkMode, error) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	switch normalized {
	case "", "none":
		return LinkModeNone, nil
	case string(LinkModeHard):
		return LinkModeHard, nil
	case string(LinkModeSoft):
		return LinkModeSoft, nil
	default:
		return LinkModeNone, fmt.Errorf("invalid link mode %q (expected one of: none, hard, soft)", raw)
	}
}

func (m LinkMode) IsValid() bool {
	switch m {
	case LinkModeNone, LinkModeHard, LinkModeSoft:
		return true
	default:
		return false
	}
}
