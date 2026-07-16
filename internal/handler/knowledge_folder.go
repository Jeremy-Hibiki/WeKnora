package handler

import (
	"net/http"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/gin-gonic/gin"
)

// KnowledgeFolderHandler handles HTTP requests for knowledge folder operations.
type KnowledgeFolderHandler struct {
	folderService    interfaces.KnowledgeFolderService
	kbService        interfaces.KnowledgeBaseService
	knowledgeService interfaces.KnowledgeService
}

// NewKnowledgeFolderHandler creates a new knowledge folder handler instance.
func NewKnowledgeFolderHandler(
	folderService interfaces.KnowledgeFolderService,
	kbService interfaces.KnowledgeBaseService,
	knowledgeService interfaces.KnowledgeService,
) *KnowledgeFolderHandler {
	return &KnowledgeFolderHandler{
		folderService:    folderService,
		kbService:        kbService,
		knowledgeService: knowledgeService,
	}
}

// CreateFolder godoc
// @Summary      Create a new folder
// @Description  Create a new folder in the specified knowledge base
// @Tags         Folders
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "Knowledge Base ID"
// @Param        body body      types.CreateFolderRequest  true  "Folder creation request"
// @Success      201  {object}  types.KnowledgeFolder
// @Failure      400  {object}  errors.AppError
// @Failure      403  {object}  errors.AppError
// @Failure      409  {object}  errors.AppError
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id}/folders [post]
func (h *KnowledgeFolderHandler) CreateFolder(c *gin.Context) {
	ctx := c.Request.Context()
	kbID := secutils.SanitizeForLog(c.Param("id"))

	// Validate knowledge base access
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	if tenantID == 0 {
		logger.Error(ctx, "Failed to get tenant ID")
		c.JSON(http.StatusUnauthorized, errors.NewUnauthorizedError("Unauthorized"))
		return
	}

	kb, err := h.kbService.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil || kb.TenantID != tenantID {
		logger.Warnf(ctx, "Permission denied or KB not found: %s", kbID)
		c.JSON(http.StatusForbidden, errors.NewForbiddenError("Permission denied"))
		return
	}

	// Parse request body
	var req types.CreateFolderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Warnf(ctx, "Invalid request body: %v", err)
		c.JSON(http.StatusBadRequest, errors.NewBadRequestError("Invalid request body"))
		return
	}

	// Create folder
	folder, err := h.folderService.CreateFolder(ctx, kbID, &req)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"kb_id":            kbID,
			"folder_name":      req.Name,
			"parent_folder_id": req.ParentFolderID,
		})
		c.JSON(http.StatusInternalServerError, errors.NewInternalServerError(err.Error()))
		return
	}

	logger.Infof(ctx, "Folder created: %s (id=%s) in KB %s", folder.Name, folder.ID, kbID)
	c.JSON(http.StatusCreated, folder)
}

// GetFolder godoc
// @Summary      Get folder by ID
// @Description  Retrieve detailed information about a specific folder
// @Tags         Folders
// @Accept       json
// @Produce      json
// @Param        id         path      string  true  "Knowledge Base ID"
// @Param        folder_id  path      string  true  "Folder ID"
// @Success      200        {object}  types.KnowledgeFolder
// @Failure      403        {object}  errors.AppError
// @Failure      404        {object}  errors.AppError
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id}/folders/{folder_id} [get]
func (h *KnowledgeFolderHandler) GetFolder(c *gin.Context) {
	ctx := c.Request.Context()
	kbID := secutils.SanitizeForLog(c.Param("id"))
	folderID := secutils.SanitizeForLog(c.Param("folder_id"))

	// Validate knowledge base access
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	if tenantID == 0 {
		c.JSON(http.StatusUnauthorized, errors.NewUnauthorizedError("Unauthorized"))
		return
	}

	kb, err := h.kbService.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil || kb.TenantID != tenantID {
		c.JSON(http.StatusForbidden, errors.NewForbiddenError("Permission denied"))
		return
	}

	// Get folder
	folder, err := h.folderService.GetFolder(ctx, folderID)
	if err != nil {
		logger.Warnf(ctx, "Folder not found: %s", folderID)
		c.JSON(http.StatusNotFound, errors.NewNotFoundError("Folder not found"))
		return
	}

	// Verify folder belongs to this KB
	if folder.KnowledgeBaseID != kbID {
		c.JSON(http.StatusForbidden, errors.NewForbiddenError("Folder does not belong to this knowledge base"))
		return
	}

	c.JSON(http.StatusOK, folder)
}

