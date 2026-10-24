package r18dump

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/r18devdump"
)

const header = "COPY public.derived_actress (id, name_romaji, image_url, name_kanji, name_kana) FROM stdin;\n"
const fixture = header + "001\tJane Doe\tbare.jpg\t日本名\tかな\n\\.\n-- complete local input\n"

func budgets() Limits {
	return Limits{10000, 1024, 100, actresscache.Limits{MaxBytes: 10000, MaxRecordBytes: 2000, MaxRecords: 100, MaxStringBytes: 512, MaxTotalStringBytes: 10000, MaxAliases: 10, MaxExternalIDs: 10, MaxTokensPerRecord: 100}}
}
func provenance() actresscache.Provenance {
	return actresscache.Provenance{SourceURL: "https://r18.dev/dump", ParserRevision: "r18-copy-v1", ObservedAt: time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)}
}
func convert(t *testing.T, input string, l Limits) (*actresscache.Snapshot, Receipt) {
	t.Helper()
	s, r, e := Convert(t.Context(), strings.NewReader(input), provenance(), l)
	if e != nil {
		t.Fatal(e)
	}
	return s, r
}
func TestRoundTrip(t *testing.T) {
	s, r := convert(t, fixture, budgets())
	want := fmt.Sprintf("%x", sha256.Sum256([]byte(fixture)))
	if r.SHA256 != want || r.Bytes != int64(len(fixture)) || r.Rows != 1 || r.ActressRows != 1 || r.EmptyActressBlock {
		t.Fatalf("receipt %+v", r)
	}
	key, _ := actresscache.SourceKey("r18dev", "001")
	e, ok := s.Get(key)
	if !ok || e.SourceID != "001" || e.Metadata.FirstName != "" || e.Metadata.LastName != "" || e.Metadata.ThumbnailURL != "" || len(e.ExternalIDs) != 0 || e.Metadata.JapaneseName != "日本名" || !contains(e.Metadata.Aliases, "Jane Doe") || !contains(e.Metadata.Aliases, "かな") || e.Provenance.DatasetSHA256 != want {
		t.Fatalf("evidence %+v", e)
	}
	d, err := actresscache.Decode(t.Context(), bytes.NewReader(s.Bytes()), budgets().Output)
	if err != nil || !bytes.Equal(d.Bytes(), s.Bytes()) {
		t.Fatalf("roundtrip %v", err)
	}
	changed := strings.Replace(fixture, "Jane Doe", "Changed Whole Name", 1)
	newer, _ := convert(t, changed, budgets())
	if _, ok := newer.Get(key); !ok {
		t.Fatal("mutable metadata changed key")
	}
}
func TestInputShapes(t *testing.T) {
	good := []string{header + "\\.\n", "COPY \"public\".\"derived_actress\" (\"name_romaji\", \"id\", \"name_kana\") FROM stdin;\nWhole Name\tabc\tWhole Name\n\\.\n", header + "a\t\\N\t\\N\t日本\t\\N\n\\.\n", header + "a\t\\\\N\t\\N\t\\N\t\\N\n\\.\n", "COPY public.other (id) FROM stdin;\nx\n\\.\n" + fixture}
	for i, input := range good {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			_, r := convert(t, input, budgets())
			if (i == 0) != r.EmptyActressBlock {
				t.Fatal(r)
			}
		})
	}
	bad := []string{"", "-- no block\n", header, header + "a\tJane\t\\N\t\\N\t\\N\n", strings.TrimSuffix(fixture, "\n"), header + "a\tb\n\\.\n", header + "\\N\tJane\t\\N\t\\N\t\\N\n\\.\n", header + " \tJane\t\\N\t\\N\t\\N\n\\.\n", header + "a\t\\N\t\\N\t\\N\t\\N\n\\.\n", header + "a\t\\q\t\\N\t\\N\t\\N\n\\.\n", header + "a\tend\\\t\\N\t\\N\t\\N\n\\.\n", header + "a\tJane\x00\t\\N\t\\N\t\\N\n\\.\n", fixture + "\xff\n", strings.ReplaceAll(fixture, "\n", "\r\n"), fixture + header + "\\.\n", "COPY public.derived_actress (name_romaji) FROM stdin;\n\\.\n", "COPY public.derived_actress (id,id) FROM stdin;\n\\.\n", "COPY public.derived_actress (id,unknown) FROM stdin;\n\\.\n", "COPY public.derived_actress (id,1bad) FROM stdin;\n\\.\n", "COPY public.derived_actress FROM stdin;\n\\.\n", strings.Replace(fixture, "001\t", "001\t", 1) + "COPY invalid\n", header + "a\tJane\t\\N\t\\N\t\\N\na\tJane\t\\N\t\\N\t\\N\n\\.\n"}
	for i, input := range bad {
		t.Run(fmt.Sprintf("bad%d", i), func(t *testing.T) {
			s, r, e := Convert(t.Context(), strings.NewReader(input), provenance(), budgets())
			if e == nil || s != nil || r != (Receipt{}) {
				t.Fatalf("accepted %q: %+v %v", input, r, e)
			}
		})
	}
}
func TestLimits(t *testing.T) {
	l := budgets()
	l.MaxInputBytes = int64(len(fixture))
	l.MaxLineBytes = len(strings.TrimSuffix(header, "\n"))
	l.MaxRows = 1
	l.Output.MaxRecords = 1
	convert(t, fixture, l)
	cases := []func(*Limits){func(l *Limits) { l.MaxInputBytes-- }, func(l *Limits) { l.MaxLineBytes-- }, func(l *Limits) { l.MaxRows = 0 }, func(l *Limits) { l.MaxInputBytes = 0 }, func(l *Limits) { l.MaxInputBytes = 1<<63 - 1 }, func(l *Limits) { l.MaxLineBytes = 0 }, func(l *Limits) { l.MaxLineBytes = 1<<20 + 1 }, func(l *Limits) { l.Output.MaxBytes = 0 }, func(l *Limits) { l.Output.MaxRecords = 0 }, func(l *Limits) { l.MaxLineBytes = 16 }}
	for i, change := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			c := l
			change(&c)
			if _, _, e := Convert(t.Context(), strings.NewReader(fixture), provenance(), c); e == nil {
				t.Fatal("accepted limit")
			}
		})
	}
	double := strings.Replace(fixture, "\\.\n", "002\tOther\t\\N\t\\N\t\\N\n\\.\n", 1)
	for _, which := range []string{"rows", "records", "aggregate"} {
		t.Run(which, func(t *testing.T) {
			l := budgets()
			switch which {
			case "rows":
				l.MaxRows = 1
			case "records":
				l.Output.MaxRecords = 1
			case "aggregate":
				l.Output.MaxTotalStringBytes = 300
			}
			if _, _, e := Convert(t.Context(), strings.NewReader(double), provenance(), l); e == nil {
				t.Fatal("accepted overflow")
			}
		})
	}
}

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }
func TestErrorsAndCancellation(t *testing.T) {
	sentinel := errors.New("late input failure")
	input := io.MultiReader(strings.NewReader(fixture), readerFunc(func([]byte) (int, error) { return 0, sentinel }))
	if s, r, e := Convert(t.Context(), input, provenance(), budgets()); !errors.Is(e, sentinel) || s != nil || r != (Receipt{}) {
		t.Fatalf("late error: %v", e)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, e := Convert(ctx, strings.NewReader(fixture), provenance(), budgets()); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	ctx, cancel = context.WithCancel(t.Context())
	input = readerFunc(func(p []byte) (int, error) { cancel(); return copy(p, fixture), nil })
	if _, _, e := Convert(ctx, input, provenance(), budgets()); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if _, _, e := Convert(t.Context(), nil, provenance(), budgets()); e == nil {
		t.Fatal("nil reader")
	}
	p := provenance()
	p.DatasetSHA256 = strings.Repeat("a", 64)
	if _, _, e := Convert(t.Context(), strings.NewReader(fixture), p, budgets()); e == nil {
		t.Fatal("caller digest")
	}
	p = provenance()
	p.SourceURL = "file:///tmp/dump"
	if _, _, e := Convert(t.Context(), strings.NewReader(fixture), p, budgets()); e == nil {
		t.Fatal("bad provenance")
	}
}

func TestCancellationAtPhases(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	c := collector{ctx: ctx}
	if e := c.add(r18devdump.DumpRow{}); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	ctx, cancel = context.WithCancel(t.Context())
	input := io.MultiReader(strings.NewReader(fixture), readerFunc(func([]byte) (int, error) { cancel(); return 0, io.EOF }))
	if s, r, e := Convert(ctx, input, provenance(), budgets()); !errors.Is(e, context.Canceled) || s != nil || r != (Receipt{}) {
		t.Fatalf("EOF cancel: %v", e)
	}
	gate := lineGate{ctx: t.Context()}
	if n, e := gate.Read(nil); n != 0 || e != nil {
		t.Fatalf("empty read: %d %v", n, e)
	}
}

func TestEmptyBudgetAndCumulativeAdmission(t *testing.T) {
	l := budgets()
	empty, err := actresscache.Build(t.Context(), nil, l.Output)
	if err != nil {
		t.Fatal(err)
	}
	l.Output.MaxBytes = int64(len(empty.Bytes()))
	l.Output.MaxRecordBytes = len(empty.Bytes()) - 1
	convert(t, header+"\\.\n", l)
	// A failed admission must not retain the rejected row or consume later input.
	p := provenance()
	p.DatasetSHA256 = strings.Repeat("0", 64)
	one := actresscache.Evidence{Source: "r18dev", SourceID: "a", Provenance: p, Metadata: actresscache.Metadata{Aliases: []string{"Jane"}}}
	single, err := actresscache.Build(t.Context(), []actresscache.Evidence{one}, budgets().Output)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"bytes", "strings", "duplicate"} {
		t.Run(kind, func(t *testing.T) {
			l := budgets().Output
			if kind == "bytes" {
				l.MaxBytes = int64(len(single.Bytes()))
			}
			if kind == "strings" {
				l.MaxTotalStringBytes = 250
			}
			c := collector{ctx: t.Context(), provenance: p, limits: l, usedBytes: int64(len(empty.Bytes())), seen: map[string]bool{}}
			row := r18devdump.DumpRow{Table: "derived_actress", Columns: []string{"id", "name_romaji"}, Values: []string{"a", "Jane"}}
			if err := c.add(row); err != nil {
				t.Fatal(err)
			}
			if kind != "duplicate" {
				row.Values[0] = "b"
			}
			if err := c.add(row); err == nil || len(c.records) != 1 {
				t.Fatalf("admission: %v count %d", err, len(c.records))
			}
		})
	}
	// Decimal record-count growth is charged along with record lines.
	input := "COPY public.derived_actress (id,name_romaji) FROM stdin;\n"
	for i := 0; i < 11; i++ {
		input += fmt.Sprintf("%d\tWhole\n", i)
	}
	input += "\\.\n"
	s, _ := convert(t, input, budgets())
	l = budgets()
	l.Output.MaxBytes = int64(len(s.Bytes()))
	convert(t, input, l)
	l.Output.MaxBytes--
	if _, _, err := Convert(t.Context(), strings.NewReader(input), provenance(), l); err == nil {
		t.Fatal("count-header growth undercharged")
	}
}
func TestProvenanceValidation(t *testing.T) {
	for _, change := range []func(*actresscache.Provenance){func(p *actresscache.Provenance) { p.ParserRevision = "" }, func(p *actresscache.Provenance) { p.ParserRevision = "\xff" }, func(p *actresscache.Provenance) { p.ParserRevision = strings.Repeat("a", 513) }, func(p *actresscache.Provenance) { p.ObservedAt = time.Time{} }, func(p *actresscache.Provenance) { p.ObservedAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) }, func(p *actresscache.Provenance) { p.SourceURL = "https://%xx" }, func(p *actresscache.Provenance) { p.SourceURL = "https://user:secret@r18.dev/" }, func(p *actresscache.Provenance) { p.SourceURL = "https://r18.dev/#fragment" }} {
		p := provenance()
		change(&p)
		if _, _, err := Convert(t.Context(), strings.NewReader(header+"\\.\n"), p, budgets()); err == nil {
			t.Fatalf("accepted %+v", p)
		}
	}
}

