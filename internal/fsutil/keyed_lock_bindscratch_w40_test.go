package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// W40: every bindScratchForUnlink arm, reached THROUGH boundProbeCleanup with
// phase-sequenced doubles (the cleanup's own create-path stat, the scratch
// reverify, and only THEN the inner bind's open/fstat/link legs).

type w40ScratchFile struct{ info os.FileInfo }

func (f w40ScratchFile) Stat() (os.FileInfo, error) { return f.info, nil }
func (f w40ScratchFile) Close() error               { return nil }

type w40FailStatFile struct{ err error }

func (f w40FailStatFile) Stat() (os.FileInfo, error) { return nil, f.err }
func (f w40FailStatFile) Close() error               { return nil }

// w40SeqAnswers serves scripted responses (in call order) for each channel.
type w40SeqAnswers struct {
	stats  []func() (os.FileInfo, error)
	openAt int
	open   func() (caseProbeFile, error)
	stat   []func() (os.FileInfo, error)
}

func (s *w40SeqAnswers) nextStat() (os.FileInfo, error) {
	if len(s.stat) == 0 {
		return nil, errors.New("w40: stat called past its script")
	}
	ans := s.stat[0]
	s.stat = s.stat[1:]
	return ans()
}

func TestBindScratchForUnlinkW40_Legs(t *testing.T) {
	w39BindIdentity(t)
	created := &w38ProbeInfo{}
	other := &w38ProbeInfo{}
	path := filepath.Join(t.TempDir(), ".javinizer_case_probe_w40")
	openErr := errors.New("w40 open wedged")
	fstatErr := errors.New("w40 fstat wedged")
	linkErr := errors.New("w40 linkstat wedged")

	mk := func(script []func() (os.FileInfo, error), open func() (caseProbeFile, error), removedGracefully *int) caseProbeOps {
		seq := &w40SeqAnswers{stat: script, open: open}
		return caseProbeOps{
			stat:     func(string) (os.FileInfo, error) { return seq.nextStat() },
			rename:   func(string, string) error { return nil },
			openFile: func(string, int, os.FileMode) (caseProbeFile, error) { return open() },
			remove: func(string) error {
				if removedGracefully != nil {
					*removedGracefully++
				}
				return nil
			},
		}
	}

	// phase order of stat calls inside boundProbeCleanup→bindScratchForUnlink:
	//  0: create-path re-proof (ops stat of path)
	//  1: post-move scratch reverify (ops stat of scratch)
	//  2+: bindScratch's own ops stat of scratch (only when reached)
	found := func() (os.FileInfo, error) { return created, nil }

	t.Run("bind-open ENOENT completes silently without remove", func(t *testing.T) {
		removed := 0
		ops := mk([]func() (os.FileInfo, error){found, found}, func() (caseProbeFile, error) { return nil, os.ErrNotExist }, &removed)
		require.NoError(t, boundProbeCleanup(ops, path, created))
		require.Zero(t, removed)
	})

	t.Run("bind-open error surfaces", func(t *testing.T) {
		ops := mk([]func() (os.FileInfo, error){found, found}, func() (caseProbeFile, error) { return nil, openErr }, nil)
		require.ErrorIs(t, boundProbeCleanup(ops, path, created), openErr)
	})

	t.Run("bind fstat error surfaces", func(t *testing.T) {
		ops := mk([]func() (os.FileInfo, error){found, found}, func() (caseProbeFile, error) { return w40FailStatFile{err: fstatErr}, nil }, nil)
		require.ErrorIs(t, boundProbeCleanup(ops, path, created), fstatErr)
	})

	t.Run("descriptor identity mismatch refuses without remove", func(t *testing.T) {
		removed := 0
		ops := mk([]func() (os.FileInfo, error){found, found}, func() (caseProbeFile, error) { return w40ScratchFile{info: other}, nil }, &removed)
		err := boundProbeCleanup(ops, path, created)
		require.ErrorIs(t, err, ErrTakeAsideForeign)
		require.Zero(t, removed)
	})

	t.Run("bind link ENOENT completes silently without remove", func(t *testing.T) {
		removed := 0
		ops := mk([]func() (os.FileInfo, error){found, found, func() (os.FileInfo, error) { return nil, os.ErrNotExist }},
			func() (caseProbeFile, error) { return w40ScratchFile{info: created}, nil }, &removed)
		require.NoError(t, boundProbeCleanup(ops, path, created))
		require.Zero(t, removed)
	})

	t.Run("bind link error surfaces", func(t *testing.T) {
		ops := mk([]func() (os.FileInfo, error){found, found, func() (os.FileInfo, error) { return nil, linkErr }},
			func() (caseProbeFile, error) { return w40ScratchFile{info: created}, nil }, nil)
		require.ErrorIs(t, boundProbeCleanup(ops, path, created), linkErr)
	})

	t.Run("descriptor-to-link divergence refuses without remove", func(t *testing.T) {
		removed := 0
		ops := mk([]func() (os.FileInfo, error){found, found, func() (os.FileInfo, error) { return other, nil }},
			func() (caseProbeFile, error) { return w40ScratchFile{info: created}, nil }, &removed)
		err := boundProbeCleanup(ops, path, created)
		require.ErrorIs(t, err, ErrTakeAsideForeign)
		require.Zero(t, removed)
	})

	t.Run("verified scratch unlinks", func(t *testing.T) {
		removed := 0
		ops := mk([]func() (os.FileInfo, error){found, found, found},
			func() (caseProbeFile, error) { return w40ScratchFile{info: created}, nil }, &removed)
		require.NoError(t, boundProbeCleanup(ops, path, created))
		require.Equal(t, 1, removed, "only the bound-verified scratch is unlinked")
	})
}
