ALTER TABLE agent_configs
    ADD COLUMN IF NOT EXISTS elevenlabs_rag_sync_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN IF NOT EXISTS elevenlabs_agent_skills_sync_enabled BOOLEAN NOT NULL DEFAULT TRUE;
