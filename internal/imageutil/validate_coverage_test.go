package imageutil

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidateRemoteImage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	if err := ValidateRemoteImage(context.Background(), server.URL); err == nil {
		t.Fatal("expected SSRF error for localhost, got nil")
	}
}

func TestValidateRemoteImageWithSafeClientRedirectBlocked(t *testing.T) {
	client := &http.Client{}
	err := ValidateRemoteImageWithSafeClient(context.Background(), client, "https://example.com/img.jpg", "test", "")
	if err == nil {
		t.Fatal("expected error for unreachable URL")
	}
}
