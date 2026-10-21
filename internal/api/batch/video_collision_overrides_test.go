package batch

import (
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/videocollision"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestIssue277AllReviewContractsRejectInvalidPolicy(t *testing.T) {
	bad := videocollision.Policy("keep-both")
	contractsByName := map[string]func(*contracts.ReviewApplyOverrides) error{
		"organize": func(overrides *contracts.ReviewApplyOverrides) error {
			_, err := reviewOverridesForOrganize(contracts.OrganizeRequest{Overrides: overrides})
			return err
		},
		"preview": func(overrides *contracts.ReviewApplyOverrides) error {
			_, err := reviewOverridesForPreview(contracts.OrganizePreviewRequest{Overrides: overrides})
			return err
		},
		"update": func(overrides *contracts.ReviewApplyOverrides) error {
			_, err := reviewOverridesForUpdate(contracts.UpdateRequest{Overrides: overrides})
			return err
		},
	}
	for name, validate := range contractsByName {
		t.Run(name, func(t *testing.T) {
			require.Error(t, validate(&contracts.ReviewApplyOverrides{VideoCollisionPolicy: &bad}))
			require.Error(t, validate(&contracts.ReviewApplyOverrides{VideoCollisionDecisions: map[string]videocollision.Decision{"source": {Policy: bad}}}))
		})
	}
}
