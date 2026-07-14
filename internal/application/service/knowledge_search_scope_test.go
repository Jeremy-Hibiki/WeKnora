package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeTagRepoForResolve implements only GetByName from interfaces.KnowledgeTagRepository.
type fakeTagRepoForResolve struct {
	interfaces.KnowledgeTagRepository
	byName map[string]*types.KnowledgeTag // keyed by lowercased name
}

func (r *fakeTagRepoForResolve) GetByName(_ context.Context, _ uint64, _ string, name string) (*types.KnowledgeTag, error) {
	if tag, ok := r.byName[name]; ok {
		return tag, nil
	}
	return nil, errors.New("tag not found")
}

func TestResolveTagNames_HappyPath(t *testing.T) {
	tagAlpha := &types.KnowledgeTag{ID: "tag-1", Name: "alpha"}
	tagBeta := &types.KnowledgeTag{ID: "tag-2", Name: "beta"}
	svc := &knowledgeService{
		tagRepo: &fakeTagRepoForResolve{
			byName: map[string]*types.KnowledgeTag{
				"alpha": tagAlpha,
				"beta":  tagBeta,
			},
		},
	}

	ids, err := svc.ResolveTagNames(context.Background(), 1, "kb-1", []string{"alpha", "beta"})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"tag-1", "tag-2"}, ids)
}

func TestResolveTagNames_SkipsUnknown(t *testing.T) {
	tagAlpha := &types.KnowledgeTag{ID: "tag-1", Name: "alpha"}
	svc := &knowledgeService{
		tagRepo: &fakeTagRepoForResolve{
			byName: map[string]*types.KnowledgeTag{
				"alpha": tagAlpha,
			},
		},
	}

	// "unknown" does not exist — it should be silently skipped.
	ids, err := svc.ResolveTagNames(context.Background(), 1, "kb-1", []string{"alpha", "unknown"})
	require.NoError(t, err)
	assert.Equal(t, []string{"tag-1"}, ids)
}

func TestResolveTagNames_EmptyInput(t *testing.T) {
	svc := &knowledgeService{
		tagRepo: &fakeTagRepoForResolve{},
	}

	ids, err := svc.ResolveTagNames(context.Background(), 1, "kb-1", []string{})
	require.NoError(t, err)
	assert.Nil(t, ids)
}

func TestResolveTagNames_AllUnknown(t *testing.T) {
	svc := &knowledgeService{
		tagRepo: &fakeTagRepoForResolve{byName: map[string]*types.KnowledgeTag{}},
	}

	ids, err := svc.ResolveTagNames(context.Background(), 1, "kb-1", []string{"nope1", "nope2"})
	require.NoError(t, err)
	assert.Empty(t, ids)
}

// TestResolveFolderNames_Delegation verifies the service delegates to the repo.
func TestResolveFolderNames_Delegation(t *testing.T) {
	called := false
	repo := &fakeKnowledgeRepoForResolve{
		resolveFolderNamesFn: func(_ context.Context, _ uint64, _ string, names []string) ([]string, error) {
			called = true
			return []string{"resolved-id"}, nil
		},
	}
	svc := &knowledgeService{repo: repo}

	ids, err := svc.ResolveFolderNames(context.Background(), 1, "kb-1", []string{"folder1"})
	require.NoError(t, err)
	assert.Equal(t, []string{"resolved-id"}, ids)
	assert.True(t, called, "service must delegate to repo.ResolveFolderNames")
}

// fakeKnowledgeRepoForResolve embeds the interface and overrides only
// ResolveFolderNames.
type fakeKnowledgeRepoForResolve struct {
	interfaces.KnowledgeRepository
	resolveFolderNamesFn func(ctx context.Context, tenantID uint64, kbID string, names []string) ([]string, error)
}

func (r *fakeKnowledgeRepoForResolve) ResolveFolderNames(ctx context.Context, tenantID uint64, kbID string, names []string) ([]string, error) {
	return r.resolveFolderNamesFn(ctx, tenantID, kbID, names)
}
