package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/r18devdump"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseOptionsPreservesSourcePriority(t *testing.T) {
	var stderr bytes.Buffer
	opts, err := parseOptions([]string{"--source", "minnanoav", "--sources", "r18dev,example", "--workers", "3"}, &stderr)
	require.NoError(t, err)
	assert.Equal(t, []string{"minnanoav", "r18dev", "example"}, []string(opts.sources))
	assert.Equal(t, 3, opts.workers)
	assert.Equal(t, defaultOutput, opts.output)
	opts, err = parseOptions([]string{"--legacy-csv", "/tmp/jvThumbs.csv"}, &stderr)
	require.NoError(t, err)
	assert.Equal(t, "/tmp/jvThumbs.csv", opts.legacyCSV)
}

func TestParseOptionsRejectsInvalidValues(t *testing.T) {
	var stderr bytes.Buffer
	_, err := parseOptions([]string{"--workers", "0"}, &stderr)
	require.Error(t, err)
	_, err = parseOptions([]string{"--delay", "-1s"}, &stderr)
	require.Error(t, err)
	_, err = parseOptions([]string{"unexpected"}, &stderr)
	require.Error(t, err)
}

func TestRunRequiresExplicitSourceSelection(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run(t.Context(), nil, &stdout, &stderr)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one actress cache source")
}

func TestStringListSetSplitsCommaSeparatedValues(t *testing.T) {
	var values stringList
	require.NoError(t, values.Set("a, b,,c"))
	assert.Equal(t, "a,b,c", values.String())
}

func TestRunListsSourcesWithoutBuilding(t *testing.T) {
	var stdout, stderr bytes.Buffer
	require.NoError(t, run(t.Context(), []string{"--list-sources"}, &stdout, &stderr))
	assert.Contains(t, stdout.String(), "legacy-jvthumbs")
	assert.Contains(t, stdout.String(), "minnanoav")
	assert.Contains(t, stdout.String(), "r18dev", "listing shows supported adapters regardless of dump configuration")
}

func TestRunListsR18DevWithDump(t *testing.T) {
	dir := t.TempDir()
	dumpPath := dir + "/r18dev.db"
	dump := "COPY public.derived_actress (id, name_romaji, image_url, name_kanji, name_kana) FROM stdin;\n1\tA B\ta.jpg\t阿部\t\n\\.\n"
	_, err := r18devdump.Import(context.Background(), strings.NewReader(dump), dumpPath, r18devdump.ImportOptions{})
	require.NoError(t, err)
	var stdout, stderr bytes.Buffer
	require.NoError(t, run(t.Context(), []string{"--list-sources", "--r18dev-dump", dumpPath}, &stdout, &stderr))
	assert.Contains(t, stdout.String(), "r18dev")
}

func TestParameterMapRequiresKeyValue(t *testing.T) {
	var values parameterMap
	require.Error(t, values.Set("missing"))
	require.NoError(t, values.Set("r18dev.dump=/tmp/dump.db"))
	assert.Equal(t, "/tmp/dump.db", values["r18dev.dump"])
}

func TestRunHelpSucceedsWithoutSources(t *testing.T) {
	var stdout, stderr bytes.Buffer
	assert.NoError(t, run(t.Context(), []string{"--help"}, &stdout, &stderr))
	assert.Contains(t, stderr.String(), "Usage of build-actress-cache")
}

func TestRunRejectsDumpOpenFailure(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run(t.Context(), []string{"--r18dev-dump", filepath.Join(t.TempDir(), "missing.db")}, &stdout, &stderr)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "r18.dev dump")
}

func TestRunRejectsExoticDefaultTransport(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	http.DefaultTransport = roundTripperFuncStub{}
	var stdout, stderr bytes.Buffer
	err := run(t.Context(), []string{"--source", "minnanoav", "--limit", "1"}, &stdout, &stderr)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a *http.Transport")
}

type roundTripperFuncStub struct{}

func (roundTripperFuncStub) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("not a transport")
}
