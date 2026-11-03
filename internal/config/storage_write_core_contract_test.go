package config

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// Record attempted mutations, not just files left behind after cleanup.
type writeCoreTraceFS struct {
	afero.Fs
	mutations []string
}

func (fs *writeCoreTraceFS) OpenFile(name string, flags int, mode os.FileMode) (afero.File, error) {
	fs.mutations = append(fs.mutations, "open-file")
	return fs.Fs.OpenFile(name, flags, mode)
}
func (fs *writeCoreTraceFS) MkdirAll(path string, mode os.FileMode) error {
	fs.mutations = append(fs.mutations, "mkdir-all")
	return fs.Fs.MkdirAll(path, mode)
}
func (fs *writeCoreTraceFS) Remove(path string) error {
	fs.mutations = append(fs.mutations, "remove")
	return fs.Fs.Remove(path)
}
func (fs *writeCoreTraceFS) Rename(old, new string) error {
	fs.mutations = append(fs.mutations, "rename")
	return fs.Fs.Rename(old, new)
}

type writeCoreEncoder struct {
	writer              io.Writer
	encodeErr, closeErr error
	calls               []string
	doc                 *yaml.Node
}

func (e *writeCoreEncoder) Encode(value any) error {
	e.calls = append(e.calls, "encode")
	e.doc = value.(*yaml.Node)
	// Even buffered partial output must never reach the filesystem on failure.
	if _, err := io.WriteString(e.writer, "partial YAML"); err != nil {
		return err
	}
	return e.encodeErr
}
func (e *writeCoreEncoder) Close() error {
	e.calls = append(e.calls, "close")
	return e.closeErr
}

