package organizer

import (
	"context"
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/testutil"
	"github.com/spf13/afero"
	"sync"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Regression: undetected multipart siblings collapsed to one destination and were
// silently overwritten when batch organize hardcoded ForceUpdate. Defaults must refuse
// to clobber; explicit force restores legacy replace behavior.
func TestOrganize_UndetectedMultipartSiblings_NeverOverwriteByDefault(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		FolderFormat:  "<ID>",
		FileFormat:    "<ID><IF:MULTIPART>-pt<PART></IF>",
		RenameFile:    true,
		OperationMode: operationmode.OperationModeOrganize,
	}
	org := NewOrganizer(fs, cfg, nil, nil)
	movie := testutil.NewMovieBuilder().WithID("IPX-535").Build()

	successes, failures := 0, 0
	for i := 1; i <= 7; i++ {
		src := fmt.Sprintf("/incoming/IPX-535-x%d.mp4", i)
		require.NoError(t, afero.WriteFile(fs, src, []byte(fmt.Sprintf("part-%d", i)), 0644))
		match := models.FileMatchInfo{
			Path: src, Name: fmt.Sprintf("IPX-535-x%d.mp4", i), Extension: ".mp4",
			MovieID: "IPX-535",
		}
		_, err := org.Organize(context.Background(), OrganizeCmd{
			Match: match, Movie: movie, DestDir: "/sorted", MoveFiles: true,
		})
		if err != nil {
			failures++
		} else {
			successes++
		}
	}

	assert.Equal(t, 1, successes, "exactly one part may occupy the shared destination")
	assert.Equal(t, 6, failures, "all other parts must fail with conflicts")

	entries, err := afero.ReadDir(fs, "/sorted/IPX-535")
	require.NoError(t, err)
	require.Len(t, entries, 1)

	srcEntries, err := afero.ReadDir(fs, "/incoming")
	require.NoError(t, err)
	assert.Len(t, srcEntries, 6, "conflicted sources must be preserved in place")
}

// The execute-time guard closes the plan→execute race: a destination created between
// planning and moving must conflict even if plan-time checks passed.
func TestOrganizeStrategy_Execute_RefusesLateConflictWithoutAuthorization(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		FolderFormat:  "<ID>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		OperationMode: operationmode.OperationModeOrganize,
	}
	org := NewOrganizer(fs, cfg, nil, nil)
	movie := testutil.NewMovieBuilder().WithID("IPX-123").Build()
	match := models.FileMatchInfo{Path: "/source/IPX-123.mp4", Name: "IPX-123.mp4", Extension: ".mp4", MovieID: "IPX-123"}
	require.NoError(t, afero.WriteFile(fs, match.Path, []byte("incoming"), 0644))

	strategy := org.strategyFromType(strategyOrganize)
	plan, err := strategy.Plan(match, movie, "/dest", false)
	require.NoError(t, err)

	require.NoError(t, afero.WriteFile(fs, "/dest/IPX-123/IPX-123.mp4", []byte("simultaneous-writer"), 0644))

	result, err := strategy.Execute(plan)
	require.Error(t, err)
	require.NotNil(t, result)
	assert.Contains(t, result.Error.Error(), "refusing to overwrite")

	content, err := afero.ReadFile(fs, "/dest/IPX-123/IPX-123.mp4")
	require.NoError(t, err)
	assert.Equal(t, []byte("simultaneous-writer"), content, "existing destination must not be replaced")

	sourceContent, err := afero.ReadFile(fs, "/source/IPX-123.mp4")
	require.NoError(t, err)
	assert.Equal(t, []byte("incoming"), sourceContent, "source must remain in place")
}

