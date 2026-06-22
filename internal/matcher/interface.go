package matcher

import "github.com/javinizer/javinizer-go/internal/models"

// MatcherInterface defines the contract for JAV ID extraction from filenames.
type MatcherInterface interface {
	Match(files []models.FileMatchInfo) []MatchResult
	MatchFile(file models.FileMatchInfo) *MatchResult
	MatchString(s string) string
}

var _ MatcherInterface = (*Matcher)(nil)
