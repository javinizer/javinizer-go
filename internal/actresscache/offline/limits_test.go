package offline

import (
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/actresscache/artifactstore"
	"github.com/javinizer/javinizer-go/internal/actresscache/sources/r18dump"
)

func TestDefaultLimitsMatchAndValidate(t *testing.T) {
	limits := DefaultLimits()
	if limits.Source.Output != limits.Store.Artifact || limits.Source.MaxInputBytes != limits.Store.MaxSourceBytes || limits.Source.MaxRows != limits.Store.MaxSourceRows {
		t.Fatal("converter and store limits diverged")
	}
	store, err := artifactstore.Open(t.TempDir(), limits.Store)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	snapshot, _, err := r18dump.Convert(t.Context(), strings.NewReader("COPY public.derived_actress (id,name_romaji) FROM stdin;\nfixture\tSynthetic Name\n\\.\n"), actresscache.Provenance{
		SourceURL: "https://r18.dev/dump", ParserRevision: "test", ObservedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}, limits.Source)
	if err != nil || snapshot == nil {
		t.Fatalf("finite defaults invalid: %v", err)
	}
	limits.Store.Artifact.MaxBytes = 0
	if DefaultLimits().Store.Artifact.MaxBytes == 0 {
		t.Fatal("mutating returned limits changed future defaults")
	}
}
