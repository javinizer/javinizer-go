package actress

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/stretchr/testify/assert"
)

func TestSafeFindTranslationsByIDs_NilRepo(t *testing.T) {
	deps := ActressDeps{}
	result, err := deps.safeFindTranslationsByIDs(context.Background(), []uint{1, 2}, "en")
	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestSafeFindTranslationByActress_NilRepo(t *testing.T) {
	deps := ActressDeps{}
	result, err := deps.safeFindTranslationByActress(context.Background(), 1, "en")
	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestNewActressDeps_Uncovered(t *testing.T) {
	content := database.ContentRepos{}
	translation := database.TranslationRepos{}
	deps := NewActressDeps(content, translation)
	assert.NotNil(t, deps)
}