func TestFinalValidationAndQuotaAgreement(t *testing.T) {
	s, r := convert(t, fixture, budgets())
	l := budgets().Output
	l.MaxBytes = 1
	if result, receipt, err := buildResult(t.Context(), s.Records(), provenance(), r, l); err == nil || result != nil || receipt != (Receipt{}) {
		t.Fatalf("final validation: %v", err)
	}
	p := provenance()
	p.ObservedAt = p.ObservedAt.In(time.FixedZone("offset", 3600))
	for _, metric := range []string{"bytes", "strings"} {
		t.Run(metric, func(t *testing.T) {
			// Find the real format's exact threshold, then require adapter parity.
			low, high := int64(1), int64(10000)
			for low < high {
				mid := (low + high) / 2
				out := budgets().Output
				if metric == "bytes" {
					out.MaxBytes = mid
				} else {
					out.MaxTotalStringBytes = mid
				}
				_, err := actresscache.Build(t.Context(), s.Records(), out)
				if err == nil {
					high = mid
				} else {
					low = mid + 1
				}
			}
			for _, delta := range []int64{-1, 0, 1} {
				l := budgets()
				if metric == "bytes" {
					l.Output.MaxBytes = low + delta
				} else {
					l.Output.MaxTotalStringBytes = low + delta
				}
				_, _, err := Convert(t.Context(), strings.NewReader(fixture), p, l)
				if (err == nil) != (delta >= 0) {
					t.Fatalf("%s threshold %d delta %d: %v", metric, low, delta, err)
				}
			}
		})
	}
}

