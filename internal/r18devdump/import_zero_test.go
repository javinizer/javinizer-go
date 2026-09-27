package r18devdump

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImport_RejectsNoRecognizedRows(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"empty input", ""},
		{"garbage text", "<html><body>503 Service Unavailable</body></html>\n"},
		{"copy header zero rows", "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n\\.\n"},
		{"unknown table with rows", "COPY public.not_a_dump_table (a) FROM stdin;\n1\n2\n\\.\n"},
		// Codex round on #273: rows only for a non-video table are desynced
		// garbage, not a dump — reject them like other tableless inputs.
		{"non-video table rows only", "COPY public.derived_actress (id, name_romaji) FROM stdin;\n1\tJane\n\\.\n"},
		// Rows that INSERT OR IGNORE silently discards (NULL content_id breaks
		// the NOT NULL primary key) must also fail the import invariant — the
		// gate counts inserted rows, not emitted ones.
		{"null content_id row ignored", "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n\\N\tIPX-535\n\\.\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "out.db")
			_, err := Import(context.Background(), strings.NewReader(tc.in), path, ImportOptions{})
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrDumpNoRows), "want ErrDumpNoRows, got %v", err)
			_, statErr := os.Stat(path)
			assert.True(t, os.IsNotExist(statErr), "no sidecar file may be installed on invariant failure")
		})
	}
}

func TestImport_NonCanonicalContentID(t *testing.T) {
	// Codex: lookups lowercase-bind before comparing against BINARY primary
	// keys, so uppercase-stored ids become unreachable; emit must reject them.
	path := filepath.Join(t.TempDir(), "out.db")
	_, err := Import(context.Background(), strings.NewReader("COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118IPX00535\tIPX-535\n\\.\n"), path, ImportOptions{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrDumpNonCanonicalID), "want ErrDumpNonCanonicalID, got %v", err)
	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr))
}

func TestImport_RejectsEmptyOrPaddedContentID(t *testing.T) {
	cases := []struct{ name, cid string }{
		{"empty", ""},
		{"space padded", " 118ipx00535 "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "out.db")
			dump := "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n" + tc.cid + "\tIPX-535\n\\.\n"
			_, err := Import(context.Background(), strings.NewReader(dump), path, ImportOptions{})
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrDumpNonCanonicalID), "want ErrDumpNonCanonicalID, got %v", err)
		})
	}
}

func TestImport_ErrorsOnEOFFInsideCopyBlock(t *testing.T) {
	// A gzip-valid but SQL-truncated dump: rows stream fine but the COPY block
	// never terminates — without this check the partial database would install
	// as if complete.
	path := filepath.Join(t.TempDir(), "out.db")
	_, err := Import(context.Background(), strings.NewReader("COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118ipx00535\tIPX-535\n"), path, ImportOptions{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTruncatedDump), "want ErrTruncatedDump, got %v", err)
	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr))

	// Sanctioned termination path still imports fine.
	path2 := filepath.Join(t.TempDir(), "ok.db")
	_, err = Import(context.Background(), strings.NewReader("COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118ipx00535\tIPX-535\n\\.\n"), path2, ImportOptions{})
	require.NoError(t, err)
}

func TestImport_TypedValues(t *testing.T) {
	cases := []struct {
		name    string
		runtime string // COPY-level text value for runtime_mins (\\N = NULL)
		wantErr error
	}{
		{"integer ok", "120", nil},
		{"null ok", "\\N", nil},
		{"text rejected", "unknown", ErrDumpTypedValue},
		{"empty rejected", "", ErrDumpTypedValue},
		{"float rejected", "120.5", ErrDumpTypedValue},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "out.db")
			dump := "COPY public.derived_video (content_id, runtime_mins) FROM stdin;\n118ipx00535\t" + tc.runtime + "\n\\.\n"
			_, err := Import(context.Background(), strings.NewReader(dump), path, ImportOptions{})
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrDumpTypedValue), "want ErrDumpTypedValue, got %v", err)
			_, statErr := os.Stat(path)
			assert.True(t, os.IsNotExist(statErr), "no sidecar may be installed on a typed-value failure")
		})
	}
}

func TestImport_TrailerCompletenessInvariant(t *testing.T) {
	mkDump := func(videos int, trailer bool) string {
		var b strings.Builder
		b.WriteString("COPY public.derived_video (content_id, dvd_id) FROM stdin;\n")
		for i := 0; i < videos; i++ {
			fmt.Fprintf(&b, "content%07d\tDVD-%07d\n", i, i)
		}
		b.WriteString("\\.\n")
		if trailer {
			b.WriteString("COPY public.source_dmm_trailer (content_id, url) FROM stdin;\ncontent0000000\thttps://example/t.mp4\n\\.\n")
		}
		return b.String()
	}

	t.Run("production-scale without trailers ⇒ truncated", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "out.db")
		_, err := Import(context.Background(), strings.NewReader(mkDump(1500, false)), path, ImportOptions{})
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrTruncatedDump))
	})

	t.Run("production-scale with trailer ⇒ ok", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "out.db")
		_, err := Import(context.Background(), strings.NewReader(mkDump(1500, true)), path, ImportOptions{})
		require.NoError(t, err)
	})

	t.Run("synthetic small dumps exempt", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "out.db")
		_, err := Import(context.Background(), strings.NewReader(mkDump(1, false)), path, ImportOptions{})
		require.NoError(t, err)
	})
}

