DO $$ BEGIN
    IF to_regclass('public.embeddings') IS NOT NULL THEN
        ALTER TABLE embeddings DROP COLUMN IF EXISTS folder_id;
        RAISE NOTICE '[Migration 000071] Dropped folder_id from embeddings';
    ELSE
        RAISE NOTICE '[Migration 000071] embeddings table absent, skipping';
    END IF;
END $$;