// ListFolders godoc
// @Summary      List folders
// @Description  List all folders under a specific parent (or root if parent_id not provided)
// @Tags         Folders
// @Accept       json
// @Produce      json
// @Param        id         path      string  true   "Knowledge Base ID"
// @Param        parent_id  query     string  false  "Parent Folder ID (omit for root level)"
// @Success      200        {array}   types.KnowledgeFolder
// @Failure      403        {object}  errors.AppError
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id}/folders [get]
func (h *KnowledgeFolderHandler) ListFolders(c *gin.Context) {
	ctx := c.Request.Context()
	kbID := secutils.SanitizeForLog(c.Param("id"))

	// Validate knowledge base access
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	if tenantID == 0 {
		c.JSON(http.StatusUnauthorized, errors.NewUnauthorizedError("Unauthorized"))
		return
	}

	kb, err := h.kbService.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil || kb.TenantID != tenantID {
		c.JSON(http.StatusForbidden, errors.NewForbiddenError("Permission denied"))
		return
	}

	// Get parent_id from query
	parentIDStr := c.Query("parent_id")
	var parentID *string
	if parentIDStr != "" {
		parentID = &parentIDStr
	}

	// List folders
	folders, err := h.folderService.ListByParent(ctx, kbID, parentID)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"kb_id":     kbID,
			"parent_id": parentID,
		})
		c.JSON(http.StatusInternalServerError, errors.NewInternalServerError(err.Error()))
		return
	}

	c.JSON(http.StatusOK, folders)
}

// GetFolderTree godoc
// @Summary      Get folder tree
// @Description  Retrieve the complete folder hierarchy for a knowledge base
// @Tags         Folders
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "Knowledge Base ID"
// @Success      200  {array}   types.KnowledgeFolder
// @Failure      403  {object}  errors.AppError
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id}/folders/tree [get]
func (h *KnowledgeFolderHandler) GetFolderTree(c *gin.Context) {
	ctx := c.Request.Context()
	kbID := secutils.SanitizeForLog(c.Param("id"))

	// Validate knowledge base access
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	if tenantID == 0 {
		c.JSON(http.StatusUnauthorized, errors.NewUnauthorizedError("Unauthorized"))
		return
	}

	kb, err := h.kbService.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil || kb.TenantID != tenantID {
		c.JSON(http.StatusForbidden, errors.NewForbiddenError("Permission denied"))
		return
	}

	// Get folder tree
	tree, err := h.folderService.GetTree(ctx, kbID)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{"kb_id": kbID})
		c.JSON(http.StatusInternalServerError, errors.NewInternalServerError(err.Error()))
		return
	}

	c.JSON(http.StatusOK, tree)
}

