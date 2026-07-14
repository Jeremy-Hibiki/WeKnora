package handler

import (
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/gin-gonic/gin"
)

// maxFolderUploadFiles caps how many files a single folder/zip upload may carry.
// It protects the server from pathologically large directory trees and keeps the
// request within the async parser's throughput budget.
const maxFolderUploadFiles = 500

// skipReason is why a single uploaded file was not turned into a knowledge entry.
type skipReason struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// folderUploadResult summarises a folder/zip upload.
type folderUploadResult struct {
	Success        bool         `json:"success"`
	CreatedFolders int          `json:"created_folders"`
	UploadedFiles  int          `json:"uploaded_files"`
	SkippedFiles   int          `json:"skipped_files"`
	Skipped        []skipReason `json:"skipped"`
	Errors         []skipReason `json:"errors"`
}

// normalizeRelativePath cleans a client-supplied relative path:
//   - backslashes → forward slashes
//   - drops ".", "..", and empty segments (path-traversal protection)
//   - returns the directory portion and the base filename
//
// The returned dir is "" when the file lives at the upload root.
func normalizeRelativePath(raw string) (dir, base string, ok bool) {
	raw = strings.ReplaceAll(raw, "\\", "/")
	segments := strings.Split(raw, "/")
	cleaned := make([]string, 0, len(segments))
	for _, seg := range segments {
		seg = strings.TrimSpace(seg)
		if seg == "" || seg == "." || seg == ".." {
			continue
		}
		cleaned = append(cleaned, seg)
	}
	if len(cleaned) == 0 {
		return "", "", false
	}
	base = cleaned[len(cleaned)-1]
	if len(cleaned) > 1 {
		dir = strings.Join(cleaned[:len(cleaned)-1], "/")
	}
	return dir, base, true
}

// UploadFolder godoc
// @Summary      Upload a folder (native browser directory upload)
// @Description  Upload multiple files preserving their relative directory structure; folders are auto-created.
// @Tags         Knowledge
// @Accept       multipart/form-data
// @Produce      json
// @Param        id             path      string  true  "Knowledge Base ID"
// @Param        files          formData  file    true  "Files (repeated)"
// @Param        paths          formData  string  true  "Relative path for each file (repeated, same order as files)"
// @Param        root_folder_id formData  string  false "Parent folder ID (omit for KB root)"
// @Param        enable_multimodel formData string false "Enable multimodel parsing (applied to all files)"
// @Success      200  {object}  folderUploadResult
// @Failure      400  {object}  errors.AppError
// @Failure      403  {object}  errors.AppError
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id}/knowledge/folder [post]
func (h *KnowledgeHandler) UploadFolder(c *gin.Context) {
	h.uploadFolder(c, false)
}

// UploadZip godoc
// @Summary      Upload a zip archive and reconstruct its folder structure
// @Description  Extract a .zip and create folders/files matching the archive's directory layout.
// @Tags         Knowledge
// @Accept       multipart/form-data
// @Produce      json
// @Param        id             path      string  true  "Knowledge Base ID"
// @Param        file           formData  file    true  "Zip archive"
// @Param        root_folder_id formData  string  false "Parent folder ID (omit for KB root)"
// @Param        enable_multimodel formData string false "Enable multimodel parsing (applied to all files)"
// @Success      200  {object}  folderUploadResult
// @Failure      400  {object}  errors.AppError
// @Failure      403  {object}  errors.AppError
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id}/knowledge/zip [post]
func (h *KnowledgeHandler) UploadZip(c *gin.Context) {
	h.uploadFolder(c, true)
}

