package r18dump

import (
	"bytes"
	"testing"

	"github.com/javinizer/javinizer-go/internal/actresscache"
)

func FuzzConvert(f *testing.F) {
	for _, seed := range []string{fixture, header + "\\.\n", header + "a\t\\N\t\\N\t\\N\t\\N\n\\.\n", "COPY malformed\n"} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		l := budgets()
		l.MaxInputBytes = 4096
		l.MaxLineBytes = 1024
		l.MaxRows = 32
		l.Output.MaxRecords = 32
		s, r, err := Convert(t.Context(), bytes.NewReader(data), provenance(), l)
		if err != nil {
			if s != nil || r != (Receipt{}) {
				t.Fatal("partial result")
			}
			return
		}
		decoded, err := actresscache.Decode(t.Context(), bytes.NewReader(s.Bytes()), l.Output)
		if err != nil || !bytes.Equal(decoded.Bytes(), s.Bytes()) {
			t.Fatalf("roundtrip: %v", err)
		}
	})
}
