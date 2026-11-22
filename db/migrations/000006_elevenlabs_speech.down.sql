ALTER TABLE voice_languages DROP COLUMN IF EXISTS elevenlabs_voice;
ALTER TABLE agent_configs
    DROP CONSTRAINT IF EXISTS agent_configs_speech_provider_check,
    DROP COLUMN IF EXISTS elevenlabs_api_key_last5,
    DROP COLUMN IF EXISTS elevenlabs_api_key_ciphertext,
    DROP COLUMN IF EXISTS elevenlabs_tts_model,
    DROP COLUMN IF EXISTS elevenlabs_stt_model,
    DROP COLUMN IF EXISTS speech_provider;
