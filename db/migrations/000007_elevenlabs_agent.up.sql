ALTER TABLE agent_configs
    ADD COLUMN IF NOT EXISTS elevenlabs_agent_id TEXT NOT NULL DEFAULT '';

ALTER TABLE agent_configs
    DROP CONSTRAINT IF EXISTS agent_configs_mode_check;

ALTER TABLE agent_configs
    ADD CONSTRAINT agent_configs_mode_check
    CHECK (mode IN ('realtime', 'cascaded', 'elevenlabs_agent'));
