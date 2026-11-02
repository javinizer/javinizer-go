package organizer_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// A late claimant must remain queued across an abandoned owner's handoff to a
// reserved standby, rather than registering twice or publishing ahead of it.
// The synchronization observes only public completion channels, not tracker
// queues. Every filesystem path belongs to the disposable fixture.
func TestOrganizeCarriedWaiterPreservesPromotedWinner(t *testing.T) {
	for _, force := range []bool{false, true} {
		name := "ordinary conflict"
		if force {
			name = "authorized duplicate skip"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				root := t.TempDir()
				input := filepath.Join(root, "input")
				dest := filepath.Join(root, "library")
				require.NoError(t, os.Mkdir(input, 0700))
				sources := []string{filepath.Join(input, "A.mkv"), filepath.Join(input, "B.mkv"), filepath.Join(input, "C.mkv")}
				before := make(map[string]os.FileInfo)
				bytes := make(map[string]string)
				foreign := filepath.Join(root, "foreign.txt")
				for _, path := range append(append([]string{}, sources...), foreign) {
					bytes[path] = "original bytes for " + filepath.Base(path)
					require.NoError(t, os.WriteFile(path, []byte(bytes[path]), 0600))
					// Freeze identity while this path exists; Windows path Stat is lazy.
					handle, err := os.Open(path)
					require.NoError(t, err)
					info, statErr := handle.Stat()
					closeErr := handle.Close()
					require.NoError(t, statErr)
					require.NoError(t, closeErr)
					before[path] = info
				}
				org := organizer.NewOrganizer(afero.NewOsFs(), &organizer.Config{
					FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true,
					OperationMode: operationmode.OperationModeOrganize,
				}, nil, nil)
				tracker := organizer.NewDuplicateTracker(false)
				command := func(source string) organizer.OrganizeCmd {
					return organizer.OrganizeCmd{
						Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mkv", MovieID: "ABC-123"},
						Movie: &models.Movie{ID: "ABC-123"}, DestDir: dest, MoveFiles: true,
						ForceUpdate: force, DuplicateTracker: tracker,
					}
				}
				var primings []organizer.DuplicatePriming
				var target string
				for _, source := range sources {
					plan, err := org.PlanOrganize(context.Background(), command(source))
					require.NoError(t, err)
					require.True(t, plan.WillMove)
					if target == "" {
						target = plan.TargetPath
					}
					require.Equal(t, target, plan.TargetPath)
					rel, err := filepath.Rel(dest, plan.TargetPath)
					require.NoError(t, err)
					require.False(t, filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)))
					primings = append(primings, organizer.DuplicatePriming{SourcePath: source, TargetPath: plan.TargetPath, WillMove: plan.WillMove})
				}
				// A owns the destination; B is a reserved standby. C arrives after this
				// planning pass and must not overtake B when A abandons its claim.
				tracker.PrimeBatch(primings[:2])
				waiterCtx, cancelWaiter := context.WithCancel(context.Background())
				done := make(chan struct{})
				var loser *organizer.OrganizeResult
				var loserErr error
				go func() {
					loser, loserErr = org.Organize(waiterCtx, command(sources[2]))
					close(done)
				}()
				defer func() { cancelWaiter(); <-done }()
				synctest.Wait()
				select {
				case <-done:
					t.Fatal("late claimant finished while original owner was pending")
				default:
				}
				require.NoFileExists(t, target)

				cancelled, cancelOwner := context.WithCancel(context.Background())
				cancelOwner()
				abandoned, err := org.Organize(cancelled, command(sources[0]))
				require.ErrorIs(t, err, context.Canceled)
				require.Nil(t, abandoned)
				// Organize's entry cancellation cannot settle a claim it never entered;
				// this documented public caller-side recovery releases the abandoned owner.
				tracker.ReleaseAbandonedBy(sources[0])
				synctest.Wait()
				select {
				case <-done:
					t.Fatal("carried waiter overtook reserved standby after promotion")
				default:
				}
				require.NoFileExists(t, target)
				tracker.ReleaseAbandonedBy(sources[0]) // repeated recovery must not release B
				synctest.Wait()
				select {
				case <-done:
					t.Fatal("repeated abandonment released another source's claim")
				default:
				}

				winner, err := org.Organize(context.Background(), command(sources[1]))
				require.NoError(t, err)
				require.NotNil(t, winner)
				require.True(t, winner.Moved)
				require.Equal(t, target, winner.NewPath)
				<-done
				if force {
					require.NoError(t, loserErr)
					require.NotNil(t, loser)
					require.False(t, loser.Moved)
					require.True(t, loser.DuplicateSkipped)
					require.Len(t, loser.Warnings, 1)
					require.Contains(t, loser.Warnings[0], "already claimed by "+sources[1])
				} else {
					require.Error(t, loserErr)
					require.Contains(t, loserErr.Error(), target)
				}
				installed, err := os.Stat(target)
				require.NoError(t, err)
				require.True(t, os.SameFile(before[sources[1]], installed), "only B's admitted inode may become the winner")
				got, err := os.ReadFile(target)
				require.NoError(t, err)
				require.Equal(t, bytes[sources[1]], string(got))
				require.NoFileExists(t, sources[1])
				for _, path := range []string{sources[0], sources[2], foreign} {
					info, err := os.Stat(path)
					require.NoError(t, err)
					require.True(t, os.SameFile(before[path], info))
					got, err := os.ReadFile(path)
					require.NoError(t, err)
					require.Equal(t, bytes[path], string(got))
				}
				var published []string
				require.NoError(t, filepath.WalkDir(dest, func(path string, entry os.DirEntry, err error) error {
					if err != nil {
						return err
					}
					if !entry.IsDir() {
						published = append(published, path)
					}
					return nil
				}))
				require.Equal(t, []string{target}, published, "promotion publishes one winner without residual stages or competing copies")
			})
		})
	}
}
