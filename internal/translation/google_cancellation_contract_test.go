package translation

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
)

type canceledGoogleClient struct{ calls atomic.Int32 }

func (c *canceledGoogleClient) Do(*http.Request) (*http.Response, error) {
	c.calls.Add(1)
	return nil, errors.New("unexpected HTTP call for canceled translation")
}

func TestGoogleFreeCanceledBatchNeverCallsHTTP(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	client := &canceledGoogleClient{}
	provider := NewGoogleProvider(Config{Google: googleConfig{Mode: models.GoogleModeFree}}, client)

	result, err := provider.Translate(ctx, "ja", "en", []string{"first source", "second source"})
	if !errors.Is(err, context.Canceled) || result != nil || client.calls.Load() != 0 {
		t.Fatalf("result=%+v err=%v HTTP calls=%d", result, err, client.calls.Load())
	}
}