func TestReplaceFile_OverwriteExistingDestination(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "new.db")
	dst := filepath.Join(dir, "old.db")
	require.NoError(t, os.WriteFile(src, []byte("new"), 0o600))
	require.NoError(t, os.WriteFile(dst, []byte("old"), 0o600))
	require.NoError(t, ReplaceFile(src, dst), "replace onto an existing destination (Windows-only failure before the fix)")
	data, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, "new", string(data))
}

func TestReplaceFile_BothRenamesFail(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("fallback-only error path; POSIX rename fails once directly")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "new.db")
	dst := filepath.Join(dir, "blocked.db")
	require.NoError(t, os.WriteFile(src, []byte("new"), 0o600))
	// Non-empty directory: direct rename fails AND Remove refuses it.
	require.NoError(t, os.MkdirAll(dst, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dst, "blocker"), []byte("x"), 0o600))
	err := ReplaceFile(src, dst)
	require.Error(t, err)
}

func TestReplaceFile_OverwriteExistingDestination_Windows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("POSIX stays on plain os.Rename; this side effect is Windows-only")
	}
	// dst exists and gets replaced via MoveFileEx(REPLACE_EXISTING) — the
	// customer case this helper was added for.
	dir := t.TempDir()
	src := filepath.Join(dir, "new.db")
	dst := filepath.Join(dir, "old.db")
	require.NoError(t, os.WriteFile(src, []byte("new"), 0o600))
	require.NoError(t, os.WriteFile(dst, []byte("old"), 0o600))
	require.NoError(t, ReplaceFile(src, dst))
	data, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, "new", string(data))
}

func TestEnforceDumpSizeLimit(t *testing.T) {
	// Snapshot the ceiling's constant — this is the receive-time policy value.
	require.Positive(t, MaxDecompressedDumpBytes)

	t.Run("content within the limit reads cleanly", func(t *testing.T) {
		r := EnforceDumpSizeLimit(strings.NewReader("copy dump bytes"), 16)
		buf := make([]byte, 10)
		var got []byte
		for {
			n, err := r.Read(buf)
			got = append(got, buf[:n]...)
			if err != nil {
				require.ErrorIs(t, err, io.EOF)
				break
			}
		}
		assert.Contains(t, string(got), "copy dum", "reads up to the cap should all succeed")
	})

	t.Run("exactly at the limit reads to clean EOF", func(t *testing.T) {
		r := EnforceDumpSizeLimit(strings.NewReader("0123456789abcdef"), 16)
		buf := make([]byte, 16)
		var got []byte
		var finalErr error
		for finalErr == nil {
			n, err := r.Read(buf)
			got = append(got, buf[:n]...)
			finalErr = err
		}
		require.ErrorIs(t, finalErr, io.EOF)
		assert.Equal(t, "0123456789abcdef", string(got))
	})

	t.Run("inner reader returning zero-nil normalizes to EOF", func(t *testing.T) {
		// Codex: after the cap endpoint probe fires, a misbehaving inner reader
		// returning (0, nil) must not mask the clean-EOF tell to the caller.
		r := EnforceDumpSizeLimit(zeroNilReader{}, 0)
		_, err := r.Read(make([]byte, 4))
		require.ErrorIs(t, err, io.EOF)
	})

	t.Run("crossing the limit fails with the sentinel and keeps partial data", func(t *testing.T) {
		r := EnforceDumpSizeLimit(strings.NewReader("0123456789abcdef"), 8)
		buf := make([]byte, 16)
		var got []byte
		var finalErr error
		for finalErr == nil {
			n, err := r.Read(buf)
			got = append(got, buf[:n]...)
			finalErr = err
		}
		require.ErrorIs(t, finalErr, ErrDumpTooLarge)
		assert.Equal(t, "01234567", string(got))
	})

	t.Run("import stops with a classified too-large error against the tiny cap", func(t *testing.T) {
		// exercises the Import integration (fsTagReader → ErrDumpTooLarge →
		// validation classification) without generating GBs of data.
		var body strings.Builder
		body.WriteString("COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118ipx00535\tIPX-535\n\\.\n")
		_, err := Import(context.Background(), EnforceDumpSizeLimit(strings.NewReader(body.String()), 8), filepath.Join(t.TempDir(), "x.db"), ImportOptions{})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrDumpTooLarge)
	})
}

type zeroNilReader struct{}

func (zeroNilReader) Read([]byte) (int, error) { return 0, nil }
