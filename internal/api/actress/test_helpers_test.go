package actress

import (
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/database"
	"testing"
)

func newMockActressRepo(t *testing.T) *database.ActressRepository {
	return testkit.NewMockActressRepo(t)
}
