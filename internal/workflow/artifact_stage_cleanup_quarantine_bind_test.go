package workflow

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
)

// codex P1 (PRRT_kwDORn9KaM6rLI26): cleanup's quarantine rename must stay
// bound to the directory its manifest+proof writes authenticated. A writer in
// the staging parent can move that root aside and plant a foreign tree at the
// same name inside the window; the sealed sidecar alone must never authorize
// the recursive removal of whatever the rename actually moved.
type cleanupRenameSwapFS struct {
	afero.Fs
	root  string
	fired bool
}

func (fs *cleanupRenameSwapFS) Rename(oldname, newname string) error {
	if !fs.fired && filepath.Clean(oldname) == filepath.Clean(fs.root) {
		info, err := fs.Fs.Stat(fs.root)
		if err != nil {
			return err
		}
		fs.fired = true
		aside := fs.root + ".attacker"
		if err := fs.Fs.Rename(fs.root, aside); err != nil {
			return err
		}
		if err := fs.Fs.MkdirAll(fs.root, 0o755); err != nil {
			return err
		}
		if err := afero.WriteFile(fs.Fs, filepath.Join(fs.root, "foreign.bin"), []byte("foreign"), 0o644); err != nil {
			return err
		}
		stamp := info.ModTime().Add(time.Second)
		if err := fs.Fs.Chtimes(fs.root, stamp, stamp); err != nil {
			return err
		}
	}
	return fs.Fs.Rename(oldname, newname)
}

func TestCleanupRetainsQuarantinedForeignTreeWhenRootSwappedInsideRename(t *testing.T) {
	base := afero.NewMemMapFs()
	parent := "/lib"
	require.NoError(t, base.MkdirAll(parent, 0o755))
	root := seedStagingRoot(t, base, parent, nil)
	faultFS := &cleanupRenameSwapFS{Fs: base, root: root}

	stage := &artifactStage{fs: faultFS, root: root}
	stage.cleanup()

	require.True(t, faultFS.fired, "the swap actually landed inside the rename seam")
	aside := root + ".attacker"

	entries, err := afero.ReadDir(base, parent)
	require.NoError(t, err)
	var quarantined string
	for _, entry := range entries {
		if strings.Contains(entry.Name(), artifactStageQuarantineMark) {
			quarantinesName := filepath.Join(parent, entry.Name())
			require.Empty(t, quarantined, "exactly one quarantined name")
			quarantined = quarantinesName
		}
	}
	require.NotEmpty(t, quarantined, "the tree the rename actually moved keeps its quarantine name")

	foreign, err := afero.Exists(base, filepath.Join(quarantined, "foreign.bin"))
	require.NoError(t, err)
	assert.True(t, foreign, "the quarantined tree is the planted one — and it was retained, never removed")

	manifestAtQuarantine, err := afero.Exists(base, filepath.Join(quarantined, artifactStageManifestName))
	require.NoError(t, err)
	assert.False(t, manifestAtQuarantine, "no sealed manifest ever entered the foreign tree")

	asideManifest, err := afero.Exists(base, filepath.Join(aside, artifactStageManifestName))
	require.NoError(t, err)
	assert.True(t, asideManifest, "the authenticated tree survives at the attacker's aside name with its manifest")
}

// codex P2 (PRRT_kwDORn9KaM6rLI28): with the primary deferred, multipart
// video siblings defer with it instead of copying multi-gigabyte bytes into
// the staging tree.
func TestPrepareArtifactOrganizeDefersMultipartSiblingCopies(t *testing.T) {
	base, _, source, subtitle, multipart, _, match := pr260FencedFiles(t, "defer-multipart")
	require.NotEmpty(t, multipart, "fixture carries a multipart sibling")
	dest := filepath.Join(filepath.Dir(source), "library")
	cmd := ApplyCmd{
		Movie:            &models.Movie{ContentID: "defer-multipart"},
		PublicationFence: pr260FailureArtifactFencer{},
		Match:            match,
		DestPath:         dest,
		Organize:         OrganizeOptions{MoveFiles: true},
		OperationMode:    operationmode.OperationModeOrganize,
		Download:         true,
	}

	stage, staged, err := (&applyOrchImpl{fs: base, organizer: subtitleStagingOrganizer(base)}).prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	require.NotNil(t, stage)
	defer stage.cleanup()

	stagedSidecar := filepath.Join(stage.root, ".source", filepath.Base(subtitle))
	exists, err := afero.Exists(base, stagedSidecar)
	require.NoError(t, err)
	assert.True(t, exists, "the small subtitle sibling still stages")

	stagedPart := filepath.Join(stage.root, ".source", filepath.Base(multipart))
	exists, err = afero.Exists(base, stagedPart)
	require.NoError(t, err)
	assert.False(t, exists, "the multipart video sibling defers with the primary, no staged copy")

	videoSiblings := 0
	for _, sibling := range stage.siblings {
		if !isStagedArtifactVideoExtension(strings.ToLower(filepath.Ext(sibling.sourcePath))) {
			continue
		}
		videoSiblings++
		assert.True(t, sibling.deferred, "video sibling is tracked for direct deferred publication: %s", sibling.sourcePath)
		assert.Empty(t, sibling.stagedPath, "deferred video sibling carries no staged bytes")
		assert.True(t, sibling.identity.known, "admission identity pinned for the deferred sibling")
	}
	assert.Equal(t, 1, videoSiblings, "the multipart sibling is admitted exactly once")
	assert.Equal(t, stage.stagedSource, staged.Match.Path)
}
