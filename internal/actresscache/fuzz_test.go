package actresscache_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/javinizer/javinizer-go/internal/actresscache"
)

func FuzzDecodeSnapshot(f *testing.F) {
	fixture, err := os.ReadFile("testdata/evidence-v1.ndjson")
	if err != nil {
		f.Fatal(err)
	}
	f.Add(fixture)
	f.Add([]byte("invalid\n"))
	f.Add(bytes.Replace(fixture, []byte(`"actor-1"`), []byte(`"\ud83d\ude00"`), 1))
	f.Fuzz(func(t *testing.T, data []byte) {
		l := limits()
		l.MaxBytes = 16 << 10
		l.MaxRecordBytes = 4 << 10
		l.MaxRecords = 16
		l.MaxStringBytes = 512
		l.MaxTotalStringBytes = 8 << 10
		l.MaxAliases = 8
		l.MaxExternalIDs = 4
		l.MaxTokensPerRecord = 256
		snapshot, err := actresscache.Decode(t.Context(), bytes.NewReader(data), l)
		if err != nil {
			return
		}
		encoded := snapshot.Bytes()
		if int64(len(encoded)) > l.MaxBytes {
			t.Fatal("accepted artifact exceeded byte budget")
		}
		roundTrip, err := actresscache.Decode(t.Context(), bytes.NewReader(encoded), l)
		if err != nil {
			t.Fatalf("accepted artifact cannot round trip: %v", err)
		}
		if !bytes.Equal(roundTrip.Bytes(), encoded) || roundTrip.Digest() != snapshot.Digest() {
			t.Fatal("accepted artifact has unstable deterministic encoding")
		}
	})
}
