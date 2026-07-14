package repository

import (
	"context"
	"errors"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Sentinel errors for folder operations.
var (
	ErrFolderNotFound    = errors.New("folder not found")
	ErrFolderNameExists  = errors.New("folder name already exists")
	ErrFolderNotEmpty    = errors.New("folder is not empty")
	ErrMaxDepthExceeded  = errors.New("maximum folder depth exceeded")
	ErrCircularReference = errors.New("cannot move folder to its own descendant")
)

// knowledgeFolderRepository implements interfaces.KnowledgeFolderRepository.
type knowledgeFolderRepository struct {
	db *gorm.DB
}

// NewKnowledgeFolderRepository creates a new knowledge folder repository.
func NewKnowledgeFolderRepository(db *gorm.DB) interfaces.KnowledgeFolderRepository {
	return &knowledgeFolderRepository{db: db}
}

// Create inserts a new folder record.
func (r *knowledgeFolderRepository) Create(ctx context.Context, folder *types.KnowledgeFolder) error {
	return r.db.WithContext(ctx).Create(folder).Error
}

// CreateInTx inserts a new folder record within the given transaction.
func (r *knowledgeFolderRepository) CreateInTx(ctx context.Context, tx *gorm.DB, folder *types.KnowledgeFolder) error {
	if tx == nil {
		tx = r.db
	}
	return tx.WithContext(ctx).Create(folder).Error
}

// GetByID retrieves a folder by ID, scoped to tenant.
func (r *knowledgeFolderRepository) GetByID(ctx context.Context, tenantID uint64, id string) (*types.KnowledgeFolder, error) {
	var folder types.KnowledgeFolder
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND id = ?", tenantID, id).
		First(&folder).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrFolderNotFound
		}
		return nil, err
	}
	return &folder, nil
}

// GetByIDForUpdate retrieves a folder with a row-level write lock (SELECT ... FOR UPDATE
// on PostgreSQL; a no-op plain read on SQLite). Use inside a transaction to serialize
// concurrent moves and prevent the TOCTOU race where two moves both pass the cycle /
// name checks and then clobber each other.
func (r *knowledgeFolderRepository) GetByIDForUpdate(ctx context.Context, tx *gorm.DB, tenantID uint64, id string) (*types.KnowledgeFolder, error) {
	if tx == nil {
		tx = r.db
	}
	var folder types.KnowledgeFolder
	q := tx.WithContext(ctx).
		Where("tenant_id = ? AND id = ?", tenantID, id)
	if tx.Dialector.Name() == "postgres" {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := q.First(&folder).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrFolderNotFound
		}
		return nil, err
	}
	return &folder, nil
}

// ListByParent lists all folders directly under the given parent within a knowledge base.
func (r *knowledgeFolderRepository) ListByParent(
	ctx context.Context,
	tenantID uint64,
	kbID string,
	parentID *string,
) ([]*types.KnowledgeFolder, error) {
	var folders []*types.KnowledgeFolder
	query := r.db.WithContext(ctx).
		Where("tenant_id = ? AND knowledge_base_id = ?", tenantID, kbID)
	if parentID == nil {
		query = query.Where("parent_folder_id IS NULL")
	} else {
		query = query.Where("parent_folder_id = ?", *parentID)
	}
	if err := query.Order("sort_order ASC, name ASC").Find(&folders).Error; err != nil {
		return nil, err
	}
	return folders, nil
}

// GetAllInKB returns all non-deleted folders in a knowledge base, ordered by path.
func (r *knowledgeFolderRepository) GetAllInKB(
	ctx context.Context,
	tenantID uint64,
	kbID string,
) ([]*types.KnowledgeFolder, error) {
	var folders []*types.KnowledgeFolder
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND knowledge_base_id = ?", tenantID, kbID).
		Order("path ASC").
		Find(&folders).Error; err != nil {
		return nil, err
	}
	return folders, nil
}

// Update updates folder properties.
func (r *knowledgeFolderRepository) Update(ctx context.Context, folder *types.KnowledgeFolder) error {
	return r.db.WithContext(ctx).Save(folder).Error
}