// UpdateFolder godoc
// @Summary      Update folder
// @Description  Update folder properties (name, color, description, sort order)
// @Tags         Folders
// @Accept       json
// @Produce      json
// @Param        id         path      string  true  "Knowledge Base ID"
// @Param        folder_id  path      string  true  "Folder ID"
// @Param        body       body      types.UpdateFolderRequest  true  "Update request"
// @Success      200        {object}  types.KnowledgeFolder
// @Failure      400        {object}  errors.AppError
// @Failure      403        {object}  errors.AppError
// @Failure      404        {object}  errors.AppError
// @Failure      409        {object}  errors.AppError
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id}/folders/{folder_id} [put]
func (h *KnowledgeFolderHandler) UpdateFolder(c *gin.Context) {
	ctx := c.Request.Context()
	kbID := secutils.SanitizeForLog(c.Param("id"))
	folderID := secutils.SanitizeForLog(c.Param("folder_id"))

	// Validate knowledge base access
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	if tenantID == 0 {
		c.JSON(http.StatusUnauthorized, errors.NewUnauthorizedError("Unauthorized"))
		return
	}

	kb, err := h.kbService.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil || kb.TenantID != tenantID {
		c.JSON(http.StatusForbidden, errors.NewForbiddenError("Permission denied"))
		return
	}

	// Parse request body
	var req types.UpdateFolderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errors.NewBadRequestError("Invalid request body"))
		return
	}

	// Fetch and verify folder belongs to this KB before mutating.
	folder, err := h.folderService.GetFolder(ctx, folderID)
	if err != nil {
		if err == repository.ErrFolderNotFound {
			c.JSON(http.StatusNotFound, errors.NewNotFoundError("Folder not found"))
			return
		}
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"folder_id": folderID,
			"kb_id":     kbID,
		})
		c.JSON(http.StatusInternalServerError, errors.NewInternalServerError(err.Error()))
		return
	}
	if folder.KnowledgeBaseID != kbID {
		c.JSON(http.StatusForbidden, errors.NewForbiddenError("Folder does not belong to this knowledge base"))
		return
	}

	// Update folder
	updated, err := h.folderService.UpdateFolder(ctx, folderID, &req)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"folder_id": folderID,
			"kb_id":     kbID,
		})
		c.JSON(http.StatusInternalServerError, errors.NewInternalServerError(err.Error()))
		return
	}

	logger.Infof(ctx, "Folder updated: %s (id=%s)", updated.Name, updated.ID)
	c.JSON(http.StatusOK, updated)
	return
}

// DeleteFolder godoc
// @Summary      Delete folder
// @Description  Delete a folder (soft delete by default, use force=true for cascade delete: subfolders are deleted and knowledge entries are moved to root)
// @Tags         Folders
// @Accept       json
// @Produce      json
// @Param        id         path      string  true   "Knowledge Base ID"
// @Param        folder_id  path      string  true   "Folder ID"
// @Param        force      query     bool    false  "Force cascade delete (delete subfolders and move knowledge entries to root)"
// @Success      200        {object}  map[string]interface{}
// @Failure      400        {object}  errors.AppError
// @Failure      403        {object}  errors.AppError
// @Failure      404        {object}  errors.AppError
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id}/folders/{folder_id} [delete]
func (h *KnowledgeFolderHandler) DeleteFolder(c *gin.Context) {
	ctx := c.Request.Context()
	kbID := secutils.SanitizeForLog(c.Param("id"))
	folderID := secutils.SanitizeForLog(c.Param("folder_id"))
	force := c.Query("force") == "true"

	// Validate knowledge base access
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	if tenantID == 0 {
		c.JSON(http.StatusUnauthorized, errors.NewUnauthorizedError("Unauthorized"))
		return
	}

	kb, err := h.kbService.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil || kb.TenantID != tenantID {
		c.JSON(http.StatusForbidden, errors.NewForbiddenError("Permission denied"))
		return
	}

	// Verify folder belongs to this KB
	folder, err := h.folderService.GetFolder(ctx, folderID)
	if err != nil {
		c.JSON(http.StatusNotFound, errors.NewNotFoundError("Folder not found"))
		return
	}
	if folder.KnowledgeBaseID != kbID {
		c.JSON(http.StatusForbidden, errors.NewForbiddenError("Folder does not belong to this knowledge base"))
		return
	}

	// Delete folder
	if err := h.folderService.DeleteFolder(ctx, folderID, force); err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"folder_id": folderID,
			"force":     force,
		})
		c.JSON(http.StatusBadRequest, errors.NewBadRequestError(err.Error()))
		return
	}

	logger.Infof(ctx, "Folder deleted: %s (force=%v)", folderID, force)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Folder deleted successfully",
	})
}

