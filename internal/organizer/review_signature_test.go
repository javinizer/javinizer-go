package organizer

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReviewNamingSignatureAcrossExecutionStrategies(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind strategyType
	}{
		{"organize", strategyOrganize},
		{"rename-in-place", strategyInPlace},
		{"rename-file", strategyInPlaceNoRenameFolder},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{FolderFormat: "<ID>", FileFormat: "<TITLE>", RenameFile: true}
			plan := &OrganizePlan{strategy: tc.kind}
			switch tc.kind {
			case strategyOrganize:
				plan.executeStrategy = &organizeStrategy{config: cfg}
			case strategyInPlace:
				plan.executeStrategy = &inPlaceStrategy{config: cfg}
			case strategyInPlaceNoRenameFolder:
				plan.executeStrategy = &inPlaceNoRenameFolderStrategy{config: cfg}
			}
			before := plan.ReviewNamingSignature()
			require.Len(t, before, 64)
			require.Equal(t, before, plan.ReviewNamingSignature())
			cfg.FileFormat = "<ID> - <TITLE>"
			require.NotEqual(t, before, plan.ReviewNamingSignature())
		})
	}
	for _, plan := range []*OrganizePlan{{}, {executeStrategy: &organizeStrategy{}}, {executeStrategy: &inPlaceStrategy{}}, {executeStrategy: &inPlaceNoRenameFolderStrategy{}}} {
		require.Empty(t, plan.ReviewNamingSignature())
	}
}

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
