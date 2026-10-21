package organizer

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestIssue277ReviewNamingSignaturePolicySeparation(t *testing.T) {
	cfg := &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}
	plan := &OrganizePlan{strategy: strategyOrganize, executeStrategy: &organizeStrategy{config: cfg}}
	before := plan.ReviewNamingSignature()
	require.NotEmpty(t, before)
	cfg.AllowRevert = true
	cfg.PosterFormat = "different-artwork.jpg"
	cfg.MoveSubtitles = true
	require.Equal(t, before, plan.ReviewNamingSignature())
	cfg.MaxTitleLength = 99
	require.NotEqual(t, before, plan.ReviewNamingSignature())
	cfg.MaxTitleLength = 0
	cfg.FileFormat = "<id>"
	require.NotEqual(t, before, plan.ReviewNamingSignature())
}
