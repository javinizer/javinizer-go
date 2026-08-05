package actresscache

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
)

// RuntimeSchemaVersion ...
const RuntimeSchemaVersion = 1

// RuntimeRecord ...
type RuntimeRecord struct {
	BuiltinKey   string   `json:"builtin_key"`
	DMMID        int      `json:"dmm_id,omitempty"`
	FirstName    string   `json:"first_name,omitempty"`
	LastName     string   `json:"last_name,omitempty"`
	JapaneseName string   `json:"japanese_name,omitempty"`
	Aliases      []string `json:"aliases,omitempty"`
	ThumbURL     string   `json:"thumb_url,omitempty"`
}

// RuntimeCache ...
type RuntimeCache struct {
	SchemaVersion int             `json:"schema_version"`
	Records       []RuntimeRecord `json:"records"`
}

// NewRuntimeCache ...
func NewRuntimeCache(cache Cache) RuntimeCache {
	candidates := make([]RuntimeRecord, 0, len(cache.Records))
	for _, record := range cache.Records {
		if !runtimeRecordHasIdentity(record) {
			continue
		}
		candidates = append(candidates, RuntimeRecord{
			BuiltinKey:   record.BuiltinKey,
			DMMID:        record.DMMID,
			FirstName:    record.FirstName,
			LastName:     record.LastName,
			JapaneseName: record.JapaneseName,
			Aliases:      append([]string(nil), record.Aliases...),
			ThumbURL:     runtimeThumbnailURL(record.ThumbURL),
		})
	}
	dmmCounts := make(map[int]int)
	jpOwners := make(map[string]map[int]struct{})
	nameOwners := make(map[string]map[int]struct{})
	for index, record := range candidates {
		if record.DMMID > 0 {
			dmmCounts[record.DMMID]++
		}
		addRuntimeIdentityOwner(jpOwners, normalizeIdentity(record.JapaneseName), index)
		for _, alias := range record.Aliases {
			addRuntimeIdentityOwner(jpOwners, normalizeIdentity(alias), index)
		}
		addRuntimeIdentityOwner(nameOwners, normalizeIdentity(record.FirstName+" "+record.LastName), index)
	}
	records := make([]RuntimeRecord, 0, len(candidates))
	for index, record := range candidates {
		if runtimeRecordReachable(record, index, dmmCounts, jpOwners, nameOwners) {
			records = append(records, record)
		}
	}
	return RuntimeCache{SchemaVersion: RuntimeSchemaVersion, Records: records}
}

// addRuntimeIdentityOwner ...
func addRuntimeIdentityOwner(owners map[string]map[int]struct{}, identity string, index int) {
	if identity == "" {
		return
	}
	if owners[identity] == nil {
		owners[identity] = make(map[int]struct{})
	}
	owners[identity][index] = struct{}{}
}

// runtimeRecordReachable ...
func runtimeRecordReachable(record RuntimeRecord, index int, dmmCounts map[int]int, jpOwners, nameOwners map[string]map[int]struct{}) bool {
	if record.DMMID > 0 && dmmCounts[record.DMMID] == 1 {
		return true
	}
	identities := append([]string{record.JapaneseName}, record.Aliases...)
	for _, identity := range identities {
		owners := jpOwners[normalizeIdentity(identity)]
		if len(owners) == 1 {
			if _, ok := owners[index]; ok {
				return true
			}
		}
	}
	owners := nameOwners[normalizeIdentity(record.FirstName+" "+record.LastName)]
	if len(owners) == 1 {
		_, ok := owners[index]
		return ok
	}
	return false
}

var createRuntimeTemp = func(dir, pattern string) (cacheTempFile, error) {
	return os.CreateTemp(dir, pattern)
}

var newRuntimeGzipWriter = gzip.NewWriterLevel

// WriteRuntimeFile ...
func WriteRuntimeFile(path string, cache Cache) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("actress runtime cache output path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	tmp, err := createRuntimeTemp(filepath.Dir(path), ".actress-runtime-cache-*.json.gz")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	writer, err := newRuntimeGzipWriter(tmp, gzip.BestCompression)
	if err != nil {
		_ = tmp.Close()
		return err
	}
	writer.ModTime = time.Unix(0, 0)
	writer.Name = ""
	writer.Comment = ""
	writer.OS = 255
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(NewRuntimeCache(cache)); err != nil {
		_ = writer.Close()
		_ = tmp.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	// fsync before rename: a crash between them must not leave a zeroed cache.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return atomicReplace(tmpPath, path)
}

// runtimeRecordHasIdentity ...
func runtimeRecordHasIdentity(record Record) bool {
	// Mirror Lookup's reachability exactly: a DMM ID, a Japanese name (alias
	// or canonical), or BOTH romanized name parts — Lookup refuses a single
	// romanized part (it collides with mononymous/stage-name records), so
	// single-part records would be embedded but unfindable.
	romanized := strings.TrimSpace(record.FirstName) != "" && strings.TrimSpace(record.LastName) != ""
	return record.DMMID > 0 || strings.TrimSpace(record.JapaneseName) != "" || romanized || len(record.Aliases) > 0
}

// atomicReplace is os.Rename on Unix; on Windows os.Rename fails when dst
// already exists, so it falls back to remove-then-rename. The fallback is not
// atomic, but the short window opens only when replacing a committed artifact.
var (
	atomicRename = os.Rename
	atomicRemove = os.Remove
)

func atomicReplace(src, dst string) error {
	if err := atomicRename(src, dst); err == nil {
		return nil
	}
	if rmErr := atomicRemove(dst); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
		return rmErr
	}
	return atomicRename(src, dst)
}

// runtimeThumbnailURL ...
func runtimeThumbnailURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || models.IsKnownInvalidDMMActressThumbnail(raw) {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || isKnownSourcePlaceholder(u) {
		return ""
	}
	return raw
}

var newRuntimeGzipReader = func(reader io.Reader) (io.ReadCloser, error) {
	return gzip.NewReader(reader)
}

// maxRuntimeCacheDecodedBytes bounds decompression of the built-in cache
// (currently ~25k records ≈ a few MB; 64MB is generous headroom).
const maxRuntimeCacheDecodedBytes = 64 << 20

// decodeRuntimeCache ...
func decodeRuntimeCache(reader io.Reader) (RuntimeCache, error) {
	compressed, err := newRuntimeGzipReader(reader)
	if err != nil {
		return RuntimeCache{}, fmt.Errorf("open built-in actress cache: %w", err)
	}
	// Cap decompressed payload size: the embedded artifact is trusted but a
	// corrupt or hostile build artifact must not OOM the process.
	decoder := json.NewDecoder(io.LimitReader(compressed, maxRuntimeCacheDecodedBytes+1))
	// cache ...
	var cache RuntimeCache
	if err := decoder.Decode(&cache); err != nil {
		_ = compressed.Close()
		return RuntimeCache{}, fmt.Errorf("parse built-in actress cache: %w", err)
	}
	// extra ...
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		_ = compressed.Close()
		if err == nil {
			return RuntimeCache{}, fmt.Errorf("parse built-in actress cache: trailing JSON data")
		}
		return RuntimeCache{}, fmt.Errorf("parse built-in actress cache: %w", err)
	}
	if err := compressed.Close(); err != nil {
		return RuntimeCache{}, fmt.Errorf("read built-in actress cache trailer: %w", err)
	}
	if cache.SchemaVersion != RuntimeSchemaVersion {
		return RuntimeCache{}, fmt.Errorf("unsupported built-in actress cache schema %d", cache.SchemaVersion)
	}
	return cache, nil
}
