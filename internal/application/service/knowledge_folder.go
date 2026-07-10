package service

import (
	"context"
	"errors"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// knowledgeFolderService implements interfaces.KnowledgeFolderService.
type knowledgeFolderService struct {
	repo   interfaces.KnowledgeFolderRepository
	kgRepo interfaces.KnowledgeRepository
	db     *gorm.DB
}

// NewKnowledgeFolderService creates a new knowledge folder service.
func NewKnowledgeFolderService(
	repo interfaces.KnowledgeFolderRepository,
	kgRepo interfaces.KnowledgeRepository,
	db *gorm.DB,
) interfaces.KnowledgeFolderService {
	return &knowledgeFolderService{
		repo:   repo,
		kgRepo: kgRepo,
		db:     db,
	}
}

// CreateFolder creates a new folder under the specified parent.
func (s *knowledgeFolderService) CreateFolder(
	ctx context.Context,
	kbID string,
	req *types.CreateFolderRequest,
) (*types.KnowledgeFolder, error) {
	tenantID := types.MustTenantIDFromContext(ctx)

	// Validate name
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errors.New("folder name cannot be empty")
	}
	if strings.ContainsAny(name, "/\\\x00") {
		return nil, errors.New("folder name must not contain '/', '\\', or null bytes")
	}
	if len(name) > 255 {
		return nil, errors.New("folder name must not exceed 255 characters")
	}

	// Check name uniqueness under the same parent
	exists, err := s.repo.CheckNameExists(ctx, tenantID, kbID, req.ParentFolderID, name, "")
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, repository.ErrFolderNameExists
	}

	// Calculate path and depth
	var path string
	var depth int = 1
	if req.ParentFolderID != nil && *req.ParentFolderID != "" {
		parent, err := s.repo.GetByID(ctx, tenantID, *req.ParentFolderID)
		if err != nil {
			return nil, err
		}
		if parent.Depth >= types.MaxFolderDepth {
			return nil, repository.ErrMaxDepthExceeded
		}
		depth = parent.Depth + 1
		path = parent.Path
	} else {
		path = "/"
	}

	folder := &types.KnowledgeFolder{
		TenantID:        tenantID,
		KnowledgeBaseID: kbID,
		Name:            name,
		ParentFolderID:  req.ParentFolderID,
		Path:            path,
		Depth:           depth,
		Color:           req.Color,
		Description:     req.Description,
	}

	if err := s.repo.Create(ctx, folder); err != nil {
		return nil, err
	}

	// Update path to include the folder's own ID
	folder.Path = path + folder.ID + "/"
	if err := s.repo.Update(ctx, folder); err != nil {
		return nil, err
	}

	logger.Infof(ctx, "[Folder] Created folder %s (id=%s, depth=%d) in KB %s",
		folder.Name, folder.ID, folder.Depth, kbID)
	return folder, nil
}

// GetFolder retrieves a folder by its ID.
func (s *knowledgeFolderService) GetFolder(ctx context.Context, id string) (*types.KnowledgeFolder, error) {
	tenantID := types.MustTenantIDFromContext(ctx)
	folder, err := s.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	// Populate knowledge count
	count, err := s.repo.CountKnowledge(ctx, tenantID, folder.ID)
	if err != nil {
		logger.Warnf(ctx, "[Folder] Failed to count knowledge for folder %s: %v", id, err)
	} else {
		folder.KnowledgeCount = count
	}
	return folder, nil
}

// ListByParent lists all folders directly under the given parent.
func (s *knowledgeFolderService) ListByParent(
	ctx context.Context,
	kbID string,
	parentID *string,
) ([]*types.KnowledgeFolder, error) {
	tenantID := types.MustTenantIDFromContext(ctx)
	folders, err := s.repo.ListByParent(ctx, tenantID, kbID, parentID)
	if err != nil {
		return nil, err
	}
	// Bulk load counts for these folders
	counts, err := s.repo.CountKnowledgeByKB(ctx, tenantID, kbID)
	if err != nil {
		logger.Warnf(ctx, "[Folder] Failed to bulk count knowledge for KB %s: %v", kbID, err)
		return folders, nil
	}
	for _, f := range folders {
		f.KnowledgeCount = counts[f.ID]
	}
	return folders, nil
}

