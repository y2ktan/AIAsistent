ALTER TABLE agent_configs
    DROP COLUMN IF EXISTS elevenlabs_agent_skills_sync_enabled,
    DROP COLUMN IF EXISTS elevenlabs_rag_sync_enabled;
