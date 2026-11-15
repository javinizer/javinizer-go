package update

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReplaceBinaryWindowsRollbackFailurePreservesRecoveryFiles(t *testing.T) {
	t.Parallel()

	for _, interrupted := range []bool{false, true} {
		name := "installed binary"
		if interrupted {
			name = "recover interrupted upgrade first"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			target := filepath.Join(dir, "javinizer recovery.exe")
			old := target + ".old"
			source := filepath.Join(dir, "new replacement.exe")
			original := []byte("previous working executable")
			replacement := []byte("new replacement executable")
			originalPath := target
			if interrupted {
				originalPath = old
			}
			require.NoError(t, os.WriteFile(originalPath, original, 0o700))
			require.NoError(t, os.WriteFile(source, replacement, 0o700))
			// File.Stat captures identity from a handle. On Windows, os.Stat
			// can defer identity lookup until SameFile, after the path moved.
			snapshotIdentity := func(path string) os.FileInfo {
				t.Helper()
				file, openErr := os.Open(path)
				require.NoError(t, openErr)
				info, statErr := file.Stat()
				closeErr := file.Close() // release the handle before any rename
				require.NoError(t, statErr)
				require.NoError(t, closeErr)
				return info
			}
			originalInfo := snapshotIdentity(originalPath)
			sourceInfo := snapshotIdentity(source)

			installFailure := errors.New("fixture replacement rename rejected")
			rollbackFailure := errors.New("fixture rollback rename rejected")
			installErr := &os.LinkError{Op: "rename", Old: source, New: target, Err: installFailure}
			rollbackErr := &os.LinkError{Op: "rename", Old: old, New: target, Err: rollbackFailure}
			type renameAttempt struct{ from, to string }
			wantAttempts := []renameAttempt{{target, old}, {source, target}, {old, target}}
			if interrupted {
				wantAttempts = append([]renameAttempt{{old, target}}, wantAttempts...)
			}
			var attempts []renameAttempt
			err := replaceBinaryWindowsWithRename(target, source, func(from, to string) error {
				attempts = append(attempts, renameAttempt{from, to})
				require.LessOrEqual(t, len(attempts), len(wantAttempts), "no rename may follow a failed rollback")
				require.Equal(t, wantAttempts[len(attempts)-1], attempts[len(attempts)-1])
				switch len(attempts) {
				case len(wantAttempts) - 1, len(wantAttempts):
					// The preceding renames are real: both install and rollback
					// fail with the original binary already safe at .old.
					retained, readErr := os.ReadFile(old)
					require.NoError(t, readErr)
					require.Equal(t, original, retained)
					require.NoFileExists(t, target)
					if len(attempts) == len(wantAttempts)-1 {
						return installErr
					}
					return rollbackErr
				default:
					return os.Rename(from, to)
				}
			})

			require.Equal(t, wantAttempts, attempts)
			require.ErrorIs(t, err, installFailure, "the install failure must remain the primary wrapped cause")
			require.NotErrorIs(t, err, rollbackFailure, "rollback failure is a recovery diagnostic, not the primary cause")
			var wrapped *os.LinkError
			require.ErrorAs(t, err, &wrapped)
			require.Same(t, installErr, wrapped)
			require.EqualError(t, err, "install new binary: "+installErr.Error()+
				" (rollback also failed: "+rollbackErr.Error()+"; previous binary preserved at "+old+" — rename it back manually)")

			// Failed rollback must not fall through to successful-install cleanup.
			// Retain both original inodes, modes and bytes for manual recovery/retry.
			require.NoFileExists(t, target)
			for _, file := range []struct {
				path     string
				contents []byte
				before   os.FileInfo
			}{
				{old, original, originalInfo},
				{source, replacement, sourceInfo},
			} {
				after := snapshotIdentity(file.path)
				require.True(t, os.SameFile(file.before, after), "recovery file must preserve its inode: %s", file.path)
				require.Equal(t, file.before.Mode(), after.Mode())
				retained, readErr := os.ReadFile(file.path)
				require.NoError(t, readErr)
				require.Equal(t, file.contents, retained)
			}
			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			require.Len(t, entries, 2, "only the original backup and replacement source should remain")
			require.Equal(t, filepath.Base(old), entries[0].Name())
			require.Equal(t, filepath.Base(source), entries[1].Name())
		})
	}
}
