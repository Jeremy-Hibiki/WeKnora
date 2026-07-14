package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRetrieveParams_FolderIDs(t *testing.T) {
	params := RetrieveParams{
		Query:            "what is RAG?",
		KnowledgeBaseIDs: []string{"kb-1"},
		FolderIDs:        []string{"folder-a", "folder-b"},
		TopK:             10,
	}

	assert.Equal(t, []string{"folder-a", "folder-b"}, params.FolderIDs)
	assert.Len(t, params.FolderIDs, 2)
	assert.Contains(t, params.FolderIDs, "folder-a")
	assert.Contains(t, params.FolderIDs, "folder-b")
}

func TestRetrieveParams_FolderIDs_Empty(t *testing.T) {
	params := RetrieveParams{
		Query: "no folder filter",
	}
	assert.Empty(t, params.FolderIDs, "default RetrieveParams should have no folder filter")
	assert.Nil(t, params.FolderIDs)
}