// Delete soft-deletes a folder by ID, scoped to tenant.
func (r *knowledgeFolderRepository) Delete(ctx context.Context, tenantID uint64, id string) error {
	result := r.db.WithContext(ctx).
		Where("tenant_id = ? AND id = ?", tenantID, id).
		Delete(&types.KnowledgeFolder{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrFolderNotFound
	}
	return nil
}

// GetByPath retrieves a folder by its exact path within a knowledge base.
func (r *knowledgeFolderRepository) GetByPath(
	ctx context.Context,
	tenantID uint64,
	kbID string,
	path string,
) (*types.KnowledgeFolder, error) {
	var folder types.KnowledgeFolder
	if err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND knowledge_base_id = ? AND path = ?", tenantID, kbID, path).
		First(&folder).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &folder, nil
}

// GetDescendants returns all descendant folders of the given folder (any depth),
// scoped to tenant and knowledge base so a path LIKE cannot bleed across tenants.
func (r *knowledgeFolderRepository) GetDescendants(
	ctx context.Context,
	tenantID uint64,
	folderID string,
) ([]*types.KnowledgeFolder, error) {
	return r.getDescendants(ctx, r.db, tenantID, folderID)
}

// GetDescendantsInTx is GetDescendants within the given transaction.
func (r *knowledgeFolderRepository) GetDescendantsInTx(
	ctx context.Context,
	tx *gorm.DB,
	tenantID uint64,
	folderID string,
) ([]*types.KnowledgeFolder, error) {
	if tx == nil {
		tx = r.db
	}
	return r.getDescendants(ctx, tx, tenantID, folderID)
}

func (r *knowledgeFolderRepository) getDescendants(
	ctx context.Context,
	db *gorm.DB,
	tenantID uint64,
	folderID string,
) ([]*types.KnowledgeFolder, error) {
	// First get the folder's path and kb scope.
	var folder types.KnowledgeFolder
	if err := db.WithContext(ctx).
		Select("path, knowledge_base_id").
		Where("tenant_id = ? AND id = ?", tenantID, folderID).
		First(&folder).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrFolderNotFound
		}
		return nil, err
	}

	var descendants []*types.KnowledgeFolder
	// Scope by tenant + kb so even a pathological UUID path collision cannot pull
	// rows from another tenant's tree.
	if err := db.WithContext(ctx).
		Where("tenant_id = ? AND knowledge_base_id = ?", tenantID, folder.KnowledgeBaseID).
		Where("path LIKE ?", folder.Path+"%").
		Where("id != ?", folderID).
		Find(&descendants).Error; err != nil {
		return nil, err
	}
	return descendants, nil
}

func (r *knowledgeFolderRepository) GetMaxDepthInTx(
	ctx context.Context,
	tx *gorm.DB,
	tenantID uint64,
	folderID string,
) (int, error) {
	if tx == nil {
		tx = r.db
	}
	var folder types.KnowledgeFolder
	if err := tx.WithContext(ctx).
		Select("path, knowledge_base_id").
		Where("tenant_id = ? AND id = ?", tenantID, folderID).
		First(&folder).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, ErrFolderNotFound
		}
		return 0, err
	}

	var maxDepth int
	if err := tx.WithContext(ctx).
		Model(&types.KnowledgeFolder{}).
		Select("COALESCE(MAX(depth), 0)").
		Where("tenant_id = ? AND knowledge_base_id = ?", tenantID, folder.KnowledgeBaseID).
		Where("path LIKE ?", folder.Path+"%").
		Scan(&maxDepth).Error; err != nil {
		return 0, err
	}
	return maxDepth, nil
}

func (r *knowledgeFolderRepository) CountKnowledge(
	ctx context.Context,
	tenantID uint64,
	folderID string,
) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&types.Knowledge{}).
		Where("tenant_id = ? AND folder_id = ?", tenantID, folderID).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// CountKnowledgeByKB returns a map from folder_id to knowledge count for all folders in a KB.
