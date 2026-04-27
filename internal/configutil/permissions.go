package configutil

import (
	"os"
	"sync/atomic"
)

const (
	DirPerm     = 0777
	DirPermTemp = 0700
	FilePerm    = 0666
)

var cachedUmask atomic.Int32

func init() {
	cachedUmask.Store(0)
}

func StoreUmask(mask int) {
	cachedUmask.Store(int32(mask))
}

func ApplyUmask(perm os.FileMode) os.FileMode {
	return perm &^ os.FileMode(cachedUmask.Load())
}
