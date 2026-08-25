package worker

import (
	"context"
	"errors"
	"sync"
)

// jobPersistFlight serializes envelope persistence per job while coalescing
// requests that arrive during an active commit. The callback must take a fresh
// snapshot on every invocation; the flight never stores a stale snapshot.
type jobPersistFlight struct {
	mu            sync.Mutex
	active        bool
	exclusive     bool
	exclusiveDone chan struct{}
	exclusiveErr  error
	dirty         bool
	idle          chan struct{}
	waiters       []chan error
}

func newJobPersistFlight() *jobPersistFlight {
	return &jobPersistFlight{}
}

func (f *jobPersistFlight) do(ctx context.Context, persist func() error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if persist == nil {
		return errors.New("job persist flight requires a callback")
	}

	result := make(chan error, 1)
	f.mu.Lock()
	if f.exclusive {
		done := f.exclusiveDone
		exclusiveErr := f.exclusiveErr
		f.mu.Unlock()
		if exclusiveErr != nil {
			return exclusiveErr
		}
		if done == nil {
			return ErrJobBusy
		}
		select {
		case <-done:
			return f.do(ctx, persist)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	owner := !f.active
	if owner {
		f.active = true
		f.idle = make(chan struct{})
	} else {
		f.dirty = true
	}
	f.waiters = append(f.waiters, result)
	f.mu.Unlock()

	if !owner {
		select {
		case err := <-result:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	for {
		err := persist()

		f.mu.Lock()
		if err == nil && f.dirty {
			// A request arrived while the callback was running. Clear only the
			// coalescing marker; keep all waiters attached to the next fresh pass.
			f.dirty = false
			f.mu.Unlock()
			continue
		}

		waiters := f.waiters
		idle := f.idle
		f.waiters = nil
		f.idle = nil
		f.active = false
		f.dirty = false
		f.mu.Unlock()
		if idle != nil {
			close(idle)
		}

		for _, waiter := range waiters {
			waiter <- err
			close(waiter)
		}
		return err
	}
}

// acquireExclusive fences new persistence requests and waits for the active
// owner (including its fresh coalesced follow-up) to finish. The caller owns
// the returned release function; a successful delete intentionally keeps the
// fence closed, while a failed delete releases it so the job remains usable.
func (f *jobPersistFlight) acquireExclusive(ctx context.Context) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	if f.exclusive {
		sealedErr := f.exclusiveErr
		f.mu.Unlock()
		if sealedErr != nil {
			return nil, sealedErr
		}
		return nil, ErrJobBusy
	}
	f.exclusive = true
	done := make(chan struct{})
	f.exclusiveDone = done
	f.exclusiveErr = nil
	idle := f.idle
	f.mu.Unlock()
	if idle != nil {
		select {
		case <-idle:
		case <-ctx.Done():
			f.releaseExclusive()
			return nil, ctx.Err()
		}
	}
	return func() { f.releaseExclusive() }, nil
}

func (f *jobPersistFlight) releaseExclusive() {
	f.finishExclusive(nil, false)
}

// sealExclusive permanently closes the flight with err (used after a
// successful deletion; callers holding an old BatchJob pointer receive the
// typed gone outcome rather than starting a new persist).
func (f *jobPersistFlight) sealExclusive(err error) {
	f.finishExclusive(err, true)
}

func (f *jobPersistFlight) finishExclusive(err error, permanent bool) {
	f.mu.Lock()
	if !f.exclusive {
		f.mu.Unlock()
		return
	}
	done := f.exclusiveDone
	f.exclusiveErr = err
	if !permanent {
		f.exclusive = false
		f.exclusiveDone = nil
	}
	f.mu.Unlock()
	if done != nil {
		close(done)
	}
}