// Uses a single GROUP BY query for efficiency.
func (r *knowledgeFolderRepository) CountKnowledgeByKB(
	ctx context.Context,
	tenantID uint64,
	kbID string,
) (map[string]int64, error) {
	type row struct {
		FolderID *string
		Count    int64
	}
	var rows []row
	if err := r.db.WithContext(ctx).Model(&types.Knowledge{}).
		Select("folder_id, count(*) as count").
		Where("tenant_id = ? AND knowledge_base_id = ? AND folder_id IS NOT NULL", tenantID, kbID).
		Group("folder_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	result := make(map[string]int64, len(rows))
	for _, r := range rows {
		if r.FolderID != nil {
			result[*r.FolderID] = r.Count
		}
	}
	return result, nil
}

// CountKnowledgeRecursive counts knowledge entries in a folder and all its descendants.
func (r *knowledgeFolderRepository) CountKnowledgeRecursive(
	ctx context.Context,
	tenantID uint64,
	folderID string,
) (int64, error) {
	// Get the folder to obtain its path and kb scope.
	var folder types.KnowledgeFolder
	if err := r.db.WithContext(ctx).
		Select("path, knowledge_base_id").
		Where("tenant_id = ? AND id = ?", tenantID, folderID).
		First(&folder).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, ErrFolderNotFound
		}
		return 0, err
	}

	// Collect all descendant folder IDs, scoped to tenant + kb.
	var folderIDs []string
	if err := r.db.WithContext(ctx).Model(&types.KnowledgeFolder{}).
		Where("tenant_id = ? AND knowledge_base_id = ?", tenantID, folder.KnowledgeBaseID).
		Where("path LIKE ?", folder.Path+"%").
		Pluck("id", &folderIDs).Error; err != nil {
		return 0, err
	}

	// Count knowledge entries in any of these folders
	var count int64
	if err := r.db.WithContext(ctx).Model(&types.Knowledge{}).
		Where("tenant_id = ? AND folder_id IN ?", tenantID, folderIDs).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// CheckNameExists checks if a folder with the given name already exists under a parent in the same KB.
func (r *knowledgeFolderRepository) CheckNameExists(
	ctx context.Context,
	tenantID uint64,
	kbID string,
	parentID *string,
	name string,
	excludeID string,
) (bool, error) {
	return r.checkNameExists(ctx, r.db, tenantID, kbID, parentID, name, excludeID)
}

// CheckNameExistsInTx is CheckNameExists within the given transaction.
func (r *knowledgeFolderRepository) CheckNameExistsInTx(
	ctx context.Context,
	tx *gorm.DB,
	tenantID uint64,
	kbID string,
	parentID *string,
	name string,
	excludeID string,
) (bool, error) {
	if tx == nil {
		tx = r.db
	}
	return r.checkNameExists(ctx, tx, tenantID, kbID, parentID, name, excludeID)
}

