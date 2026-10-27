package movie

import (
	"context"

	"github.com/javinizer/javinizer-go/internal/scrapeevidence"
)

// beginEvidence installs the API deadline before read admission. Default-off
// callers keep their original workflow/timeout order and strip inherited sessions.
func (d MovieDeps) beginEvidence(ctx context.Context, requested bool) (context.Context, func(), error) {
	var reader *scrapeevidence.Service
	if !requested {
		clean, release := reader.Begin(ctx, false)
		return clean, release, nil
	}
	cancel := func() {}
	if d.RequestTimeoutFn != nil {
		if timeout := d.RequestTimeoutFn(); timeout > 0 {
			ctx, cancel = context.WithTimeout(ctx, timeout)
		}
	}
	success := false
	defer func() {
		if !success {
			cancel()
		}
	}()
	var release func()
	var err error
	if d.BeginOfflineEvidence != nil {
		ctx, release, err = d.BeginOfflineEvidence(ctx, true)
	} else {
		ctx, release = reader.Begin(ctx, true)
	}
	if err != nil {
		return ctx, nil, err
	}
	success = true
	return ctx, func() { cancel(); release() }, nil
}

func evidenceAuditContext(ctx context.Context, requested bool) context.Context {
	if requested {
		return ctx
	}
	return context.Background()
}
func evidencePosterContext(request, operation context.Context, requested bool) context.Context {
	if requested {
		return operation
	}
	return request
}
