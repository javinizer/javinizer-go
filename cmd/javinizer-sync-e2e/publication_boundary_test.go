package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/actresscache/artifactstore"
	"github.com/javinizer/javinizer-go/internal/actresscache/sources/r18dump"
	"github.com/stretchr/testify/require"
)

type fixturePublicationProbe struct {
	publish func(context.Context, io.Reader, r18dump.Receipt) (artifactstore.Result, error)
	close   func() error
}

func (p fixturePublicationProbe) Publish(ctx context.Context, r io.Reader, receipt r18dump.Receipt) (artifactstore.Result, error) {
	return p.publish(ctx, r, receipt)
}
func (p fixturePublicationProbe) Close() error { return p.close() }

func TestPublishFixtureArtifactOutcomeAndOwnership(t *testing.T) {
	publishErr := errors.New("publish failed")
	closeErr := errors.New("close failed")
	durable := artifactstore.Result{Published: true, FilesSynced: true, DirectorySynced: true}
	for _, tc := range []struct {
		name                           string
		result                         artifactstore.Result
		publishErr, errorOnClose, want error
		incomplete                     bool
	}{
		{name: "durable", result: durable},
		{name: "publish error closes store", publishErr: publishErr, want: publishErr},
		{name: "publication precedes close failure", publishErr: publishErr, errorOnClose: closeErr, want: publishErr},
		{name: "close failure after durable publish", result: durable, errorOnClose: closeErr, want: closeErr},
		{name: "close failure precedes durability", errorOnClose: closeErr, want: closeErr},
		{name: "not published", result: artifactstore.Result{FilesSynced: true, DirectorySynced: true}, incomplete: true},
		{name: "files not synced", result: artifactstore.Result{Published: true, DirectorySynced: true}, incomplete: true},
		{name: "directory not synced", result: artifactstore.Result{Published: true, FilesSynced: true}, incomplete: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			input := strings.NewReader("fixture artifact")
			receipt := r18dump.Receipt{SHA256: strings.Repeat("a", 64), Bytes: 123, Rows: 7, ActressRows: 3, EmptyActressBlock: true}
			calls := []string{}
			probe := fixturePublicationProbe{
				publish: func(got context.Context, r io.Reader, gotReceipt r18dump.Receipt) (artifactstore.Result, error) {
					calls = append(calls, "publish")
					require.Same(t, ctx, got)
					require.Same(t, input, r)
					require.Equal(t, receipt, gotReceipt)
					return tc.result, tc.publishErr
				},
				close: func() error { calls = append(calls, "close"); return tc.errorOnClose },
			}
			err := publishFixtureArtifact(ctx, probe, input, receipt)
			require.Equal(t, []string{"publish", "close"}, calls)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
			} else if tc.incomplete {
				require.EqualError(t, err, "fixture not durably published")
			} else {
				require.NoError(t, err)
			}
		})
	}
}
