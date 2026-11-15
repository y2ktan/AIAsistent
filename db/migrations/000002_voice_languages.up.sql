CREATE TABLE IF NOT EXISTS voice_languages (
    code TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    voice TEXT NOT NULL DEFAULT '',
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    position INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_voice_languages_single_default
    ON voice_languages (is_default)
    WHERE is_default = TRUE;
