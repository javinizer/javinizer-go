package actress

import (
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/database"
)

func newMockActressRepo() *database.ActressRepository {
	return testkit.NewMockActressRepo()
}
