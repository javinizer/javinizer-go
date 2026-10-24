package r18devdump

import (
	"strings"
	"testing"
)

func TestDumpRowIsNull(t *testing.T) {
	err := ParseDump(strings.NewReader("COPY public.test (a,b) FROM stdin;\n\\N\t\\\\N\n\\.\n"), func(row DumpRow) error {
		if !row.IsNull(0) || row.IsNull(1) || row.IsNull(-1) || row.IsNull(2) || row.Values[1] != `\N` {
			t.Fatalf("NULL distinction: %+v", row)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
