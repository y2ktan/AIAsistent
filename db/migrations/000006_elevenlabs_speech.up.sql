ALTER TABLE agent_configs
    ADD COLUMN IF NOT EXISTS speech_provider TEXT NOT NULL DEFAULT 'local',
    ADD COLUMN IF NOT EXISTS elevenlabs_stt_model TEXT NOT NULL DEFAULT 'scribe_v3',
    ADD COLUMN IF NOT EXISTS elevenlabs_tts_model TEXT NOT NULL DEFAULT 'eleven_flash_v2_5',
    ADD COLUMN IF NOT EXISTS elevenlabs_api_key_ciphertext BYTEA,
    ADD COLUMN IF NOT EXISTS elevenlabs_api_key_last5 TEXT NOT NULL DEFAULT '';

ALTER TABLE voice_languages
    ADD COLUMN IF NOT EXISTS elevenlabs_voice TEXT NOT NULL DEFAULT '';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'agent_configs_speech_provider_check'
    ) THEN
        ALTER TABLE agent_configs
            ADD CONSTRAINT agent_configs_speech_provider_check
            CHECK (speech_provider IN ('local', 'elevenlabs'));
    END IF;
END $$;
