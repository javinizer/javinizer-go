package database

import (
	"context"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/models"
)

type ActressRepository struct {
	*BaseRepository[models.Actress, uint]
	merger *actressMerger
}

func NewActressRepository(db *DB) *ActressRepository {
	repo := &ActressRepository{
		BaseRepository: NewBaseRepository[models.Actress, uint](
			db, "actress",
			func(a models.Actress) string { return fmt.Sprintf("%d", a.ID) },
			withDefaultOrder[models.Actress, uint]("japanese_name ASC, last_name ASC, first_name ASC, id ASC"),
			WithNewEntity[models.Actress, uint](func() models.Actress { return models.Actress{} }),
		),
	}
	repo.merger = &actressMerger{repo: repo}
	return repo
}

func (r *ActressRepository) Create(ctx context.Context, actress *models.Actress) error {
	return r.BaseRepository.Create(ctx, actress)
}

func (r *ActressRepository) Update(ctx context.Context, actress *models.Actress) error {
	if err := r.GetDB().WithContext(ctx).Save(actress).Error; err != nil {
		return wrapDBErr("update", fmt.Sprintf("actress %s", actress.JapaneseName), err)
	}
	return nil
}

func (r *ActressRepository) FindByID(ctx context.Context, id uint) (*models.Actress, error) {
	return r.BaseRepository.FindByID(ctx, id)
}

func (r *ActressRepository) Delete(ctx context.Context, id uint) error {
	return r.BaseRepository.Delete(ctx, id)
}

func (r *ActressRepository) Count(ctx context.Context) (int64, error) {
	return r.BaseRepository.Count(ctx)
}

func (r *ActressRepository) FindByDMMID(ctx context.Context, dmmID int) (*models.Actress, error) {
	if dmmID < 0 {
		return nil, wrapDBErr("find", fmt.Sprintf("actress by dmm_id %d", dmmID), ErrInvalidLookup)
	}
	if dmmID == 0 {
		return nil, wrapDBErr("find", fmt.Sprintf("actress by dmm_id %d", dmmID), ErrNotFound)
	}
	var actress models.Actress
	err := r.GetDB().WithContext(ctx).First(&actress, "dmm_id = ?", dmmID).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("actress by dmm_id %d", dmmID), err)
	}
	return &actress, nil
}

func (r *ActressRepository) FindByJapaneseName(ctx context.Context, name string) (*models.Actress, error) {
	var actress models.Actress
	err := r.GetDB().WithContext(ctx).Order("dmm_id DESC, id ASC").First(&actress, "japanese_name = ?", name).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("actress %s", name), err)
	}
	return &actress, nil
}

func (r *ActressRepository) FindByFirstNameLastName(ctx context.Context, firstName, lastName string) (*models.Actress, error) {
	var actress models.Actress
	err := r.GetDB().WithContext(ctx).Order("dmm_id DESC, id ASC").First(&actress, "first_name = ? AND last_name = ?", firstName, lastName).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("actress %s %s", lastName, firstName), err)
	}
	return &actress, nil
}

func (r *ActressRepository) FindByJapaneseNameAndDMMID(ctx context.Context, name string, dmmID int) (*models.Actress, error) {
	var actress models.Actress
	if name != "" && dmmID > 0 {
		err := r.GetDB().WithContext(ctx).First(&actress, "japanese_name = ? AND dmm_id = ?", name, dmmID).Error
		if err != nil {
			return nil, wrapDBErr("find", fmt.Sprintf("actress %s dmm_id %d", name, dmmID), err)
		}
		return &actress, nil
	} else if name != "" {
		return r.FindByJapaneseName(ctx, name)
	} else if dmmID > 0 {
		return r.FindByDMMID(ctx, dmmID)
	}
	return nil, wrapDBErr("find", "actress by japanese_name and dmm_id", ErrInvalidLookup)
}

func (r *ActressRepository) ListAll(ctx context.Context) ([]models.Actress, error) {
	return r.BaseRepository.ListAll(ctx)
}

func (r *ActressRepository) FindOrCreate(ctx context.Context, actress *models.Actress) error {
	if actress.JapaneseName != "" {
		existing, err := r.FindByJapaneseName(ctx, actress.JapaneseName)
		if err == nil {
			*actress = *existing
			return nil
		}
	}

	return r.Create(ctx, actress)
}

