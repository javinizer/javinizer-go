package organizer

import (
	"github.com/javinizer/javinizer-go/internal/models"
)

type OperationStrategy interface {
	Plan(match models.FileMatchInfo, movie *models.Movie, destDir string, forceUpdate bool) (*OrganizePlan, error)
	Execute(plan *OrganizePlan) (*OrganizeResult, error)
}
