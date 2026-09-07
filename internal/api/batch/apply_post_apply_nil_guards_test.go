package batch

// Pins the nil-payload guards at the head of both post-apply audit hooks
// (apply_post_apply.go makeOrganizePostApplyAuditHook /
// makeUpdatePostApplyAuditHook): a nil ApplyFileContext, a context missing
// Movie, or a nil ApplyFileResult must return WITHOUT panicking and WITHOUT
// emitting any audit row, so the original apply error is never masked by a
// nil-panic in the secondary-event lane. Sibling of the detached-ctx pins in
// apply_config_builder_detached_audit_ctx_test.go; reuses its recording
// ctxEnforcingEmitter.

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
)

func TestPostApplyAuditHook_NilGuards(t *testing.T) {
	validAFC := &worker.ApplyFileContext{FilePath: "/f.mp4", Movie: &models.Movie{ID: "MOV-1"}}
	validAFR := &worker.ApplyFileResult{}
	cases := []struct {
		name string
		afc  *worker.ApplyFileContext
		afr  *worker.ApplyFileResult
	}{{"nil afc", nil, validAFR}, {"nil movie", &worker.ApplyFileContext{}, validAFR}, {"nil afr", validAFC, nil}}
	gen := uint64(7)
	for _, lane := range []string{"organize", "update"} {
		for _, tc := range cases {
			t.Run(lane+"/"+tc.name, func(t *testing.T) {
				emitter := &ctxEnforcingEmitter{}
				deps := &core.APIDeps{EventEmitter: emitter}
				hook := makeOrganizePostApplyAuditHook(deps, &stubControlledJob{}, &gen)
				if lane == "update" {
					hook = makeUpdatePostApplyAuditHook(deps, &stubControlledJob{}, &gen)
				}
				assert.NotPanics(t, func() { hook(context.Background(), tc.afc, tc.afr) })
				assert.Empty(t, emitter.calls, "nil guard must skip all audit emits")
				assert.Empty(t, emitter.drops)
			})
		}
	}
}
