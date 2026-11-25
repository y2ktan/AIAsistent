ALTER TABLE mcp_servers
    ADD COLUMN IF NOT EXISTS hosted_mode TEXT NOT NULL DEFAULT 'disabled',
    ADD COLUMN IF NOT EXISTS hosted_resource_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS hosted_agent_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS hosted_sync_status TEXT NOT NULL DEFAULT 'disabled',
    ADD COLUMN IF NOT EXISTS hosted_sync_error TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS hosted_proxy_credential BYTEA,
    ADD COLUMN IF NOT EXISTS hosted_creation_intent_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS hosted_creation_config BYTEA,
    ADD COLUMN IF NOT EXISTS hosted_creation_attempted BOOLEAN NOT NULL DEFAULT FALSE;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'mcp_servers_hosted_mode_check'
          AND conrelid = 'mcp_servers'::regclass
    ) THEN
        ALTER TABLE mcp_servers
            ADD CONSTRAINT mcp_servers_hosted_mode_check
            CHECK (hosted_mode IN ('disabled', 'direct', 'proxy'));
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'mcp_servers_hosted_sync_status_check'
          AND conrelid = 'mcp_servers'::regclass
    ) THEN
        ALTER TABLE mcp_servers
            ADD CONSTRAINT mcp_servers_hosted_sync_status_check
            CHECK (hosted_sync_status IN ('disabled', 'pending', 'synced', 'failed', 'recovery_pending', 'revocation_pending'));
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS mcp_servers_hosted_resource_unique
    ON mcp_servers (hosted_resource_id) WHERE hosted_resource_id <> '';
CREATE UNIQUE INDEX IF NOT EXISTS mcp_servers_hosted_intent_unique
    ON mcp_servers (hosted_creation_intent_id) WHERE hosted_creation_intent_id <> '';
