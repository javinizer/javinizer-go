package models

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenderNameWithSelectionDistinguishesCreditNamesFromCanonicalFallback(t *testing.T) {
	tests := []struct {
		name         string
		credit       MovieCredit
		useCredited  bool
		canonical    string
		wantName     string
		wantSelected bool
	}{
		{name: "override equal canonical", credit: MovieCredit{UserOverride: true, OverrideName: " Canonical Name "}, canonical: "Canonical Name", wantName: "Canonical Name", wantSelected: true},
		{name: "credited equal canonical", credit: MovieCredit{CreditedName: " Canonical Name "}, useCredited: true, canonical: "Canonical Name", wantName: "Canonical Name", wantSelected: true},
		{name: "canonical fallback", credit: MovieCredit{CreditedName: "Alias"}, canonical: "Canonical Name", wantName: "Canonical Name"},
		{name: "forced canonical fallback", credit: MovieCredit{CreditedName: "Alias", DisplayForceCanonical: true}, useCredited: true, canonical: "Canonical Name", wantName: "Canonical Name"},
		{name: "credited fallback without canonical", credit: MovieCredit{CreditedName: " Alias "}, canonical: "", wantName: "Alias", wantSelected: true},
		{name: "empty fallback", credit: MovieCredit{}, canonical: "", wantName: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName, gotSelected := tt.credit.RenderNameWithSelection(tt.useCredited, tt.canonical)
			require.Equal(t, tt.wantName, gotName)
			require.Equal(t, tt.wantSelected, gotSelected)
			require.Equal(t, tt.wantName, tt.credit.RenderName(tt.useCredited, tt.canonical))
		})
	}
}