// uploadFolder is the shared implementation for both the native folder upload
// (webkitdirectory) and the zip upload. When isZip is true the single uploaded
// file is expected to be a .zip that the handler extracts server-side; the
// resulting entries are then processed exactly like the native folder case.
func (h *KnowledgeHandler) uploadFolder(c *gin.Context, isZip bool) {
	ctx := c.Request.Context()

	_, kbID, effectiveTenantID, permission, err := h.validateKnowledgeBaseAccess(c)
	if err != nil {
		c.Error(err)
		return
	}
	if permission != types.OrgRoleAdmin && permission != types.OrgRoleEditor {
		c.Error(errors.NewForbiddenError("No permission to upload knowledge"))
		return
	}
	ctx = context.WithValue(ctx, types.TenantIDContextKey, effectiveTenantID)

	// Resolve + validate the root folder (if any).
	rootFolderID := strings.TrimSpace(c.PostForm("root_folder_id"))
	var rootFolderIDPtr *string
	if rootFolderID != "" {
		rootFolderIDPtr = &rootFolderID
		if err := h.folderService.ValidateFolderOwnership(ctx, effectiveTenantID, kbID, rootFolderIDPtr); err != nil {
			c.Error(errors.NewBadRequestError("root_folder_id does not belong to this knowledge base"))
			return
		}
	}

	// Shared parse options applied to every file in this upload.
	var enableMultimodel *bool
	if v := c.PostForm("enable_multimodel"); v != "" {
		b, perr := parseBoolSafe(v)
		if perr != nil {
			c.Error(errors.NewBadRequestError("Invalid enable_multimodel format").WithDetails(perr.Error()))
			return
		}
		enableMultimodel = &b
	}
	tagIDs := parseCommaSeparatedTagIDs(c.PostForm("tag_ids"))
	channel := c.PostForm("channel")
	var processOverrides *types.KnowledgeProcessOverrides
	if raw := c.PostForm("process_config"); raw != "" {
		processOverrides = &types.KnowledgeProcessOverrides{}
		if err := parseJSONSafe(raw, processOverrides); err != nil {
			c.Error(errors.NewBadRequestError("Invalid process_config format").WithDetails(err.Error()))
			return
		}
	}

	var (
		files []*multipart.FileHeader
		paths []string
	)
	if isZip {
		zipHeader, err := c.FormFile("file")
		if err != nil {
			c.Error(errors.NewBadRequestError("Zip file is required").WithDetails(err.Error()))
			return
		}
		if !isZipArchive(zipHeader.Filename) {
			c.Error(errors.NewBadRequestError("Uploaded file is not a .zip archive"))
			return
		}
		extracted, extractPaths, extractErr := extractZipToEntries(zipHeader)
		if extractErr != nil {
			logger.ErrorWithFields(ctx, extractErr, map[string]interface{}{"zip": secutils.SanitizeForLog(zipHeader.Filename)})
			c.Error(errors.NewBadRequestError("Failed to extract zip archive").WithDetails(extractErr.Error()))
			return
		}
		files = extracted
		paths = extractPaths
	} else {
		form, ferr := c.MultipartForm()
		if ferr != nil {
			c.Error(errors.NewBadRequestError("Multipart form parse failed").WithDetails(ferr.Error()))
			return
		}
		files = form.File["files"]
		paths = form.Value["paths"]
	}

	if len(files) == 0 {
		c.Error(errors.NewBadRequestError("No files in upload"))
		return
	}
	if len(files) > maxFolderUploadFiles {
		c.Error(errors.NewBadRequestError("Too many files in folder upload (limit " + itoa(maxFolderUploadFiles) + ")"))
		return
	}
	if len(paths) < len(files) {
		// Fall back to each file's own name when paths are missing (acts like a flat upload).
		for i := len(paths); i < len(files); i++ {
			paths = append(paths, files[i].Filename)
		}
	}

	result := folderUploadResult{Success: true, Skipped: []skipReason{}, Errors: []skipReason{}}
	createdFolders := make(map[string]struct{}) // dedupe count by path

	maxSizeMB := secutils.GetMaxFileSizeMB()
	maxSize := maxSizeMB * 1024 * 1024

	for i, fh := range files {
		if fh.Size > maxSize {
			result.Skipped = append(result.Skipped, skipReason{Path: paths[i], Reason: fmt.Sprintf("file size exceeds %dMB limit", maxSizeMB)})
			result.SkippedFiles++
			continue
		}

		dir, base, ok := normalizeRelativePath(paths[i])
		if !ok {
			result.Errors = append(result.Errors, skipReason{Path: paths[i], Reason: "invalid path"})
			continue
		}
		if !validUploadFileType(base) {
			result.Skipped = append(result.Skipped, skipReason{Path: paths[i], Reason: "unsupported file type"})
			result.SkippedFiles++
			continue
		}

		// Ensure the directory chain exists (skip-on-conflict). The leaf is the
		// folder that should own this file; nil means KB root.
		var folderIDPtr *string
		if dir != "" {
			leaf, ferr := h.folderService.EnsureFolderPath(ctx, kbID, rootFolderIDPtr, dir)
			if ferr != nil {
				logger.Warnf(ctx, "[UploadFolder] EnsureFolderPath failed for %q: %v", dir, ferr)
				result.Errors = append(result.Errors, skipReason{Path: paths[i], Reason: "folder creation failed: " + ferr.Error()})
				continue
			}
			if leaf != nil {
				folderIDPtr = &leaf.ID
				createdFolders[leaf.Path] = struct{}{}
			}
		} else if rootFolderIDPtr != nil {
			folderIDPtr = rootFolderIDPtr
		}

		// Create the knowledge entry; duplicates surface as a typed error we skip on.
		knowledge, kErr := h.kgService.CreateKnowledgeFromFile(
			ctx, kbID, fh, nil, enableMultimodel, base, tagIDs, channel, processOverrides, folderIDPtr,
		)
		if kErr != nil {
			if isDuplicateKnowledgeError(kErr) {
				result.Skipped = append(result.Skipped, skipReason{Path: paths[i], Reason: "duplicate"})
				result.SkippedFiles++
				continue
			}
			logger.Warnf(ctx, "[UploadFolder] CreateKnowledgeFromFile failed for %q: %v", paths[i], kErr)
			result.Errors = append(result.Errors, skipReason{Path: paths[i], Reason: kErr.Error()})
			continue
		}
		_ = knowledge
		result.UploadedFiles++
	}

	result.CreatedFolders = len(createdFolders)
	logger.Infof(ctx, "[UploadFolder] kb=%s uploaded=%d skipped=%d created_folders=%d errors=%d",
		kbID, result.UploadedFiles, result.SkippedFiles, result.CreatedFolders, len(result.Errors))

	c.JSON(http.StatusOK, result)
}

// validUploadFileType mirrors the service-layer file-type allowlist without
// importing the service package (avoid a cycle).
func validUploadFileType(name string) bool {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	switch ext {
	case "pdf", "txt", "docx", "doc", "epub", "mhtml", "md", "markdown", "png", "jpg", "jpeg", "gif",
		"csv", "xlsx", "xls", "pptx", "ppt", "json", "mp3", "wav", "m4a", "flac", "ogg":
		return true
	}
	return false
}
