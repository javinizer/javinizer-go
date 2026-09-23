package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type pr260FiniteCopyFile struct {
	afero.File
	writeErr, closeErr bool
}

func (f *pr260FiniteCopyFile) Write(p []byte) (int, error) {
	if f.writeErr {
		return 0, errors.New("staged write denied")
	}
	return f.File.Write(p)
}
func (f *pr260FiniteCopyFile) Close() error {
	err := f.File.Close()
	if f.closeErr {
		return errors.New("staged close denied")
	}
	return err
}

type pr260FiniteCopyFS struct {
	afero.Fs
	op, sourceDir, source, sidecar string
}

func (f *pr260FiniteCopyFS) MkdirAll(name string, mode os.FileMode) error {
	if f.op == "staged mkdir" && strings.Contains(name, ".javinizer-apply-") && strings.HasSuffix(name, ".source") {
		return errors.New("staged directory denied")
	}
	return f.Fs.MkdirAll(name, mode)
}
func (f *pr260FiniteCopyFS) Open(name string) (afero.File, error) {
	if f.op == "source directory" && name == f.sourceDir {
		return nil, errors.New("source directory denied")
	}
	return f.Fs.Open(name)
}
func (f *pr260FiniteCopyFS) Stat(name string) (os.FileInfo, error) {
	if f.op == "sibling stat" && name == f.sidecar {
		return nil, errors.New("sidecar stat denied")
	}
	return f.Fs.Stat(name)
}
func (f *pr260FiniteCopyFS) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if strings.Contains(name, ".javinizer-apply-") && strings.HasSuffix(name, filepath.Base(f.source)) {
		if f.op == "staged create" {
			return nil, errors.New("staged create denied")
		}
		file, err := f.Fs.OpenFile(name, flag, perm)
		if err != nil {
			return nil, err
		}
		if f.op == "staged write" || f.op == "staged close" {
			return &pr260FiniteCopyFile{File: file, writeErr: f.op == "staged write", closeErr: f.op == "staged close"}, nil
		}
		return file, nil
	}
	return f.Fs.OpenFile(name, flag, perm)
}
func TestPR260FiniteArtifactCopyAndSiblingFailures(t *testing.T) {
	for _, tc := range []struct{ op, want string }{
		{"staged mkdir", "create staged source directory"},
		{"staged create", "create staged source"},
		{"staged write", "stage artifact source"},
		{"staged close", "close staged source"},
		{"source directory", "artifact staging source directory"},
		{"sibling stat", "artifact staging sibling"},
	} {
		t.Run(tc.op, func(t *testing.T) {
			base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "copy-"+tc.op)
			fs := &pr260FiniteCopyFS{Fs: base, op: tc.op, source: source, sourceDir: filepath.Dir(source), sidecar: subtitle}
			dest := filepath.Join(root, "published")
			cmd := ApplyCmd{Movie: &models.Movie{ContentID: "pr260-copy"}, PublicationFence: pr260FailureArtifactFencer{}, Match: match, DestPath: dest, Organize: OrganizeOptions{MoveFiles: true}}
			stage, _, err := (&applyOrchImpl{fs: fs}).prepareArtifact(context.Background(), cmd)
			require.ErrorContains(t, err, tc.want)
			require.Nil(t, stage)
			pr260AssertNoFinals(t, base, dest)
			pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
			pr260AssertStageGone(t, base, root)
		})
	}
}
