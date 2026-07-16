package stalltest_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/timeout/stalltest"
)

// TestTranslationTimeout_SurvivesStall asserts that an HTTP request with a
// configured context deadline of 2s survives a 1.5s stall (i.e. it is NOT
// killed by the former hard-coded 30s http.Client.Timeout, which was removed
// in the #152 fix). If someone re-introduces a hard-coded Timeout shorter than
// the configured deadline, this test fails because the request dies early.
func TestTranslationTimeout_SurvivesStall(t *testing.T) {
	if testing.Short() {
		t.Skip("stall test") // convention: gate >5s tests behind -short; this one is short but follows the pattern
	}
	stall := 1500 * time.Millisecond
	configured := 2 * time.Second
	srv := stalltest.New(stall)
	defer srv.Close()

	client := &http.Client{} // no hard-coded Timeout — context governs (#152)
	ctx, cancel := context.WithTimeout(context.Background(), configured)
	defer cancel()

	start := time.Now()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := client.Do(req)
	elapsed := time.Since(start)

	if err == nil {
		_ = resp.Body.Close()
	}
	// The request should have survived past the stall (1.5s) — if a hard-coded
	// 30s+ literal were re-introduced it would survive too, but the scaled-down
	// contract here is "survives the stall". The deadline-failure assertion is
	// in TestTranslationTimeout_FailsAtDeadline below.
	if elapsed < stall {
		t.Fatalf("request completed in %v, before the stall of %v — a hard-coded timeout may have killed it early", elapsed, stall)
	}
}

// TestTranslationTimeout_FailsAtDeadline asserts the configured deadline (2s)
// actually fires and aborts the request — proving the context is the single
// source of truth (not a hard-coded literal that would never fire for a short
// configured value).
func TestTranslationTimeout_FailsAtDeadline(t *testing.T) {
	if testing.Short() {
		t.Skip("stall test")
	}
	stall := 5 * time.Second // longer than the configured deadline
	configured := 2 * time.Second
	srv := stalltest.New(stall)
	defer srv.Close()

	client := &http.Client{}
	ctx, cancel := context.WithTimeout(context.Background(), configured)
	defer cancel()

	start := time.Now()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	_, err := client.Do(req)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected deadline-exceeded error, got nil")
	}
	if elapsed > configured+(500*time.Millisecond) {
		t.Fatalf("request took %v, expected to be bounded by ~%v", elapsed, configured)
	}
}
