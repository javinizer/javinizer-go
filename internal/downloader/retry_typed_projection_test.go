package downloader

import (
	"errors"
	"fmt"
	"net"
	"testing"
)

// typedDNSFailure exposes a resolver failure through errors.As without
// implementing net.Error itself, as adapters may do when translating errors.
type typedDNSFailure struct {
	cause *net.DNSError
}

func (e typedDNSFailure) Error() string { return "resolver adapter: " + e.cause.Error() }
func (e typedDNSFailure) As(target any) bool {
	if dns, ok := target.(**net.DNSError); ok {
		*dns = e.cause
		return true
	}
	return false
}

func TestRetryClassificationHonorsTypedDNSProjection(t *testing.T) {
	cause := &net.DNSError{Err: "resolver unavailable", Name: "media.invalid"}
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"adapter", typedDNSFailure{cause: cause}},
		{"wrapped adapter", fmt.Errorf("download failed: %w", typedDNSFailure{cause: cause})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var networkError net.Error
			if errors.As(tc.err, &networkError) {
				t.Fatal("fixture unexpectedly exposes the general net.Error interface")
			}
			var dnsError *net.DNSError
			if !errors.As(tc.err, &dnsError) || dnsError != cause {
				t.Fatal("fixture does not preserve the typed resolver cause")
			}
			if !isRetryableError(tc.err) {
				t.Fatal("typed DNS failures must remain retryable through adapters and wrappers")
			}
		})
	}

	for _, err := range []error{errors.New("invalid media"), fmt.Errorf("download failed: %w", errors.New("invalid media"))} {
		if isRetryableError(err) {
			t.Fatalf("ordinary application error must not be retried: %v", err)
		}
	}
}

func TestRetryOperationHonorsTypedDNSProjection(t *testing.T) {
	const target = "https://media.invalid/poster.jpg"
	t.Run("transient resolver failure recovers", func(t *testing.T) {
		op := &retryableOperation{} // Zero delays exercise retry policy without wall-time backoff.
		calls := 0
		cause := &net.DNSError{Err: "resolver unavailable", Name: "media.invalid"}
		err := op.ExecuteWithRetry(t.Context(), func() error {
			calls++
			if calls == 1 {
				return fmt.Errorf("resolve media: %w", typedDNSFailure{cause: cause})
			}
			return nil
		}, 3, target)
		if err != nil || calls != 2 {
			t.Fatalf("err=%v attempts=%d; want success after two attempts", err, calls)
		}
	})

	t.Run("exhaustion preserves final resolver cause", func(t *testing.T) {
		op := &retryableOperation{}
		calls := 0
		var lastFailure error
		var lastCause *net.DNSError
		err := op.ExecuteWithRetry(t.Context(), func() error {
			calls++
			lastCause = &net.DNSError{Err: fmt.Sprintf("resolver failure %d", calls), Name: "media.invalid"}
			lastFailure = fmt.Errorf("resolve media: %w", typedDNSFailure{cause: lastCause})
			return lastFailure
		}, 2, target)
		if calls != 3 {
			t.Fatalf("attempts=%d; want initial attempt plus two retries", calls)
		}
		if !errors.Is(err, lastFailure) {
			t.Fatalf("err=%v; want the final attempt's error preserved", err)
		}
		var dnsError *net.DNSError
		if !errors.As(err, &dnsError) || dnsError != lastCause {
			t.Fatalf("err=%v; want the final typed resolver cause", err)
		}
		want := fmt.Sprintf("download failed after 3 attempt(s): %s returned %s", target, lastFailure)
		if err.Error() != want {
			t.Fatalf("error=%q; want %q", err, want)
		}
	})

	t.Run("ordinary failure stops immediately", func(t *testing.T) {
		op := &retryableOperation{}
		calls := 0
		cause := errors.New("invalid media")
		err := op.ExecuteWithRetry(t.Context(), func() error {
			calls++
			return cause
		}, 3, target)
		if calls != 1 || !errors.Is(err, cause) {
			t.Fatalf("err=%v attempts=%d; want original failure after one attempt", err, calls)
		}
		want := fmt.Sprintf("download failed after 1 attempt(s): %s returned %s", target, cause)
		if err.Error() != want {
			t.Fatalf("error=%q; want %q", err, want)
		}
	})
}