// GetTree returns the full folder tree for a knowledge base.
func (s *knowledgeFolderService) GetTree(
	ctx context.Context,
	kbID string,
) ([]*types.KnowledgeFolder, error) {
	tenantID := types.MustTenantIDFromContext(ctx)
	allFolders, err := s.repo.GetAllInKB(ctx, tenantID, kbID)
	if err != nil {
		return nil, err
	}
	// Bulk load knowledge counts for all folders
	counts, err := s.repo.CountKnowledgeByKB(ctx, tenantID, kbID)
	if err != nil {
		logger.Warnf(ctx, "[Folder] Failed to bulk count knowledge for KB %s: %v", kbID, err)
		counts = make(map[string]int64)
	}
	// Populate counts on each folder
	for _, f := range allFolders {
		f.KnowledgeCount = counts[f.ID]
	}
	// Build tree from flat list and accumulate child counts
	roots := buildFolderTree(allFolders)
	for _, root := range roots {
		populateChildCounts(root)
	}
	return roots, nil
}

// populateChildCounts recursively sums knowledge counts for a tree node.
func populateChildCounts(folder *types.KnowledgeFolder) int64 {
	total := folder.KnowledgeCount
	for _, child := range folder.Children {
		total += populateChildCounts(child)
	}
	folder.KnowledgeCount = total
	return total
}

// buildFolderTree converts a flat folder list into a tree structure.
func buildFolderTree(folders []*types.KnowledgeFolder) []*types.KnowledgeFolder {
	folderMap := make(map[string]*types.KnowledgeFolder, len(folders))
	for _, f := range folders {
		f.Children = make([]*types.KnowledgeFolder, 0)
		folderMap[f.ID] = f
	}

	var roots []*types.KnowledgeFolder
	for _, f := range folders {
		if f.ParentFolderID == nil || *f.ParentFolderID == "" {
			roots = append(roots, f)
		} else if parent, ok := folderMap[*f.ParentFolderID]; ok {
			parent.Children = append(parent.Children, f)
		} else {
			// Parent not found (might be deleted), treat as root
			roots = append(roots, f)
		}
	}
	return roots
}

// UpdateFolder updates folder properties.
func (s *knowledgeFolderService) UpdateFolder(
	ctx context.Context,
	id string,
	req *types.UpdateFolderRequest,
) (*types.KnowledgeFolder, error) {
	tenantID := types.MustTenantIDFromContext(ctx)
	folder, err := s.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}

	oldPath := folder.Path

	if req.Name != nil && *req.Name != "" {
		name := strings.TrimSpace(*req.Name)
		if name != folder.Name {
			if strings.ContainsAny(name, "/\\\x00") {
				return nil, errors.New("folder name must not contain '/', '\\', or null bytes")
			}
			if len(name) > 255 {
				return nil, errors.New("folder name must not exceed 255 characters")
			}
			// Check uniqueness
			exists, err := s.repo.CheckNameExists(ctx, tenantID, folder.KnowledgeBaseID,
				folder.ParentFolderID, name, folder.ID)
			if err != nil {
				return nil, err
			}
			if exists {
				return nil, repository.ErrFolderNameExists
			}
			folder.Name = name
		}
	}
	if req.Color != nil {
		folder.Color = *req.Color
	}
	if req.Description != nil {
		folder.Description = *req.Description
	}
	if req.SortOrder != nil {
		folder.SortOrder = *req.SortOrder
	}

	// If the name changed, we need to reconstruct the path (the last segment changes)
	// but since we use IDs for paths, renaming doesn't change paths. So just update.
	if err := s.repo.Update(ctx, folder); err != nil {
		return nil, err
	}

	_ = oldPath // no path change on rename since we use IDs in paths
	return folder, nil
}

// DeleteFolder deletes a folder. When force is true, cascade-deletes all contents.
func (s *knowledgeFolderService) DeleteFolder(ctx context.Context, id string, force bool) error {
	tenantID := types.MustTenantIDFromContext(ctx)
	folder, err := s.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return err
	}

	// Check if folder has subfolders
	children, err := s.repo.ListByParent(ctx, tenantID, folder.KnowledgeBaseID, &id)
	if err != nil {
		return err
	}

	// Check if folder has knowledge entries
	knowledgeCount, err := s.repo.CountKnowledge(ctx, tenantID, id)
	if err != nil {
		return err
	}

	if !force && (len(children) > 0 || knowledgeCount > 0) {
		return repository.ErrFolderNotEmpty
	}

	if force && (len(children) > 0 || knowledgeCount > 0) {
		// Cascade-delete atomically: the repo reads descendants, unlinks knowledge,
		// and deletes the subtree all within `tx` (no reads leak outside the txn).
		err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			return s.repo.ForceDeleteSubtree(ctx, tx, tenantID, id)
		})
		if err != nil {
			return err
		}
	} else {
		if err := s.repo.Delete(ctx, tenantID, id); err != nil {
			return err
		}
	}

	logger.Infof(ctx, "[Folder] Deleted folder %s (id=%s, force=%v)", folder.Name, folder.ID, force)
	return nil
}