// MoveFolder godoc
// @Summary      Move folder
// @Description  Move a folder to a new parent location
// @Tags         Folders
// @Accept       json
// @Produce      json
// @Param        id         path      string  true  "Knowledge Base ID"
// @Param        folder_id  path      string  true  "Folder ID"
// @Param        body       body      types.MoveFolderRequest  true  "Move request"
// @Success      200        {object}  types.KnowledgeFolder
// @Failure      400        {object}  errors.AppError
// @Failure      403        {object}  errors.AppError
// @Failure      404        {object}  errors.AppError
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id}/folders/{folder_id}/move [post]
func (h *KnowledgeFolderHandler) MoveFolder(c *gin.Context) {
	ctx := c.Request.Context()
	kbID := secutils.SanitizeForLog(c.Param("id"))
	folderID := secutils.SanitizeForLog(c.Param("folder_id"))

	// Validate knowledge base access
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	if tenantID == 0 {
		c.JSON(http.StatusUnauthorized, errors.NewUnauthorizedError("Unauthorized"))
		return
	}

	kb, err := h.kbService.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil || kb.TenantID != tenantID {
		c.JSON(http.StatusForbidden, errors.NewForbiddenError("Permission denied"))
		return
	}

	// Parse request body
	var req types.MoveFolderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errors.NewBadRequestError("Invalid request body"))
		return
	}

	// Verify folder belongs to this KB
	folder, err := h.folderService.GetFolder(ctx, folderID)
	if err != nil {
		c.JSON(http.StatusNotFound, errors.NewNotFoundError("Folder not found"))
		return
	}
	if folder.KnowledgeBaseID != kbID {
		c.JSON(http.StatusForbidden, errors.NewForbiddenError("Folder does not belong to this knowledge base"))
		return
	}

	// Move folder
	updatedFolder, err := h.folderService.MoveFolder(ctx, folderID, &req)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"folder_id":     folderID,
			"target_parent": req.TargetParentFolderID,
		})
		c.JSON(http.StatusBadRequest, errors.NewBadRequestError(err.Error()))
		return
	}

	logger.Infof(ctx, "Folder moved: %s to parent %v", folderID, req.TargetParentFolderID)
	c.JSON(http.StatusOK, updatedFolder)
}

// GetBreadcrumb godoc
// @Summary      Get folder breadcrumb
// @Description  Get the breadcrumb path from root to the specified folder
// @Tags         Folders
// @Accept       json
// @Produce      json
// @Param        id         path      string  true  "Knowledge Base ID"
// @Param        folder_id  path      string  true  "Folder ID"
// @Success      200        {array}   types.KnowledgeFolder
// @Failure      403        {object}  errors.AppError
// @Failure      404        {object}  errors.AppError
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id}/folders/{folder_id}/breadcrumb [get]
func (h *KnowledgeFolderHandler) GetBreadcrumb(c *gin.Context) {
	ctx := c.Request.Context()
	kbID := secutils.SanitizeForLog(c.Param("id"))
	folderID := secutils.SanitizeForLog(c.Param("folder_id"))

	// Validate knowledge base access
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	if tenantID == 0 {
		c.JSON(http.StatusUnauthorized, errors.NewUnauthorizedError("Unauthorized"))
		return
	}

	kb, err := h.kbService.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil || kb.TenantID != tenantID {
		c.JSON(http.StatusForbidden, errors.NewForbiddenError("Permission denied"))
		return
	}

	// Get breadcrumb
	breadcrumb, err := h.folderService.GetBreadcrumb(ctx, folderID)
	if err != nil {
		logger.Warnf(ctx, "Failed to get breadcrumb for folder %s: %v", folderID, err)
		c.JSON(http.StatusNotFound, errors.NewNotFoundError("Folder not found"))
		return
	}

	// Verify folder belongs to this KB
	if len(breadcrumb) > 0 && breadcrumb[len(breadcrumb)-1].KnowledgeBaseID != kbID {
		c.JSON(http.StatusForbidden, errors.NewForbiddenError("Folder does not belong to this knowledge base"))
		return
	}

	c.JSON(http.StatusOK, breadcrumb)
}

// TagByFolderRequest is the DTO for bulk tag-by-folder operations.
type TagByFolderRequest struct {
	FolderIDs []string `json:"folder_ids" binding:"required,min=1"`
	TagIDs    []string `json:"tag_ids" binding:"required,min=1"`
	Action    string   `json:"action" binding:"required,oneof=add remove"`
	Recursive *bool    `json:"recursive"`
}

