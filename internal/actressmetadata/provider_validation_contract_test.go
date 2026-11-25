package actressmetadata

import (
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/actresscache"
)

func TestSnapshotProviderRejectsMalformedIdentityBeforeGivingEvidence(t *testing.T) {
	snapshot := fixture(t)
	for _, tc := range []struct {
		name string
		q    Query
		want error
	}{
		{"claim", Query{Claim: &actresscache.ExternalID{Namespace: "dmm", Value: ""}}, identityError("dmm", "")},
		{"other-source reference", ref("other", ""), identityError("other", "")},
	} {
		for _, state := range []struct {
			name string
			snap *actresscache.Snapshot
		}{{"pinned", snapshot}, {"absent", nil}} {
			t.Run(tc.name+"/"+state.name, func(t *testing.T) {
				r, err := provider(t, state.snap, "fixture_a").LookupEvidence(t.Context(), tc.q)
				if r != (Result{}) || tc.want == nil || err == nil || err.Error() != tc.want.Error() {
					t.Fatalf("got (%+v, %v), want zero evidence and %v", r, err, tc.want)
				}
			})
		}
	}
	// A malformed claim is rejected even before touching the receiver.
	var noProvider *SnapshotProvider
	r, err := noProvider.LookupEvidence(t.Context(), claim(""))
	if r != (Result{}) || err == nil {
		t.Fatalf("got (%+v, %v), want zero evidence and an identity error", r, err)
	}

	both := ref("fixture_a", "actor-1")
	both.Claim = claim("7").Claim
	r, err = provider(t, snapshot, "fixture_a").LookupEvidence(t.Context(), both)
	if r != (Result{}) || err == nil || !strings.Contains(err.Error(), "exactly one reference or claim") {
		t.Fatalf("got (%+v, %v), want zero evidence and shape error", r, err)
	}
}

func identityError(namespace, value string) error {
	_, err := actresscache.SourceKey(namespace, value)
	return err
}