// MoveFolder moves a folder to a new parent. All reads and writes run inside a
// single transaction so the cycle/name checks are atomic with the move itself,
// eliminating the TOCTOU race where two concurrent moves both pass the checks and
// then form a cycle. The source and target rows are locked (SELECT ... FOR UPDATE
// on PostgreSQL) so a concurrent move on the same subtree blocks until this txn
// commits.
func (s *knowledgeFolderService) MoveFolder(
	ctx context.Context,
	id string,
	req *types.MoveFolderRequest,
) (*types.KnowledgeFolder, error) {
	tenantID := types.MustTenantIDFromContext(ctx)

	var (
		kbIDForLog string
		name       string
		oldPath    string
		newPath    string
	)

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Lock the source folder.
		folder, err := s.repo.GetByIDForUpdate(ctx, tx, tenantID, id)
		if err != nil {
			return err
		}
		kbIDForLog = folder.KnowledgeBaseID
		name = folder.Name
		oldPath = folder.Path
		oldDepth := folder.Depth

		if req.TargetParentFolderID != nil && *req.TargetParentFolderID == id {
			return repository.ErrCircularReference
		}

		var newParentPath string
		newDepth := 1
		if req.TargetParentFolderID != nil && *req.TargetParentFolderID != "" {
			targetParent, err := s.repo.GetByIDForUpdate(ctx, tx, tenantID, *req.TargetParentFolderID)
			if err != nil {
				return err
			}
			if targetParent.KnowledgeBaseID != folder.KnowledgeBaseID {
				return repository.ErrCircularReference
			}
			// Cycle check: target must not be the source or one of its descendants.
			descendants, err := s.repo.GetDescendantsInTx(ctx, tx, tenantID, id)
			if err != nil {
				return err
			}
			for _, d := range descendants {
				if d.ID == *req.TargetParentFolderID {
					return repository.ErrCircularReference
				}
			}
			if targetParent.Depth >= types.MaxFolderDepth {
				return repository.ErrMaxDepthExceeded
			}
			newParentPath = targetParent.Path
			newDepth = targetParent.Depth + 1
		} else {
			newParentPath = "/"
			newDepth = 1
		}

		// Name uniqueness check inside the transaction.
		exists, err := s.repo.CheckNameExistsInTx(ctx, tx, tenantID, folder.KnowledgeBaseID,
			req.TargetParentFolderID, folder.Name, id)
		if err != nil {
			return err
		}
		if exists {
			return repository.ErrFolderNameExists
		}

		newPath = newParentPath + folder.ID + "/"
		depthDelta := newDepth - oldDepth
		return s.repo.MoveSubtree(ctx, tx, tenantID, id, req.TargetParentFolderID, newPath, newDepth, oldPath, depthDelta)
	})
	if err != nil {
		return nil, err
	}

	// Reload the updated folder.
	updated, err := s.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}

	logger.Infof(ctx, "[Folder] Moved folder %s (id=%s, kb=%s) from %s to %s",
		name, id, kbIDForLog, oldPath, newPath)
	return updated, nil
}

// GetBreadcrumb returns the path of folders from root to the given folder.
func (s *knowledgeFolderService) GetBreadcrumb(
	ctx context.Context,
	folderID string,
) ([]*types.KnowledgeFolder, error) {
	tenantID := types.MustTenantIDFromContext(ctx)
	folder, err := s.repo.GetByID(ctx, tenantID, folderID)
	if err != nil {
		return nil, err
	}

	// Parse the path to get ancestor folder IDs
	path := folder.Path
	if path == "/" || path == "/"+folderID+"/" {
		return []*types.KnowledgeFolder{folder}, nil
	}

	// Split path and fetch each ancestor (skip consecutive duplicate segments
	// that may exist from a previous path construction bug).
	// Path format: /ancestor1_id/ancestor2_id/current_id/
	segments := strings.Split(strings.Trim(path, "/"), "/")
	breadcrumb := make([]*types.KnowledgeFolder, 0, len(segments))
	var lastSegID string
	for _, seg := range segments {
		if seg == "" || seg == folderID || seg == lastSegID {
			continue
		}
		lastSegID = seg
		ancestor, err := s.repo.GetByID(ctx, tenantID, seg)
		if err != nil {
			logger.Warnf(ctx, "[Folder] Failed to fetch breadcrumb ancestor %s: %v", seg, err)
			continue
		}
		breadcrumb = append(breadcrumb, ancestor)
	}
	breadcrumb = append(breadcrumb, folder)
	return breadcrumb, nil
}

