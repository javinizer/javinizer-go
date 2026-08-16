//go:build !windows

package legacycsvsource

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/stretchr/testify/require"
)

type doneHookContext struct {
	context.Context
	once sync.Once
	hook func() error
	err  error
	done chan struct{}
}

func (c *doneHookContext) Done() <-chan struct{} {
	c.once.Do(func() { c.err = c.hook() })
	return c.done
}

func TestCollectReturnsCSVReadError(t *testing.T) {
	csvPath := writeCSV(t, "FullName,ThumbUrl\nOne,"+strings.Repeat("x", 128<<10)+"\n")
	ctx := &doneHookContext{Context: context.Background(), done: make(chan struct{})}
	ctx.hook = func() error {
		writeOnlyPath := filepath.Join(filepath.Dir(csvPath), "write-only")
		writeOnly, err := os.OpenFile(writeOnlyPath, os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		defer writeOnly.Close()

		var csvStat syscall.Stat_t
		if err := syscall.Stat(csvPath, &csvStat); err != nil {
			return err
		}
		replaced := 0
		for fd := 3; fd < 1024; fd++ {
			var fdStat syscall.Stat_t
			if err := syscall.Fstat(fd, &fdStat); err != nil || fdStat.Dev != csvStat.Dev || fdStat.Ino != csvStat.Ino {
				continue
			}
			if err := syscall.Dup2(int(writeOnly.Fd()), fd); err != nil {
				return err
			}
			replaced++
		}
		if replaced == 0 {
			return fmt.Errorf("open CSV descriptor not found")
		}
		return nil
	}

	err := New().Collect(ctx, actresscache.SourceOptions{
		Parameters: map[string]string{"legacy.csv": csvPath},
	}, func(actresscache.Candidate) error { return nil })
	require.NoError(t, ctx.err)
	require.ErrorContains(t, err, "read legacy thumbnail CSV row 2")
}
