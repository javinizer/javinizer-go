package workflow

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// FinalizeDeleteIntentCopyDigest seals the copy lane's INTERIM partial pin
// into the full-hash shape with the digest the publish stream teed — in the
// same serialized journal channel as every other intent mutation, so a crash
// can never observe a half-rewritten entry.
func TestFinalizeDeleteIntentCopyDigestSealsInterimPin(t *testing.T) {
	db, _ := pr260ArtifactDB(t)
	movie := pr260FencedMovie(t, db, "seal-pin", "")
	fs, root, _, _, _, _, match := pr260FencedFiles(t, "seal-pin")
	repo := database.NewBatchFileOperationRepository(db)
	log := NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "seal-pin", fs, nil, nil, nil)
	dest := filepath.Join(root, "library")
	opID, err := log.Begin(context.Background(), ApplyCmd{Movie: &movie, Match: match, DestPath: dest})
	require.NoError(t, err)

	videoTarget := filepath.Join(dest, "movie", "movie.mp4")
	sidecarTarget := filepath.Join(dest, "movie", "movie.srt")
	require.NoError(t, log.RecordDeleteIntent(context.Background(), opID, []models.DeleteEntry{
		{Path: sidecarTarget, SHA256: "aa11"},
		{Path: videoTarget, CopySize: 4096, CopyPartialSHA256: "bb22"},
	}))

	const sealed = "9F86D081884C7D659A2FEAA0C55AD015A3BF4F1B2B0B822CD15D6C15B0F00A08"
	require.NoError(t, log.FinalizeDeleteIntentCopyDigest(context.Background(), opID, videoTarget, sealed))

	ledger := p3Ledger(t, repo, opID)
	require.Len(t, ledger.PlannedDeletes, 2)
	byPath := map[string]models.DeleteEntry{}
	for _, pd := range ledger.PlannedDeletes {
		byPath[pd.Path] = pd
	}
	sealedEntry, ok := byPath[videoTarget]
	require.True(t, ok, "the sealed entry keeps its place in the pin set")
	assert.Equal(t, strings.ToLower(sealed), sealedEntry.SHA256, "sealed digests canonicalize to the hash leg's lowercase comparison")
	assert.Zero(t, sealedEntry.CopySize, "the seal rewrites the entry wholesale — no interim residue")
	assert.Empty(t, sealedEntry.CopyPartialSHA256)
	assert.Equal(t, "aa11", byPath[sidecarTarget].SHA256, "other pins are untouched")

	t.Run("a repeated seal is an idempotent no-op", func(t *testing.T) {
		require.NoError(t, log.FinalizeDeleteIntentCopyDigest(context.Background(), opID, videoTarget, sealed))
		again := p3Ledger(t, repo, opID)
		require.Len(t, again.PlannedDeletes, 2, "sealed entries are not duplicated or rewritten")
	})

	t.Run("an unknown path is a no-op", func(t *testing.T) {
		require.NoError(t, log.FinalizeDeleteIntentCopyDigest(context.Background(), opID, filepath.Join(dest, "movie", "never-pinned.bin"), sealed))
		assert.Len(t, p3Ledger(t, repo, opID).PlannedDeletes, 2)
	})

	t.Run("an already full-hash entry is not re-shaped", func(t *testing.T) {
		require.NoError(t, log.FinalizeDeleteIntentCopyDigest(context.Background(), opID, sidecarTarget, sealed))
		ledger := p3Ledger(t, repo, opID)
		for _, pd := range ledger.PlannedDeletes {
			if pd.Path == sidecarTarget {
				assert.Equal(t, "aa11", pd.SHA256, "the seal conforms only interim pins")
			}
		}
	})
}

func TestFinalizeDeleteIntentCopyDigestFaultLegs(t *testing.T) {
	db, _ := pr260ArtifactDB(t)
	repo := database.NewBatchFileOperationRepository(db)
	log := NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "seal-pin-fault", afero.NewMemMapFs(), nil, nil, nil)

	t.Run("unparsable operation id refuses", func(t *testing.T) {
		err := log.FinalizeDeleteIntentCopyDigest(context.Background(), "not-a-number", "/x", "aa")
		require.ErrorContains(t, err, "unparsable operation ID")
	})
	t.Run("zero operation id refuses", func(t *testing.T) {
		err := log.FinalizeDeleteIntentCopyDigest(context.Background(), "0", "/x", "aa")
		require.ErrorContains(t, err, "unparsable operation ID")
	})
	t.Run("empty endpoints refuse", func(t *testing.T) {
		require.ErrorContains(t, log.FinalizeDeleteIntentCopyDigest(context.Background(), "1", "", "aa"), "empty seal endpoint")
		require.ErrorContains(t, log.FinalizeDeleteIntentCopyDigest(context.Background(), "1", "/x", ""), "empty seal endpoint")
	})
	t.Run("an unknown row surfaces not-found like every intent mutation", func(t *testing.T) {
		err := log.FinalizeDeleteIntentCopyDigest(context.Background(), "987654", "/x", "aa")
		require.ErrorContains(t, err, "not found")
	})
	t.Run("empty opID is the no-log no-op", func(t *testing.T) {
		require.NoError(t, log.FinalizeDeleteIntentCopyDigest(context.Background(), "", "/x", "aa"))
	})
}

func TestFinalizeDeleteIntentCopyDigestMalformedJournal(t *testing.T) {
	db, _ := pr260ArtifactDB(t)
	movie := pr260FencedMovie(t, db, "seal-malformed", "")
	fs, _, _, _, _, _, match := pr260FencedFiles(t, "seal-malformed")
	repo := database.NewBatchFileOperationRepository(db)
	log := NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "seal-malformed", fs, nil, nil, nil)
	opID, err := log.Begin(context.Background(), ApplyCmd{Movie: &movie, Match: match, DestPath: t.TempDir()})
	require.NoError(t, err)
	require.NoError(t, db.Model(&models.BatchFileOperation{}).Where("id = ?", mustParseOpID(t, opID)).Update("generated_files", "{not json").Error)

	err = log.FinalizeDeleteIntentCopyDigest(context.Background(), opID, "/lib/m.mkv", "aa")
	require.Error(t, err, "a malformed journal fails the seal closed rather than rewriting blind")
}

// The no-op adapter answers the seal like every other intent no-op (scrape
// flows without a repository drive the same signature).
func TestNoOpRevertLogFinalizeDeleteIntentCopyDigest(t *testing.T) {
	require.NoError(t, noOpRevertLog{}.FinalizeDeleteIntentCopyDigest(context.Background(), "1", "/x", "aa"))
}