// ValidateFolderOwnership returns nil iff folderID belongs to (tenantID, kbID).
// A nil/empty folderID means "root" and is always valid. This is the ownership
// gate that CreateKnowledgeFromFile/URL and MoveToFolder/BatchMoveToFolder must
// call before trusting a client-supplied folder_id — the FK only guarantees the
// folder exists, not that it belongs to the caller's KB/tenant.
func (s *knowledgeFolderService) ValidateFolderOwnership(
	ctx context.Context,
	tenantID uint64,
	kbID string,
	folderID *string,
) error {
	if folderID == nil || *folderID == "" {
		return nil // root
	}
	folder, err := s.repo.GetByID(ctx, tenantID, *folderID)
	if err != nil {
		return err
	}
	if folder.KnowledgeBaseID != kbID {
		return repository.ErrFolderNotFound
	}
	return nil
}

// EnsureFolderPath creates any missing folders along a slash-separated relative
// path (e.g. "docs/api/v2") rooted at parentFolderID (nil = KB root) and returns
// the leaf folder. Existing segments are reused (skip conflict policy). The whole
// walk runs inside a single transaction with the root locked, so concurrent uploads
// cannot create duplicate folders. Backslashes are normalized to slashes; empty
// segments and "."/".." are dropped for path-traversal safety.
func (s *knowledgeFolderService) EnsureFolderPath(
	ctx context.Context,
	kbID string,
	parentFolderID *string,
	relativePath string,
) (*types.KnowledgeFolder, error) {
	tenantID := types.MustTenantIDFromContext(ctx)

	// Normalize segments.
	raw := strings.Split(strings.ReplaceAll(relativePath, "\\", "/"), "/")
	segments := make([]string, 0, len(raw))
	for _, seg := range raw {
		seg = strings.TrimSpace(seg)
		if seg == "" || seg == "." || seg == ".." {
			continue
		}
		segments = append(segments, seg)
	}
	if len(segments) == 0 {
		if parentFolderID != nil && *parentFolderID != "" {
			return s.repo.GetByID(ctx, tenantID, *parentFolderID)
		}
		return nil, nil
	}

	var leafID string
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Resolve + lock the starting parent.
		var (
			parentPath  string
			parentDepth int
			parentID    *string = parentFolderID
		)
		if parentFolderID != nil && *parentFolderID != "" {
			p, err := s.repo.GetByIDForUpdate(ctx, tx, tenantID, *parentFolderID)
			if err != nil {
				return err
			}
			if p.KnowledgeBaseID != kbID {
				return repository.ErrFolderNotFound
			}
			parentPath = p.Path
			parentDepth = p.Depth
		} else {
			parentPath = "/"
			parentDepth = 0
		}

		for _, seg := range segments {
			depth := parentDepth + 1
			if depth > types.MaxFolderDepth {
				return repository.ErrMaxDepthExceeded
			}
			// Reuse an existing same-named child if present (skip-on-conflict).
			existing, err := s.repo.GetChildByNameInTx(ctx, tx, tenantID, kbID, parentID, seg)
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if existing != nil {
				parentID = &existing.ID
				parentPath = existing.Path
				parentDepth = existing.Depth
				leafID = existing.ID
				continue
			}
			// Create the missing folder.
			folder := &types.KnowledgeFolder{
				TenantID:        tenantID,
				KnowledgeBaseID: kbID,
				Name:            seg,
				ParentFolderID:  parentID,
				Depth:           depth,
			}
			folder.ID = uuid.New().String()
			folder.Path = parentPath + folder.ID + "/"
			if err := s.repo.CreateInTx(ctx, tx, folder); err != nil {
				return err
			}
			parentID = &folder.ID
			parentPath = folder.Path
			parentDepth = depth
			leafID = folder.ID
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if leafID == "" {
		return nil, nil
	}
	return s.repo.GetByID(ctx, tenantID, leafID)
}

// (findChildByNameInTx removed: EnsureFolderPath now uses repo.GetChildByNameInTx
// so the lookup honors the configured repository — including the in-memory fake
// used in tests.)