func TestRetainedStringsDoNotOwnIgnoredRow(t *testing.T) {
	raw := "actor-id\tWhole Name\t" + strings.Repeat("x", 1<<19)
	fields := strings.Split(raw, "\t")
	p := provenance()
	p.DatasetSHA256 = strings.Repeat("0", 64)
	empty, err := actresscache.Build(t.Context(), nil, budgets().Output)
	if err != nil {
		t.Fatal(err)
	}
	c := collector{ctx: t.Context(), provenance: p, limits: budgets().Output, usedBytes: int64(len(empty.Bytes())), seen: map[string]bool{}}
	if err := c.add(r18devdump.DumpRow{Table: "derived_actress", Columns: []string{"id", "name_romaji", "image_url"}, Values: fields}); err != nil {
		t.Fatal(err)
	}
	kept := c.records[0]
	if kept.SourceID != fields[0] || kept.Metadata.Aliases[0] != fields[1] {
		t.Fatal("changed claims")
	}
	if unsafe.StringData(kept.SourceID) == unsafe.StringData(fields[0]) || unsafe.StringData(kept.Metadata.Aliases[0]) == unsafe.StringData(fields[1]) {
		t.Fatal("retained parser row backing allocation")
	}
}

func TestFullInputAndLargeLineBoundaries(t *testing.T) {
	// Reader errors attached to the final data read must survive buffered parsing.
	sentinel := errors.New("last read error")
	once := false
	input := readerFunc(func(p []byte) (int, error) {
		if once {
			return 0, io.EOF
		}
		once = true
		return copy(p, fixture), sentinel
	})
	if s, r, err := Convert(t.Context(), input, provenance(), budgets()); !errors.Is(err, sentinel) || s != nil || r != (Receipt{}) {
		t.Fatalf("same-read late error: %v", err)
	}
	_, a := convert(t, fixture, budgets())
	_, b := convert(t, fixture+"-- trailing source text\n", budgets())
	if a.SHA256 == b.SHA256 {
		t.Fatal("trailing bytes not hashed")
	}
	l := budgets()
	l.MaxLineBytes = 1 << 20
	l.MaxInputBytes = 2 << 20
	large := strings.Repeat("-", l.MaxLineBytes) + "\n" + fixture
	convert(t, large, l)
	if _, _, err := Convert(t.Context(), strings.NewReader("-"+large), provenance(), l); err == nil {
		t.Fatal("accepted line limit+1")
	}
	mixed := "COPY \"public\".derived_actress (\"id\",name_romaji) FROM stdin;\nKey\tWhole Name\n\\.\n"
	convert(t, mixed, budgets())
	other := "COPY public.other (id) FROM stdin;\nx\n\\.\n" + fixture
	l = budgets()
	l.MaxRows = 2
	convert(t, other, l)
	l.MaxRows = 1
	if _, _, err := Convert(t.Context(), strings.NewReader(other), provenance(), l); err == nil {
		t.Fatal("unrelated COPY rows not charged")
	}
}
