-- Migration 000071: Add folder_id to embeddings table
-- Only applies when pgvector is the retrieve driver (embeddings table exists).
-- Non-postgres engines (Milvus, Qdrant, etc.) skip this — folder_id is managed
-- in the vector store's own schema via code (ensureCollectionFields / etc.).
DO $$ BEGIN
    IF to_regclass('public.embeddings') IS NOT NULL THEN
        ALTER TABLE embeddings ADD COLUMN IF NOT EXISTS folder_id VARCHAR(36) DEFAULT '';
        RAISE NOTICE '[Migration 000071] Added folder_id to embeddings';
    ELSE
        RAISE NOTICE '[Migration 000071] embeddings table absent (non-postgres driver), skipping';
    END IF;
END $$;
