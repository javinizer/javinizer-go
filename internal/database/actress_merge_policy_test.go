package database

import (
	"context"
	"encoding/json"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestMergePoliciesScalarParity(t *testing.T) {
	for _, policy := range []string{"", MergePolicyFillEmpty, MergePolicyPreserveTarget} {
		for _, blank := range []string{"", " \t "} {
			t.Run(policy+"/"+blank, func(t *testing.T) {
				db, err := New(&Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
				require.NoError(t, err)
				defer db.Close()
				require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
				repo := NewActressRepository(db)
				target := models.Actress{FirstName: "桜", LastName: blank, JapaneseName: blank, ThumbURL: blank}
				source := models.Actress{DMMID: 902, FirstName: "Sakura", LastName: "Example", JapaneseName: "例", ThumbURL: "source.jpg"}
				require.NoError(t, repo.Create(t.Context(), &target))
				require.NoError(t, repo.Create(t.Context(), &source))
				opts := ActressMergeOptions{Policy: policy}
				preview, err := repo.PreviewMergeWithOptions(t.Context(), target.ID, source.ID, opts)
				require.NoError(t, err)
				require.Len(t, preview.Fields, 5)
				if policy == MergePolicyPreserveTarget {
					require.Equal(t, blank, preview.ProposedMerged.LastName)
					require.Equal(t, blank, preview.ProposedMerged.JapaneseName)
					require.Equal(t, blank, preview.ProposedMerged.ThumbURL)
					require.Zero(t, preview.ProposedMerged.DMMID)
				} else {
					require.Equal(t, source.LastName, preview.ProposedMerged.LastName)
					require.Equal(t, source.JapaneseName, preview.ProposedMerged.JapaneseName)
					require.Equal(t, source.ThumbURL, preview.ProposedMerged.ThumbURL)
					require.Equal(t, source.DMMID, preview.ProposedMerged.DMMID)
				}
				require.Equal(t, target.FirstName, preview.ProposedMerged.FirstName)
				encoded, err := json.Marshal(preview.Fields)
				require.NoError(t, err)
				require.Contains(t, string(encoded), `"target_value":0`)
				result, err := repo.MergeWithOptions(t.Context(), target.ID, source.ID, opts)
				require.NoError(t, err)
				require.Equal(t, actressMergeFields(&target, &source, &preview.ProposedMerged, preview.Policy), actressMergeFields(&target, &source, &result.MergedActress, preview.Policy))
				_, err = repo.FindByID(t.Context(), source.ID)
				require.True(t, IsNotFound(err))
			})
		}
	}
}

func TestMergePolicyExplicitDecisionsAndNormalization(t *testing.T) {
	target := models.Actress{FirstName: " ", LastName: "Target", JapaneseName: "標", ThumbURL: "target", DMMID: 1}
	source := models.Actress{FirstName: "Source", LastName: " \t", JapaneseName: "", ThumbURL: "", DMMID: 0}
	for _, policy := range []string{"", " FILL_EMPTY ", " PRESERVE_TARGET "} {
		decisions, err := normalizeActressMergeOptions(ActressMergeOptions{Policy: policy, Resolutions: map[string]string{
			"first_name": "target", "last_name": "source", "japanese_name": "source", "thumb_url": "source", "dmm_id": "source",
		}})
		require.NoError(t, err)
		merged := mergeActressValuesResolved(&target, &source, decisions)
		require.Equal(t, " ", merged.FirstName)
		require.Empty(t, merged.LastName)
		require.Empty(t, merged.JapaneseName)
		require.Empty(t, merged.ThumbURL)
		require.Zero(t, merged.DMMID)
	}
	for _, options := range []ActressMergeOptions{{Policy: "unknown"}, {Resolutions: map[string]string{"bad": "target"}}, {Resolutions: map[string]string{"last_name": "bad"}}} {
		_, err := normalizeActressMergeOptions(options)
		require.Error(t, err)
	}
}

func TestPreserveMergeRecomputesCurrentRows(t *testing.T) {
	db, err := New(&Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	repo := NewActressRepository(db)
	target := models.Actress{FirstName: "桜"}
	source := models.Actress{FirstName: "Source", LastName: "Old", DMMID: 102}
	require.NoError(t, repo.Create(t.Context(), &target))
	require.NoError(t, repo.Create(t.Context(), &source))
	plan, err := repo.merger.planWithOptions(t.Context(), target.ID, source.ID, ActressMergeOptions{Policy: MergePolicyPreserveTarget, Resolutions: map[string]string{"last_name": "source"}})
	require.NoError(t, err)
	require.NoError(t, db.Model(&models.Actress{}).Where("id = ?", source.ID).Update("last_name", "Current").Error)
	require.NoError(t, db.Model(&models.Actress{}).Where("id = ?", target.ID).Update("first_name", "Current target").Error)
	result, err := repo.merger.ExecuteMerge(t.Context(), plan, db)
	require.NoError(t, err)
	require.Equal(t, "Current", result.MergedActress.LastName)
	require.Equal(t, "Current target", result.MergedActress.FirstName)
	require.Zero(t, result.MergedActress.DMMID)
}

type legacyMergePolicyRepo struct{ ActressRepositoryInterface }

func (legacyMergePolicyRepo) PreviewMerge(context.Context, uint, uint) (*ActressMergePreview, error) {
	return &ActressMergePreview{}, nil
}
func (legacyMergePolicyRepo) Merge(context.Context, uint, uint, map[string]string) (*ActressMergeResult, error) {
	return &ActressMergeResult{}, nil
}

func TestMergePolicyAdaptersAndErrors(t *testing.T) {
	legacy := legacyMergePolicyRepo{}
	_, err := PreviewActressMerge(t.Context(), legacy, 1, 2, ActressMergeOptions{})
	require.NoError(t, err)
	_, err = ApplyActressMerge(t.Context(), legacy, 1, 2, ActressMergeOptions{})
	require.NoError(t, err)
	for _, options := range []ActressMergeOptions{{Policy: "invalid"}, {Policy: MergePolicyPreserveTarget}} {
		_, err = PreviewActressMerge(t.Context(), legacy, 1, 2, options)
		require.Error(t, err)
		_, err = ApplyActressMerge(t.Context(), legacy, 1, 2, options)
		require.Error(t, err)
	}
	db, err := New(&Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	repo := NewActressRepository(db)
	target := models.Actress{FirstName: "Target"}
	source := models.Actress{FirstName: "Source"}
	require.NoError(t, repo.Create(t.Context(), &target))
	require.NoError(t, repo.Create(t.Context(), &source))
	_, err = repo.PreviewMergeWithOptions(t.Context(), target.ID, source.ID, ActressMergeOptions{Policy: "bad"})
	require.ErrorIs(t, err, ErrActressMergeInvalidPolicy)
	_, err = repo.MergeWithOptions(t.Context(), target.ID, source.ID, ActressMergeOptions{Policy: "bad"})
	require.ErrorIs(t, err, ErrActressMergeInvalidPolicy)
	_, err = PreviewActressMerge(t.Context(), repo, target.ID, source.ID, ActressMergeOptions{Policy: MergePolicyPreserveTarget})
	require.NoError(t, err)
	_, err = ApplyActressMerge(t.Context(), repo, target.ID, source.ID, ActressMergeOptions{Policy: MergePolicyPreserveTarget})
	require.NoError(t, err)
}
