ALTER TABLE documents
    ADD COLUMN IF NOT EXISTS extracted_text TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS elevenlabs_doc_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS elevenlabs_synced_hash TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS elevenlabs_agent_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS elevenlabs_sync_status TEXT NOT NULL DEFAULT 'not_synced',
    ADD COLUMN IF NOT EXISTS elevenlabs_sync_error TEXT NOT NULL DEFAULT '';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'documents_elevenlabs_sync_status_check'
    ) THEN
        ALTER TABLE documents
            ADD CONSTRAINT documents_elevenlabs_sync_status_check
            CHECK (elevenlabs_sync_status IN ('not_synced', 'indexing', 'synced', 'failed'));
    END IF;
END $$;
