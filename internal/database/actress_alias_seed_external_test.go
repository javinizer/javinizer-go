package database_test

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	mocks "github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/stretchr/testify/mock"
)

// TestSeedDefaultActressAliases_ErrorPaths covers the two warn-and-continue
// branches in SeedDefaultActressAliases that a happy-path run never hits:
//   - FindByAliasName returning a non-NotFound error
//   - Create returning an error after a NotFound
func TestSeedDefaultActressAliases_ErrorPaths(t *testing.T) {
	repo := mocks.NewMockActressAliasRepositoryInterface(t)

	// defaultActressAliases loop order: 青木桃, 朝日芹奈, 堤セリナ, 与田さくら, 広瀬みつき.
	repo.EXPECT().FindByAliasName(mock.Anything, "青木桃").Return(nil, nil)                    // already present
	repo.EXPECT().FindByAliasName(mock.Anything, "朝日芹奈").Return(nil, nil)                   // already present
	repo.EXPECT().FindByAliasName(mock.Anything, "堤セリナ").Return(nil, errors.New("db down")) // !IsNotFound -> warn + continue
	repo.EXPECT().FindByAliasName(mock.Anything, "与田さくら").Return(nil, database.ErrNotFound) // proceed to Create
	repo.EXPECT().Create(mock.Anything, mock.Anything).Return(errors.New("create failed"))  // Create error -> warn
	repo.EXPECT().FindByAliasName(mock.Anything, "広瀬みつき").Return(nil, nil)                  // already present

	// Must not panic or return an error; the seed logs warnings and continues.
	database.SeedDefaultActressAliases(context.Background(), repo)
}