// Explicit authorization restores legacy replace behavior.
func TestOrganizeStrategy_Execute_AuthorizedOverwriteReplaces(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		FolderFormat:  "<ID>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		OperationMode: operationmode.OperationModeOrganize,
	}
	org := NewOrganizer(fs, cfg, nil, nil)
	movie := testutil.NewMovieBuilder().WithID("IPX-123").Build()
	match := models.FileMatchInfo{Path: "/source/IPX-123.mp4", Name: "IPX-123.mp4", Extension: ".mp4", MovieID: "IPX-123"}
	require.NoError(t, afero.WriteFile(fs, match.Path, []byte("incoming"), 0644))
	require.NoError(t, afero.WriteFile(fs, "/dest/IPX-123/IPX-123.mp4", []byte("previous"), 0644))

	result, err := org.Organize(context.Background(), OrganizeCmd{
		Match: match, Movie: movie, DestDir: "/dest", MoveFiles: true, ForceUpdate: true,
	})
	require.NoError(t, err)
	assert.True(t, result.Moved)

	content, err := afero.ReadFile(fs, "/dest/IPX-123/IPX-123.mp4")
	require.NoError(t, err)
	assert.Equal(t, []byte("incoming"), content)
}

// Concurrent plans-then-executes: every part plans FIRST (all plan-time checks pass —
// the destination is unused at plan time), then all executions race onto the shared
// destination. Exactly one may win; all losing sources must survive untouched.
func TestOrganize_UndetectedMultipartSiblings_ConcurrentPlansSerialize(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		FolderFormat:  "<ID>",
		FileFormat:    "<ID><IF:MULTIPART>-pt<PART></IF>",
		RenameFile:    true,
		OperationMode: operationmode.OperationModeOrganize,
	}
	org := NewOrganizer(fs, cfg, nil, nil)
	movie := testutil.NewMovieBuilder().WithID("IPX-535").Build()
	strategy := org.strategyFromType(strategyOrganize)

	const parts = 7
	plans := make([]*OrganizePlan, parts)
	for i := 1; i <= parts; i++ {
		src := fmt.Sprintf("/incoming/IPX-535-x%d.mp4", i)
		require.NoError(t, afero.WriteFile(fs, src, []byte(fmt.Sprintf("part-%d", i)), 0644))
		match := models.FileMatchInfo{
			Path: src, Name: fmt.Sprintf("IPX-535-x%d.mp4", i), Extension: ".mp4",
			MovieID: "IPX-535",
		}
		p, err := strategy.Plan(match, movie, "/sorted", false)
		require.NoError(t, err)
		plans[i-1] = p
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make([]*OrganizeResult, parts)
	errs := make([]error, parts)
	for i := 0; i < parts; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			results[idx], errs[idx] = strategy.Execute(plans[idx])
		}(i)
	}
	close(start)
	wg.Wait()

	moved := 0
	for i := range errs {
		if errs[i] == nil && results[i] != nil && results[i].Moved {
			moved++
		}
	}
	assert.Equal(t, 1, moved, "exactly one racing part may occupy the shared destination")

	// Exactly one winner occupies the destination, and its bytes must be one of the
	// seven part payloads — assert unconditionally so an unreadable or wrong-content
	// destination fails loudly instead of skipping the anti-clobber check.
	winnerDest, derr := afero.ReadFile(fs, "/sorted/IPX-535/IPX-535.mp4")
	require.NoError(t, derr, "shared destination must exist and be readable after one winner moves in")
	winnerIdx := -1
	for i := 1; i <= parts; i++ {
		if string(winnerDest) == fmt.Sprintf("part-%d", i) {
			winnerIdx = i
			break
		}
	}
	require.NotEqual(t, -1, winnerIdx, "destination bytes must be exactly one part's payload — any mixture or foreign bytes means a clobber")

	// All six losing parts still live under /incoming with their original bytes.
	preserved := 0
	for i := 1; i <= parts; i++ {
		if i == winnerIdx {
			continue
		}
		data, err := afero.ReadFile(fs, fmt.Sprintf("/incoming/IPX-535-x%d.mp4", i))
		if err == nil && string(data) == fmt.Sprintf("part-%d", i) {
			preserved++
		}
	}
	assert.Equal(t, parts-1, preserved, "all losing parts must remain at their source paths with original bytes")

	entries, err := afero.ReadDir(fs, "/sorted/IPX-535")
	require.NoError(t, err)
	require.Len(t, entries, 1, "shared destination holds exactly one file")
	assert.Equal(t, "IPX-535.mp4", entries[0].Name())
}
