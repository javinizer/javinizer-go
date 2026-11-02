package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// Only an exclusively created stage owned by this Save receives the failing
// handle. Reads, the committed destination and the native lock remain real.
type saveProtocolFs struct {
	afero.Fs
	target, phase   string
	failure         error
	trace           []string
	stages, removed []string
	renames         [][2]string
}

func (fs *saveProtocolFs) OpenFile(name string, flags int, mode os.FileMode) (afero.File, error) {
	f, err := fs.Fs.OpenFile(name, flags, mode)
	if err != nil {
		return nil, err
	}
	if filepath.Dir(name) == filepath.Dir(fs.target) && strings.HasPrefix(filepath.Base(name), "."+filepath.Base(fs.target)+".tmp-") && flags&(os.O_CREATE|os.O_EXCL) == os.O_CREATE|os.O_EXCL {
		fs.stages = append(fs.stages, name)
		return &saveProtocolFile{File: f, owner: fs}, nil
	}
	return f, nil
}
func (fs *saveProtocolFs) Remove(name string) error {
	fs.removed = append(fs.removed, name)
	return fs.Fs.Remove(name)
}
func (fs *saveProtocolFs) Rename(old, new string) error {
	fs.renames = append(fs.renames, [2]string{old, new})
	return fs.Fs.Rename(old, new)
}

type saveProtocolFile struct {
	afero.File
	owner *saveProtocolFs
}

func (f *saveProtocolFile) Write(p []byte) (int, error) {
	f.owner.trace = append(f.owner.trace, "write")
	if f.owner.phase == "write" {
		// A partial write with a nonnil error is a valid Writer failure; leave real
		// uncommitted bytes so cleanup is not merely removal of an empty placeholder.
		n, err := f.File.Write(p[:len(p)/2])
		if err != nil {
			return n, err
		}
		return n, f.owner.failure
	}
	return f.File.Write(p)
}
func (f *saveProtocolFile) Sync() error {
	f.owner.trace = append(f.owner.trace, "sync")
	if f.owner.phase == "sync" {
		return f.owner.failure
	}
	return f.File.Sync()
}
func (f *saveProtocolFile) Close() error {
	f.owner.trace = append(f.owner.trace, "close")
	err := f.File.Close()
	if err != nil {
		return err
	}
	if f.owner.phase == "close" {
		return f.owner.failure
	}
	return nil
}

func TestConfigStorageSave_HandleFailurePreservesAndRetries(t *testing.T) {
	for _, phase := range []string{"write", "sync", "close"} {
		t.Run(phase, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			foreign := filepath.Join(dir, "foreign.txt")
			healthy := NewConfigStorage(afero.NewOsFs(), nil)
			cfg := DefaultConfig(nil, nil)
			require.NoError(t, healthy.Save(cfg, path))
			require.NoError(t, os.WriteFile(foreign, []byte("foreign bytes"), 0600))
			original, err := os.ReadFile(path)
			require.NoError(t, err)
			before := saveProtocolIdentitySnapshot(t, path)
			foreignBefore := saveProtocolIdentitySnapshot(t, foreign)
			cfg.Server.Port++
			cause := errors.New("storage handle unavailable")
			fs := &saveProtocolFs{Fs: afero.NewOsFs(), target: path, phase: phase, failure: cause}
			err = NewConfigStorage(fs, nil).Save(cfg, path)
			require.ErrorIs(t, err, cause)
			require.EqualError(t, err, "failed to write config file: failed to "+phase+" temp config file: "+cause.Error())
			expected := []string{"write", "sync", "close"}
			if phase == "write" {
				expected = []string{"write", "close"}
			}
			require.Equal(t, expected, fs.trace)
			require.Len(t, fs.stages, 1)
			require.Equal(t, fs.stages, fs.removed)
			require.Empty(t, fs.renames, "failure must not reach publication")
			got, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, original, got)
			after, err := os.Stat(path)
			require.NoError(t, err)
			require.True(t, os.SameFile(before, after))
			checkForeign := func() {
				t.Helper()
				got, err := os.ReadFile(foreign)
				require.NoError(t, err)
				require.Equal(t, []byte("foreign bytes"), got)
				info, err := os.Stat(foreign)
				require.NoError(t, err)
				require.True(t, os.SameFile(foreignBefore, info))
			}
			checkForeign()
			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			require.Len(t, entries, 2, "no temporary file or native lock may remain")
			require.NoError(t, healthy.Save(cfg, path), "retry must reacquire the real lock and publish")
			loaded, err := healthy.Load(path)
			require.NoError(t, err)
			require.Equal(t, cfg.Server.Port, loaded.Server.Port)
			checkForeign()
			entries, err = os.ReadDir(dir)
			require.NoError(t, err)
			require.Len(t, entries, 2)
		})
	}
}

// Snapshot through a handle before the attempted publication. On Windows a
// path-only os.Stat result may load its file identity lazily at SameFile time.
func saveProtocolIdentitySnapshot(t *testing.T, path string) os.FileInfo {
	t.Helper()
	file, err := os.Open(path)
	require.NoError(t, err)
	info, statErr := file.Stat()
	closeErr := file.Close()
	require.NoError(t, statErr)
	require.NoError(t, closeErr)
	return info
}