// This injects only into the shared production core, not public Save or yaml.v3.
// Public wrapper propagation/unlock composition is independently exercised by
// TestConfigStorageSave_HandleFailurePreservesAndRetries with real native locks.
func TestWriteConfigLocked_EncodingFailurePreservesTarget(t *testing.T) {
	for _, target := range []string{"mapping", "malformed", "missing", "directory"} {
		for _, phase := range []string{"encode", "close", "both"} {
			t.Run(target+"/"+phase, func(t *testing.T) {
				dir := t.TempDir()
				path := filepath.Join(dir, "config.yaml")
				foreign := filepath.Join(dir, "foreign.txt")
				require.NoError(t, os.WriteFile(foreign, []byte("foreign"), 0600))
				foreignBefore := saveProtocolIdentitySnapshot(t, foreign)
				var original []byte
				switch target {
				case "mapping":
					original = []byte("# retained comment\nunknown: retained\nserver:\n    port: 1\n")
				case "malformed":
					original = []byte("server: [")
				case "directory":
					require.NoError(t, os.Mkdir(path, 0700))
					require.NoError(t, os.WriteFile(filepath.Join(path, "sentinel"), []byte("owned sentinel"), 0600))
				}
				if original != nil {
					require.NoError(t, os.WriteFile(path, original, 0600))
				}
				var before, sentinelBefore os.FileInfo
				if target != "missing" {
					before = saveProtocolIdentitySnapshot(t, path)
				}
				if target == "directory" {
					sentinelBefore = saveProtocolIdentitySnapshot(t, filepath.Join(path, "sentinel"))
				}
				cfg := DefaultConfig(nil, nil)
				cfg.Server.Port = 19234
				cfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
					"r18dev": {Enabled: false, Cookies: map[string]string{"session": "preserved"}},
				}
				serverBefore := cfg.Server
				settingsBefore := cfg.Scrapers.Overrides["r18dev"].Clone()
				encodeCause, closeCause := errors.New("encode unavailable"), errors.New("close unavailable")
				encoder := &writeCoreEncoder{}
				if phase != "close" {
					encoder.encodeErr = encodeCause
				}
				if phase != "encode" {
					encoder.closeErr = closeCause
				}
				fs := &writeCoreTraceFS{Fs: afero.NewOsFs()}
				cs := NewConfigStorage(fs, nil)
				unlock, err := cs.acquireLock(path)
				require.NoError(t, err)
				// The test, like Update, owns this lock across the entire core call.
				factoryCalls, readCalls := 0, 0
				func() {
					defer unlock()
					err = cs.writeConfigLocked(cfg, path, func(name string) ([]byte, error) {
						readCalls++
						require.Equal(t, path, name)
						data, readErr := os.ReadFile(name)
						if target == "directory" {
							require.Error(t, readErr)
							require.False(t, os.IsNotExist(readErr))
						}
						return data, readErr
					}, func(w io.Writer) yamlEncoder {
						factoryCalls++
						encoder.writer = w
						return encoder
					})
				}()
				if phase == "close" {
					require.ErrorIs(t, err, closeCause)
					require.EqualError(t, err, "failed to finalize YAML encoding: close unavailable")
				} else {
					require.ErrorIs(t, err, encodeCause)
					require.NotErrorIs(t, err, closeCause)
					require.EqualError(t, err, "failed to encode YAML document: encode unavailable")
				}
				require.Equal(t, 1, readCalls)
				require.Equal(t, 1, factoryCalls)
				require.Equal(t, []string{"encode", "close"}, encoder.calls)
				require.Empty(t, fs.mutations, "encoding failure must precede any staging or publication")
				require.Equal(t, "19234", navigateToMapping(encoder.doc, "server", "port").Value)
				unknown := navigateToMapping(encoder.doc, "unknown")
				if target == "mapping" {
					require.NotNil(t, unknown)
					require.Equal(t, "retained", unknown.Value)
					root := encoder.doc.Content[0]
					require.Equal(t, "# retained comment", root.Content[0].HeadComment)
				} else {
					require.Nil(t, unknown)
				}
				// Snapshot only the supported state exercised here, including a
				// deep-copied settings map; do not assume marshaling is universally pure.
				require.Equal(t, serverBefore, cfg.Server)
				require.Equal(t, settingsBefore, *cfg.Scrapers.Overrides["r18dev"])
				checkFile := func(name string, expected []byte, identity os.FileInfo) {
					t.Helper()
					got, readErr := os.ReadFile(name)
					require.NoError(t, readErr)
					require.Equal(t, expected, got)
					require.True(t, os.SameFile(identity, saveProtocolIdentitySnapshot(t, name)))
				}
				checkFile(foreign, []byte("foreign"), foreignBefore)
				switch target {
				case "missing":
					_, statErr := os.Stat(path)
					require.True(t, os.IsNotExist(statErr))
				case "directory":
					require.True(t, os.SameFile(before, saveProtocolIdentitySnapshot(t, path)))
					checkFile(filepath.Join(path, "sentinel"), []byte("owned sentinel"), sentinelBefore)
				default:
					checkFile(path, original, before)
				}
				entries, err := os.ReadDir(dir)
				require.NoError(t, err)
				expectedEntries := 2
				if target == "missing" {
					expectedEntries = 1
				}
				require.Len(t, entries, expectedEntries, "no stage or released native lock remains")
			})
		}
	}
}

func TestConfigStorageSave_IdenticalBytesPreserveIdentityWithoutStaging(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	fs := &writeCoreTraceFS{Fs: afero.NewOsFs()}
	cs := NewConfigStorage(fs, nil)
	cfg := DefaultConfig(nil, nil)
	require.NoError(t, cs.Save(cfg, path))
	before := saveProtocolIdentitySnapshot(t, path)
	original, err := os.ReadFile(path)
	require.NoError(t, err)
	fs.mutations = nil
	require.NoError(t, cs.Save(cfg, path))
	// Save still owns directory setup even when the shared core is a no-op.
	require.Equal(t, []string{"mkdir-all"}, fs.mutations)
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, original, got)
	require.True(t, os.SameFile(before, saveProtocolIdentitySnapshot(t, path)))
}
