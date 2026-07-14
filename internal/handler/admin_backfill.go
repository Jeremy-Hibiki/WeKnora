package handler

import (
	"net/http"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/gin-gonic/gin"
)

// BackfillFolderMetadata backfills folder_id metadata in vector stores for
// existing chunks.
//
// POST /api/v1/admin/vector-stores/backfill-folder-metadata?kb_id=<optional>
//
// When kb_id is omitted the sweep covers every knowledge base across all
// tenants; when provided only that KB is processed. The response carries
// progress counters and a per-KB error list so the operator can see which
// KBs failed without re-running blindly.
func (h *KnowledgeHandler) BackfillFolderMetadata(c *gin.Context) {
	ctx := logger.CloneContext(c.Request.Context())
	kbID := c.Query("kb_id")

	result, err := h.kgService.BackfillFolderMetadata(ctx, kbID)
	if err != nil {
		logger.Errorf(ctx, "BackfillFolderMetadata failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}
