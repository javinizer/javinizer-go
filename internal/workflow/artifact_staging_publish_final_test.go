package workflow

import (
	"errors"
	"os"

	"github.com/spf13/afero"
)

type pr260PublishFinalFS struct {
	afero.Fs
	op, path string
}

func (f *pr260PublishFinalFS) Remove(name string) error {
	if f.op == "remove" && name == f.path {
		return errors.New("original cleanup denied")
	}
	return f.Fs.Remove(name)
}
func (f *pr260PublishFinalFS) Stat(name string) (os.FileInfo, error) {
	if f.op == "stat" && name == f.path {
		return nil, errors.New("sidecar inspection denied")
	}
	return f.Fs.Stat(name)
}
func (f *pr260PublishFinalFS) Rename(old, new string) error {
	if f.op == "rename" && old == f.path {
		return errors.New("sidecar rename denied")
	}
	return f.Fs.Rename(old, new)
}

func (f *pr260PublishFinalFS) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if f.op == "write" && name == f.path && flag&(os.O_WRONLY|os.O_CREATE) != 0 {
		return nil, errors.New("sidecar target write denied")
	}
	return f.Fs.OpenFile(name, flag, perm)
}