func (r *ActressRepository) List(ctx context.Context, limit, offset int) ([]models.Actress, error) {
	return r.BaseRepository.List(ctx, limit, offset)
}

func (r *ActressRepository) ListSorted(ctx context.Context, limit, offset int, sortBy, sortOrder string) ([]models.Actress, error) {
	var actresses []models.Actress

	sortBy, sortOrder, err := normalizeActressSort(sortBy, sortOrder)
	if err != nil {
		return nil, err
	}
	dbq := r.GetDB().WithContext(ctx)
	for _, clause := range actressOrderClauses(sortBy, sortOrder) {
		dbq = dbq.Order(clause)
	}

	err = dbq.Limit(limit).Offset(offset).Find(&actresses).Error
	if err != nil {
		return nil, wrapDBErr("find", "actresses", err)
	}
	return actresses, nil
}

func (r *ActressRepository) SearchPaged(ctx context.Context, query string, limit, offset int) ([]models.Actress, error) {
	var actresses []models.Actress

	searchPattern := "%" + query + "%"
	err := r.GetDB().WithContext(ctx).Where("first_name LIKE ? OR last_name LIKE ? OR japanese_name LIKE ?",
		searchPattern, searchPattern, searchPattern).
		Order("japanese_name ASC, last_name ASC, first_name ASC, id ASC").
		Limit(limit).
		Offset(offset).
		Find(&actresses).Error
	if err != nil {
		return nil, wrapDBErr("search", "actresses", err)
	}
	return actresses, nil
}

func (r *ActressRepository) SearchPagedSorted(ctx context.Context, query string, limit, offset int, sortBy, sortOrder string) ([]models.Actress, error) {
	var actresses []models.Actress

	sortBy, sortOrder, err := normalizeActressSort(sortBy, sortOrder)
	if err != nil {
		return nil, err
	}
	searchPattern := "%" + query + "%"

	dbq := r.GetDB().WithContext(ctx).Where("first_name LIKE ? OR last_name LIKE ? OR japanese_name LIKE ?",
		searchPattern, searchPattern, searchPattern)
	for _, clause := range actressOrderClauses(sortBy, sortOrder) {
		dbq = dbq.Order(clause)
	}

	err = dbq.Limit(limit).Offset(offset).Find(&actresses).Error
	if err != nil {
		return nil, wrapDBErr("search", "actresses", err)
	}
	return actresses, nil
}

func (r *ActressRepository) CountSearch(ctx context.Context, query string) (int64, error) {
	var count int64
	searchPattern := "%" + query + "%"
	err := r.GetDB().WithContext(ctx).Model(&models.Actress{}).
		Where("first_name LIKE ? OR last_name LIKE ? OR japanese_name LIKE ?",
			searchPattern, searchPattern, searchPattern).
		Count(&count).Error
	if err != nil {
		return 0, wrapDBErr("count", "search actresses", err)
	}
	return count, nil
}

func (r *ActressRepository) Search(ctx context.Context, query string) ([]models.Actress, error) {
	var actresses []models.Actress

	if query == "" {
		err := r.GetDB().WithContext(ctx).Limit(100).Order("japanese_name ASC, last_name ASC, first_name ASC").Find(&actresses).Error
		if err != nil {
			return nil, wrapDBErr("find", "actresses", err)
		}
		return actresses, nil
	}

	searchPattern := "%" + query + "%"
	err := r.GetDB().WithContext(ctx).Where("first_name LIKE ? OR last_name LIKE ? OR japanese_name LIKE ?",
		searchPattern, searchPattern, searchPattern).
		Order("japanese_name ASC, last_name ASC, first_name ASC").
		Limit(50).
		Find(&actresses).Error
	if err != nil {
		return nil, wrapDBErr("search", "actresses", err)
	}
	return actresses, nil
}

func (r *ActressRepository) PreviewMerge(ctx context.Context, targetID, sourceID uint) (*ActressMergePreview, error) {
	return r.merger.PreviewMerge(ctx, targetID, sourceID)
}

func (r *ActressRepository) Merge(ctx context.Context, targetID, sourceID uint, resolutions map[string]string) (*ActressMergeResult, error) {
	return r.merger.Merge(ctx, targetID, sourceID, resolutions, r.GetDB())
}
