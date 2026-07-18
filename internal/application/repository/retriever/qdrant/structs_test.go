package qdrant

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestToQdrantVectorEmbedding_FolderID(t *testing.T) {
	info := &types.IndexInfo{
		SourceID:        "src-1",
		SourceType:      types.ChunkSourceType,
		ChunkID:         "chunk-1",
		KnowledgeID:     "k-1",
		KnowledgeBaseID: "kb-1",
		Content:         "hello",
		FolderID:        "test-folder-id",
		IsEnabled:       true,
	}

	got := toQdrantVectorEmbedding(info, nil)

	assert.Equal(t, "test-folder-id", got.FolderID, "FolderID must be propagated from IndexInfo")
	assert.Equal(t, "src-1", got.SourceID)
	assert.Equal(t, "chunk-1", got.ChunkID)
	assert.Equal(t, "k-1", got.KnowledgeID)
	assert.Equal(t, "kb-1", got.KnowledgeBaseID)
}

func TestToQdrantVectorEmbedding_EmptyFolderID(t *testing.T) {
	info := &types.IndexInfo{
		SourceID:  "src-1",
		FolderID:  "",
		IsEnabled: true,
	}
	got := toQdrantVectorEmbedding(info, nil)
	assert.Empty(t, got.FolderID, "empty FolderID should stay empty (root-level)")
}
