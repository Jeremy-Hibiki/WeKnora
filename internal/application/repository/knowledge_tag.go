package repository

import (
	"context"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SetKnowledgeTags replaces all tags for a single knowledge entry.
// It deletes existing relations and inserts new ones in a transaction.
func (r *knowledgeRepository) SetKnowledgeTags(
	ctx context.Context,
	knowledgeID string,
	tagIDs []string,
) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Delete all existing tag relations for this knowledge
		if err := tx.Where("knowledge_id = ?", knowledgeID).
			Delete(&types.KnowledgeTagRelation{}).Error; err != nil {
			return err
		}
		// Insert new relations (skip empty and duplicate IDs)
		if len(tagIDs) == 0 {
			return nil
		}
		seen := make(map[string]struct{}, len(tagIDs))
		now := time.Now()
		relations := make([]types.KnowledgeTagRelation, 0, len(tagIDs))
		for _, tagID := range tagIDs {
			if tagID == "" {
				continue
			}
			if _, dup := seen[tagID]; dup {
				continue
			}
			seen[tagID] = struct{}{}
			relations = append(relations, types.KnowledgeTagRelation{
				KnowledgeID: knowledgeID,
				TagID:       tagID,
				CreatedAt:   now,
			})
		}
		if len(relations) == 0 {
			return nil
		}
		return tx.Create(&relations).Error
	})
}

// GetKnowledgeTags returns tags for multiple knowledge IDs.
// The result is a map from knowledge ID to its tag list.
func (r *knowledgeRepository) GetKnowledgeTags(
	ctx context.Context,
	knowledgeIDs []string,
) (map[string][]*types.KnowledgeTag, error) {
	result := make(map[string][]*types.KnowledgeTag)
	if len(knowledgeIDs) == 0 {
		return result, nil
	}

	// Query relations and join with knowledge_tags to get full tag info
	type relationWithTag struct {
		KnowledgeID string `gorm:"column:knowledge_id"`
		types.KnowledgeTag
	}
	var rows []relationWithTag
	if err := r.db.WithContext(ctx).
		Table("knowledge_tag_relations AS ktr").
		Select("ktr.knowledge_id, kt.id, kt.seq_id, kt.tenant_id, kt.knowledge_base_id, kt.name, kt.color, kt.sort_order, kt.created_at, kt.updated_at").
		Joins("JOIN knowledge_tags AS kt ON ktr.tag_id = kt.id").
		Where("ktr.knowledge_id IN (?)", knowledgeIDs).
		Find(&rows).Error; err != nil {
		return nil, err
	}

	for _, row := range rows {
		tag := row.KnowledgeTag
		result[row.KnowledgeID] = append(result[row.KnowledgeID], &tag)
	}
	return result, nil
}

// DeleteKnowledgeTagRelations deletes all tag relations for a knowledge entry.
func (r *knowledgeRepository) DeleteKnowledgeTagRelations(
	ctx context.Context,
	knowledgeID string,
) error {
	return r.db.WithContext(ctx).
		Where("knowledge_id = ?", knowledgeID).
		Delete(&types.KnowledgeTagRelation{}).Error
}

// AddTagToKnowledgeBatch adds tag relations for multiple knowledge entries.
// Uses ON CONFLICT DO NOTHING so entries that already carry the tag are silently skipped.
func (r *knowledgeRepository) AddTagToKnowledgeBatch(
	ctx context.Context,
	knowledgeIDs []string,
	tagIDs []string,
) error {
	if len(knowledgeIDs) == 0 || len(tagIDs) == 0 {
		return nil
	}
	// Dedup both inputs. ON CONFLICT DO NOTHING only protects against rows
	// already in the table, not duplicates within the same INSERT statement.
	// PostgreSQL raises "ON CONFLICT DO NOTHING command cannot affect row a
	// second time" if the same (knowledge_id, tag_id) pair appears twice in
	// the batch. tagIDs come from request JSON and may contain dupes.
	seenKid := make(map[string]struct{}, len(knowledgeIDs))
	uniqueKids := make([]string, 0, len(knowledgeIDs))
	for _, kid := range knowledgeIDs {
		if _, dup := seenKid[kid]; dup {
			continue
		}
		seenKid[kid] = struct{}{}
		uniqueKids = append(uniqueKids, kid)
	}
	seenTid := make(map[string]struct{}, len(tagIDs))
	uniqueTids := make([]string, 0, len(tagIDs))
	for _, tid := range tagIDs {
		if tid == "" {
			continue
		}
		if _, dup := seenTid[tid]; dup {
			continue
		}
		seenTid[tid] = struct{}{}
		uniqueTids = append(uniqueTids, tid)
	}
	if len(uniqueKids) == 0 || len(uniqueTids) == 0 {
		return nil
	}
	now := time.Now()
	relations := make([]types.KnowledgeTagRelation, 0, len(uniqueKids)*len(uniqueTids))
	for _, kid := range uniqueKids {
		for _, tid := range uniqueTids {
			relations = append(relations, types.KnowledgeTagRelation{
				KnowledgeID: kid,
				TagID:       tid,
				CreatedAt:   now,
			})
		}
	}
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		CreateInBatches(relations, 500).Error
}

// RemoveTagFromKnowledgeBatch removes specific tags from multiple knowledge entries.
// Only deletes associations matching both a tag ID in tagIDs and a knowledge ID in knowledgeIDs.
func (r *knowledgeRepository) RemoveTagFromKnowledgeBatch(
	ctx context.Context,
	knowledgeIDs []string,
	tagIDs []string,
) error {
	if len(knowledgeIDs) == 0 || len(tagIDs) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).
		Where("knowledge_id IN ? AND tag_id IN ?", knowledgeIDs, tagIDs).
		Delete(&types.KnowledgeTagRelation{}).Error
}
