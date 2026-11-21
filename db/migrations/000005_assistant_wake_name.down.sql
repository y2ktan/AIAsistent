ALTER TABLE agent_configs
DROP COLUMN IF EXISTS wake_aliases,
DROP COLUMN IF EXISTS assistant_name;