// TagByFolder godoc
// @Summary      Bulk tag documents by folder
// @Description  Add or remove tags from all knowledge entries in a folder subtree. The folder is used as a selector — tags are written directly to knowledge_tag_relations. Only document KBs are supported.
// @Tags         Folders
// @Accept       json
// @Produce      json
// @Param        id   path      string  true  "Knowledge Base ID"
// @Param        body body      TagByFolderRequest  true  "Tag-by-folder request"
// @Success      200  {object}  map[string]interface{}
// @Failure      400  {object}  errors.AppError
// @Failure      403  {object}  errors.AppError
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id}/folders/tag-bulk [post]
func (h *KnowledgeFolderHandler) TagByFolder(c *gin.Context) {
	ctx := c.Request.Context()
	kbID := secutils.SanitizeForLog(c.Param("id"))

	// Defense-in-depth: verify tenant owns this KB (matches sibling folder handlers).
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	if tenantID == 0 {
		c.JSON(http.StatusUnauthorized, errors.NewUnauthorizedError("Unauthorized"))
		return
	}
	kb, err := h.kbService.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		c.JSON(http.StatusNotFound, errors.NewNotFoundError("Knowledge base not found"))
		return
	}
	if kb.TenantID != tenantID {
		c.JSON(http.StatusForbidden, errors.NewForbiddenError("Knowledge base does not belong to this tenant"))
		return
	}

	var req TagByFolderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Warnf(ctx, "Invalid tag-by-folder request: %v", err)
		c.JSON(http.StatusBadRequest, errors.NewBadRequestError("Invalid request body"))
		return
	}

	recursive := true
	if req.Recursive != nil {
		recursive = *req.Recursive
	}

	affectedCount, err := h.knowledgeService.TagByFolder(ctx, kbID, req.FolderIDs, req.TagIDs, req.Action, recursive)
	if err != nil {
		// Map AppError to its HTTPCode; unknown errors get 500 with no leakage.
		if appErr, ok := err.(*errors.AppError); ok {
			c.JSON(appErr.HTTPCode, appErr)
		} else {
			logger.ErrorWithFields(ctx, err, map[string]interface{}{
				"kb_id":   kbID,
				"action":  req.Action,
				"folders": len(req.FolderIDs),
				"tags":    len(req.TagIDs),
			})
			c.JSON(http.StatusInternalServerError, errors.NewInternalServerError("Internal server error"))
		}
		return
	}

	logger.Infof(ctx, "TagByFolder: kb=%s action=%s affected=%d", kbID, req.Action, affectedCount)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"affected_count": affectedCount,
		},
	})
}

// CountKnowledgeByFolderIDs godoc
// @Summary      Count documents in folder scope
// @Description  Returns the number of knowledge entries in a folder subtree. Read-only — used by the tag-by-folder dialog to preview affected document count.
// @Tags         Folders
// @Produce      json
// @Param        id          path   string  true  "Knowledge Base ID"
// @Param        folder_ids  query  string  true  "Comma-separated folder IDs"
// @Param        recursive   query  bool    false "Include descendant subfolders (default true)"
// @Success      200  {object}  map[string]interface{}
// @Failure      403  {object}  errors.AppError
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id}/folders/count [get]
func (h *KnowledgeFolderHandler) CountKnowledgeByFolderIDs(c *gin.Context) {
	ctx := c.Request.Context()
	kbID := secutils.SanitizeForLog(c.Param("id"))

	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	if tenantID == 0 {
		c.JSON(http.StatusUnauthorized, errors.NewUnauthorizedError("Unauthorized"))
		return
	}

	folderIDsStr := c.Query("folder_ids")
	if folderIDsStr == "" {
		c.JSON(http.StatusBadRequest, errors.NewBadRequestError("folder_ids is required"))
		return
	}
	folderIDs := strings.Split(folderIDsStr, ",")

	recursive := true
	if c.Query("recursive") == "false" {
		recursive = false
	}

	count, err := h.knowledgeService.CountKnowledgeByFolderIDs(ctx, tenantID, kbID, folderIDs, recursive)
	if err != nil {
		if appErr, ok := err.(*errors.AppError); ok {
			c.JSON(appErr.HTTPCode, appErr)
		} else {
			logger.Errorf(ctx, "CountKnowledgeByFolderIDs: %v", err)
			c.JSON(http.StatusInternalServerError, errors.NewInternalServerError("Internal server error"))
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"count": count,
		},
	})
}
