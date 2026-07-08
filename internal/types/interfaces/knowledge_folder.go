package interfaces

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

// KnowledgeFolderRepository defines the data access interface for knowledge folders.
type KnowledgeFolderRepository interface {
	// Create creates a new folder.
	Create(ctx context.Context, folder *types.KnowledgeFolder) error
	// CreateInTx creates a new folder within the given transaction.
	CreateInTx(ctx context.Context, tx *gorm.DB, folder *types.KnowledgeFolder) error
	// GetByID retrieves a folder by its ID, scoped to tenant.
	GetByID(ctx context.Context, tenantID uint64, id string) (*types.KnowledgeFolder, error)
	// GetByIDForUpdate retrieves a folder with a row-level write lock (SELECT ... FOR UPDATE).
	// Use inside a transaction to serialize concurrent moves and prevent TOCTOU races.
	GetByIDForUpdate(ctx context.Context, tx *gorm.DB, tenantID uint64, id string) (*types.KnowledgeFolder, error)
	// ListByParent lists all folders directly under the given parent within a knowledge base.
	// When parentID is nil, returns root-level folders.
	ListByParent(ctx context.Context, tenantID uint64, kbID string, parentID *string) ([]*types.KnowledgeFolder, error)
	// GetAllInKB returns all non-deleted folders in a knowledge base (for building trees).
	GetAllInKB(ctx context.Context, tenantID uint64, kbID string) ([]*types.KnowledgeFolder, error)
	// Update updates folder properties (name, color, description, sort_order).
	Update(ctx context.Context, folder *types.KnowledgeFolder) error
	// Delete soft-deletes a folder by ID, scoped to tenant.
	Delete(ctx context.Context, tenantID uint64, id string) error
	// GetByPath retrieves a folder by its exact path within a knowledge base.
	GetByPath(ctx context.Context, tenantID uint64, kbID string, path string) (*types.KnowledgeFolder, error)
	// GetDescendants returns all descendant folders of the given folder (any depth),
	// scoped to tenant and knowledge base to keep materialized-path queries safe.
	GetDescendants(ctx context.Context, tenantID uint64, folderID string) ([]*types.KnowledgeFolder, error)
	// GetDescendantsInTx is GetDescendants executed within the given transaction.
	GetDescendantsInTx(ctx context.Context, tx *gorm.DB, tenantID uint64, folderID string) ([]*types.KnowledgeFolder, error)
	// CountKnowledge counts knowledge entries directly in a folder.
	CountKnowledge(ctx context.Context, tenantID uint64, folderID string) (int64, error)
	// CountKnowledgeByKB returns a map from folder_id to knowledge count for all folders in a KB.
	CountKnowledgeByKB(ctx context.Context, tenantID uint64, kbID string) (map[string]int64, error)
	// CountKnowledgeRecursive counts knowledge entries in a folder and all its descendants.
	CountKnowledgeRecursive(ctx context.Context, tenantID uint64, folderID string) (int64, error)
	// CheckNameExists checks if a folder with the given name already exists under a parent in the same KB.
	CheckNameExists(ctx context.Context, tenantID uint64, kbID string, parentID *string, name string, excludeID string) (bool, error)
	// CheckNameExistsInTx is CheckNameExists executed within the given transaction.
	CheckNameExistsInTx(ctx context.Context, tx *gorm.DB, tenantID uint64, kbID string, parentID *string, name string, excludeID string) (bool, error)
	// GetChildByNameInTx returns the direct child folder with the given name under
	// parentID (nil = root), or (nil, gorm.ErrRecordNotFound) when absent. Runs
	// within the given transaction so idempotent folder-tree creation is atomic.
	GetChildByNameInTx(ctx context.Context, tx *gorm.DB, tenantID uint64, kbID string, parentID *string, name string) (*types.KnowledgeFolder, error)
	// MoveSubtree atomically moves a folder and repaths all its descendants.
	// The folder's own parent_folder_id/path/depth is updated and every descendant's
	// path/depth is adjusted by the prefix replacement. Must be the only write the
	// caller performs in the transaction so the whole move is atomic. The depth
	// delta is applied via integer arithmetic (not string interpolation) to avoid
	// SQL injection; the prefix rewrite uses SUBSTR/OVERLAY to replace only the
	// leading oldPath segment, not every occurrence (unlike SQL REPLACE).
	MoveSubtree(ctx context.Context, tx *gorm.DB, tenantID uint64, id string, newParentID *string, newPath string, newDepth int, oldPath string, depthDelta int) error
	// ForceDeleteSubtree cascade-deletes a folder, all descendant folders, and
	// unlinks (folder_id = NULL) the knowledge entries that lived under them.
	// Executed atomically within the given transaction.
	ForceDeleteSubtree(ctx context.Context, tx *gorm.DB, tenantID uint64, id string) error
	// CreateManyInTx bulk-inserts folders within a transaction (used when building
	// a folder tree from an uploaded directory or unzipped archive).
	CreateManyInTx(ctx context.Context, tx *gorm.DB, folders []*types.KnowledgeFolder) error
	// FindByOwnerPath returns the folder owned by (tenantID, kbID) at the exact
	// materialized path, or nil when none exists. Used by the upload-folder
	// idempotent creation flow to detect existing folders and skip them.
	FindByOwnerPath(ctx context.Context, tenantID uint64, kbID string, path string) (*types.KnowledgeFolder, error)
}

// KnowledgeFolderService defines the business logic interface for knowledge folders.
type KnowledgeFolderService interface {
	// CreateFolder creates a new folder under the specified parent.
	CreateFolder(ctx context.Context, kbID string, req *types.CreateFolderRequest) (*types.KnowledgeFolder, error)
	// GetFolder retrieves a folder by its ID.
	GetFolder(ctx context.Context, id string) (*types.KnowledgeFolder, error)
	// ListByParent lists all folders directly under the given parent.
	ListByParent(ctx context.Context, kbID string, parentID *string) ([]*types.KnowledgeFolder, error)
	// GetTree returns the full folder tree for a knowledge base.
	GetTree(ctx context.Context, kbID string) ([]*types.KnowledgeFolder, error)
	// UpdateFolder updates folder properties.
	UpdateFolder(ctx context.Context, id string, req *types.UpdateFolderRequest) (*types.KnowledgeFolder, error)
	// DeleteFolder deletes a folder. When force is true, cascade-deletes all contents.
	DeleteFolder(ctx context.Context, id string, force bool) error
	// MoveFolder moves a folder to a new parent.
	MoveFolder(ctx context.Context, id string, req *types.MoveFolderRequest) (*types.KnowledgeFolder, error)
	// GetBreadcrumb returns the path of folders from root to the given folder.
	GetBreadcrumb(ctx context.Context, folderID string) ([]*types.KnowledgeFolder, error)
	// EnsureFolderPath creates any missing folders along a slash-separated relative
	// path (e.g. "docs/api/v2") rooted at parentFolderID (nil = KB root) and returns
	// the leaf folder. Existing segments are reused (skip conflict policy).
	EnsureFolderPath(ctx context.Context, kbID string, parentFolderID *string, relativePath string) (*types.KnowledgeFolder, error)
	// ValidateFolderOwnership returns nil iff folderID belongs to (tenantID, kbID).
	// When folderID is nil/empty, returns nil (root is always valid).
	ValidateFolderOwnership(ctx context.Context, tenantID uint64, kbID string, folderID *string) error
}
