package artifactstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/actresscache/sources/r18dump"
)

func limits() Limits {
	return Limits{MaxDirectoryEntries: 100, MaxStoredBytes: 1 << 20, MaxManifestBytes: 4096, MaxSourceBytes: 10000, MaxSourceRows: 100, Artifact: actresscache.Limits{MaxBytes: 10000, MaxRecordBytes: 2000, MaxRecords: 100, MaxStringBytes: 512, MaxTotalStringBytes: 10000, MaxAliases: 10, MaxExternalIDs: 10, MaxTokensPerRecord: 100}}
}
func evidence(t *testing.T, name string) ([]byte, r18dump.Receipt) {
	t.Helper()
	input := "COPY public.derived_actress (id,name_romaji) FROM stdin;\na\t" + name + "\n\\.\n"
	s, r, err := r18dump.Convert(t.Context(), strings.NewReader(input), actresscache.Provenance{SourceURL: "https://r18.dev/dump", ParserRevision: "copy-v1", ObservedAt: time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)}, r18dump.Limits{MaxInputBytes: 10000, MaxLineBytes: 1024, MaxRows: 100, Output: limits().Artifact})
	if err != nil {
		t.Fatal(err)
	}
	return s.Bytes(), r
}
func openStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := t.TempDir()
	s, err := Open(path, limits())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, path
}
func writeSlot(t *testing.T, path, slot string, data []byte, r r18dump.Receipt) Manifest {
	t.Helper()
	s, err := actresscache.Decode(t.Context(), bytes.NewReader(data), limits().Artifact)
	if err != nil {
		t.Fatal(err)
	}
	m := Manifest{1, "supplied-file-only", s.Digest(), int64(len(data)), r}
	b, _ := json.Marshal(m)
	b = append(b, '\n')
	if err := os.WriteFile(filepath.Join(path, s.Digest()+".ndjson"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, slot), b, 0600); err != nil {
		t.Fatal(err)
	}
	return m
}
func TestLoadRecoveryAndStrictManifest(t *testing.T) {
	s, path := openStore(t)
	if _, err := s.Load(t.Context()); !errors.Is(err, ErrEmpty) {
		t.Fatal(err)
	}
	data, r := evidence(t, "Whole Name")
	m := writeSlot(t, path, "current.json", data, r)
	view, err := s.Load(t.Context())
	if err != nil || view.Recovered || view.Manifest != m {
		t.Fatalf("load %+v %v", view, err)
	}
	writeSlot(t, path, "previous.json", data, r)
	for _, invalid := range []string{"{", "{}\n", `{"version":1,"version":1}` + "\n", strings.Replace(string(mustJSON(t, m)), `"version":1`, `"version":2`, 1), strings.Replace(string(mustJSON(t, m)), `supplied-file-only`, `complete-upstream`, 1), strings.Replace(string(mustJSON(t, m)), m.ArtifactSHA256, "../../outside", 1)} {
		if err := os.WriteFile(filepath.Join(path, "current.json"), []byte(invalid), 0600); err != nil {
			t.Fatal(err)
		}
		v, err := s.Load(t.Context())
		if err != nil || !v.Recovered {
			t.Fatalf("recovery %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(path, m.ArtifactSHA256+".ndjson"), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(t.Context()); err == nil {
		t.Fatal("corrupt artifact accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.Load(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func mustJSON(t *testing.T, m Manifest) []byte {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return append(b, '\n')
}
func TestReceiptAndArtifactValidation(t *testing.T) {
	s, _ := openStore(t)
	data, r := evidence(t, "Whole Name")
	for _, change := range []func(*r18dump.Receipt){func(r *r18dump.Receipt) { r.SHA256 = "BAD" }, func(r *r18dump.Receipt) { r.SHA256 = strings.Repeat("0", 64) }, func(r *r18dump.Receipt) { r.Bytes = 0 }, func(r *r18dump.Receipt) { r.Bytes = 10001 }, func(r *r18dump.Receipt) { r.Rows = -1 }, func(r *r18dump.Receipt) { r.Rows = 101 }, func(r *r18dump.Receipt) { r.ActressRows = 0; r.EmptyActressBlock = true }, func(r *r18dump.Receipt) { r.EmptyActressBlock = true }} {
		copy := r
		change(&copy)
		if _, err := s.validate(t.Context(), data, copy); err == nil {
			t.Fatal("invalid receipt accepted")
		}
	}
	for _, invalid := range [][]byte{[]byte("garbage"), append([]byte(" "), data...)} {
		if _, err := s.validate(t.Context(), invalid, r); err == nil {
			t.Fatal("invalid artifact accepted")
		}
	}
}
func TestOpenAndRegularEntries(t *testing.T) {
	for _, path := range []string{"relative", filepath.Join(t.TempDir(), "missing")} {
		if s, err := Open(path, limits()); err == nil {
			_ = s.Close()
			t.Fatal("invalid root accepted")
		}
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(file, limits()); err == nil {
		t.Fatal("file root")
	}
	for _, change := range []func(*Limits){func(l *Limits) { l.MaxManifestBytes = 0 }, func(l *Limits) { l.MaxManifestBytes = 1<<20 + 1 }, func(l *Limits) { l.MaxSourceRows = 0 }, func(l *Limits) { l.MaxDirectoryEntries = 1_000_001 }, func(l *Limits) { l.Artifact.MaxBytes = 0 }} {
		l := limits()
		change(&l)
		if _, err := Open(t.TempDir(), l); err == nil {
			t.Fatal("invalid limits")
		}
	}
	s, path := openStore(t)
	if err := os.Mkdir(filepath.Join(path, "current.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := s.readRegular(t.Context(), "current.json", 4096); err == nil {
		t.Fatal("directory read")
	}
	if err := os.WriteFile(filepath.Join(path, "oversize"), []byte("ab"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.readRegular(t.Context(), "oversize", 1); err == nil {
		t.Fatal("oversize read")
	}
}

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }
func TestBoundedReader(t *testing.T) {
	sentinel := errors.New("late read")
	if _, err := readBounded(t.Context(), io.MultiReader(strings.NewReader("x"), readerFunc(func([]byte) (int, error) { return 0, sentinel })), 10); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	if _, err := readBounded(t.Context(), strings.NewReader("xx"), 1); err == nil {
		t.Fatal("limit")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := readBounded(ctx, strings.NewReader("x"), 10); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	ctx, cancel = context.WithCancel(t.Context())
	if _, err := readBounded(ctx, readerFunc(func([]byte) (int, error) { cancel(); return 0, io.EOF }), 10); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestStoredReceiptAndDigestFailures(t *testing.T) {
	for _, kind := range []string{"digest", "length", "receipt", "noncanonical"} {
		t.Run(kind, func(t *testing.T) {
			s, path := openStore(t)
			data, r := evidence(t, "Whole")
			m := writeSlot(t, path, "current.json", data, r)
			switch kind {
			case "digest":
				bad := append([]byte(nil), data...)
				bad[len(bad)-2] ^= 1
				if err := os.WriteFile(filepath.Join(path, m.ArtifactSHA256+".ndjson"), bad, 0600); err != nil {
					t.Fatal(err)
				}
			case "length":
				m.ArtifactBytes++
			case "receipt":
				m.Source.ActressRows = 0
				m.Source.EmptyActressBlock = true
			case "noncanonical":
				m.Source.SHA256 = strings.Repeat("0", 64)
			}
			if kind != "digest" {
				if err := os.WriteFile(filepath.Join(path, "current.json"), mustJSON(t, m), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.Load(t.Context()); err == nil {
				t.Fatal("invalid stored binding")
			}
		})
	}
}