func (r *knowledgeFolderRepository) checkNameExists(
	ctx context.Context,
	db *gorm.DB,
	tenantID uint64,
	kbID string,
	parentID *string,
	name string,
	excludeID string,
) (bool, error) {
	var count int64
	query := db.WithContext(ctx).Model(&types.KnowledgeFolder{}).
		Where("tenant_id = ? AND knowledge_base_id = ? AND name = ?", tenantID, kbID, name)

	if parentID == nil {
		query = query.Where("parent_folder_id IS NULL")
	} else {
		query = query.Where("parent_folder_id = ?", *parentID)
	}

	if excludeID != "" {
		query = query.Where("id != ?", excludeID)
	}

	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// GetChildByNameInTx returns the direct child folder with the given name under
// parentID (nil = root), or (nil, gorm.ErrRecordNotFound) when absent.
func (r *knowledgeFolderRepository) GetChildByNameInTx(
	ctx context.Context,
	tx *gorm.DB,
	tenantID uint64,
	kbID string,
	parentID *string,
	name string,
) (*types.KnowledgeFolder, error) {
	if tx == nil {
		tx = r.db
	}
	q := tx.WithContext(ctx).
		Where("tenant_id = ? AND knowledge_base_id = ? AND name = ?", tenantID, kbID, name)
	if parentID == nil || *parentID == "" {
		q = q.Where("parent_folder_id IS NULL")
	} else {
		q = q.Where("parent_folder_id = ?", *parentID)
	}
	var f types.KnowledgeFolder
	if err := q.First(&f).Error; err != nil {
		return nil, err
	}
	return &f, nil
}

// MoveSubtree atomically moves a folder and repaths all its descendants within
// the given transaction. The folder's own parent_folder_id/path/depth is updated,
// then every descendant's path/depth is adjusted. Both writes run against `tx`, so
// the caller's Transaction() wrapper actually controls the commit boundary.
//
// The prefix rewrite avoids SQL REPLACE, which would substitute *every* occurrence
// of oldPath inside a descendant path rather than just the leading prefix. Instead
// we concatenate newPath with the suffix of the existing path past the oldPath
// prefix: path = newPath || SUBSTR(path, LENGTH(oldPath)+1).
func (r *knowledgeFolderRepository) MoveSubtree(
	ctx context.Context,
	tx *gorm.DB,
	tenantID uint64,
	id string,
	newParentID *string,
	newPath string,
	newDepth int,
	oldPath string,
	depthDelta int,
) error {
	if tx == nil {
		tx = r.db
	}
	// 1. Update the folder itself.
	if err := tx.WithContext(ctx).Model(&types.KnowledgeFolder{}).
		Where("tenant_id = ? AND id = ?", tenantID, id).
		Updates(map[string]interface{}{
			"parent_folder_id": newParentID,
			"path":             newPath,
			"depth":            newDepth,
		}).Error; err != nil {
		return err
	}
	// 2. Repath every descendant. The depth delta is bound as an integer parameter
	//    (never string-interpolated), and the prefix is replaced positionally.
	updateDepth := gorm.Expr("depth + ?", depthDelta)
	var pathExpr interface{}
	switch tx.Dialector.Name() {
	case "sqlite":
		// SQLite SUBSTR is 1-indexed.
		pathExpr = gorm.Expr("? || SUBSTR(path, LENGTH(?) + 1)", newPath, oldPath)
	default:
		// PostgreSQL: use OVERLAY so only the leading segment is replaced.
		pathExpr = gorm.Expr("OVERLAY(path PLACING ? FROM 1 FOR LENGTH(?))", newPath, oldPath)
	}
	return tx.WithContext(ctx).Model(&types.KnowledgeFolder{}).
		Where("tenant_id = ? AND path LIKE ?", tenantID, oldPath+"%").
		Where("id != ?", id).
		Updates(map[string]interface{}{
			"path":  pathExpr,
			"depth": updateDepth,
		}).Error
}

// ForceDeleteSubtree cascade-deletes a folder and all its descendants within the
// given transaction, first unlinking (folder_id = NULL) any knowledge entries that
// lived under them so the ON DELETE SET NULL FK does not fight us.
func (r *knowledgeFolderRepository) ForceDeleteSubtree(
	ctx context.Context,
	tx *gorm.DB,
	tenantID uint64,
	id string,
) error {
	if tx == nil {
		tx = r.db
	}
	descendants, err := r.getDescendants(ctx, tx, tenantID, id)
	if err != nil {
		return err
	}
	allFolderIDs := make([]string, 0, len(descendants)+1)
	for _, d := range descendants {
		allFolderIDs = append(allFolderIDs, d.ID)
	}
	allFolderIDs = append(allFolderIDs, id)

	// Unlink knowledge entries in these folders (folder_id = NULL).
	if err := tx.WithContext(ctx).Model(&types.Knowledge{}).
		Where("tenant_id = ? AND folder_id IN ?", tenantID, allFolderIDs).
		Update("folder_id", nil).Error; err != nil {
		return err
	}
	// Delete descendant folders first (deepest last) to avoid FK self-reference issues.
	for i := len(descendants) - 1; i >= 0; i-- {
		if err := tx.WithContext(ctx).
			Where("tenant_id = ? AND id = ?", tenantID, descendants[i].ID).
			Delete(&types.KnowledgeFolder{}).Error; err != nil {
			return err
		}
	}
	// Delete the folder itself.
	res := tx.WithContext(ctx).
		Where("tenant_id = ? AND id = ?", tenantID, id).
		Delete(&types.KnowledgeFolder{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrFolderNotFound
	}
	return nil
}

// CreateManyInTx bulk-inserts folders within a transaction (used when building a
// folder tree from an uploaded directory or unzipped archive).
func (r *knowledgeFolderRepository) CreateManyInTx(ctx context.Context, tx *gorm.DB, folders []*types.KnowledgeFolder) error {
	if len(folders) == 0 {
		return nil
	}
	if tx == nil {
		tx = r.db
	}
	return tx.WithContext(ctx).CreateInBatches(folders, 100).Error
}

// FindByOwnerPath returns the folder owned by (tenantID, kbID) at the exact
// materialized path, or nil when none exists. Used by the upload-folder idempotent
// creation flow to detect existing folders and skip them.
func (r *knowledgeFolderRepository) FindByOwnerPath(
	ctx context.Context,
	tenantID uint64,
	kbID string,
	path string,
) (*types.KnowledgeFolder, error) {
	var folder types.KnowledgeFolder
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND knowledge_base_id = ? AND path = ?", tenantID, kbID, path).
		First(&folder).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &folder, nil
}
