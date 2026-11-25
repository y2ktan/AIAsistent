package api

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"aegis-voice/server/internal/mcp"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestHostedMCPPersistenceMigration(t *testing.T) {
	dsn := os.Getenv("HOSTED_MCP_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires an isolated empty PostgreSQL database")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	oldSQL, err := os.ReadFile(filepath.Join("..", "..", "..", "db", "migrations", "000001_init.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(oldSQL)); err != nil {
		t.Fatal(err)
	}
	var existingID string
	if err := pool.QueryRow(ctx, `INSERT INTO mcp_servers(name, endpoint_url) VALUES ('existing', 'https://8.8.8.8/mcp') RETURNING id::text`).Scan(&existingID); err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile(filepath.Join("..", "..", "..", "db", "migrations", "000009_hosted_mcp.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(migration)); err != nil {
		t.Fatal(err)
	}
	store := NewPGAdminStore(pool)
	existing, err := store.GetMCPServer(ctx, existingID)
	if err != nil {
		t.Fatal(err)
	}
	if existing.HostedMode != "disabled" || existing.HostedSyncStatus != "disabled" || existing.HostedCreationAttempted || len(existing.HostedProxyCredential) != 0 {
		t.Fatal("migration exposed an existing MCP server")
	}
	rec := &MCPServerRecord{Name: "new", Transport: "streamable_http", EndpointURL: "https://8.8.8.8/mcp", Enabled: true, TimeoutMs: 10000, DiscoveredTools: []mcp.ToolDef{}}
	if _, err := store.UpsertMCPServer(ctx, rec); err != nil {
		t.Fatal(err)
	}
	saved, err := store.GetMCPServer(ctx, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.HostedMode != "disabled" || saved.HostedSyncStatus != "disabled" {
		t.Fatal("new default is not disabled")
	}
	rec.HostedMode = "proxy"
	rec.HostedSyncStatus = "recovery_pending"
	rec.HostedProxyCredential = []byte("encrypted-proxy")
	rec.HostedCreationIntentID = "intent_fixture"
	rec.HostedCreationConfig = []byte("encrypted-intent")
	rec.HostedCreationAttempted = true
	rec.HostedResourceID = "mcp_fixture"
	rec.HostedAgentID = "agent_fixture"
	rec.HostedSyncError = "Recovery required."
	if err := store.WithHostedMCPLock(ctx, func(locked AdminStore) error {
		if _, err := locked.UpsertMCPServer(ctx, rec); err != nil {
			return err
		}
		// A different connection must see durable intent before the lock is released.
		visible, err := store.GetMCPServer(ctx, rec.ID)
		if err != nil {
			return err
		}
		if !visible.HostedCreationAttempted || visible.HostedCreationIntentID != "intent_fixture" {
			t.Fatal("creation intent was not committed before POST")
		}
		if err := store.WithHostedMCPLock(ctx, func(AdminStore) error { t.Fatal("concurrent session acquired the reconciliation lock"); return nil }); err == nil {
			t.Fatal("lock did not exclude another instance")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	records, err := store.ListMCPServers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range records {
		if r.ID != rec.ID {
			continue
		}
		found = true
		if r.HostedResourceID != rec.HostedResourceID || r.HostedAgentID != rec.HostedAgentID || string(r.HostedCreationConfig) != "encrypted-intent" || string(r.HostedProxyCredential) != "encrypted-proxy" {
			t.Fatal("hosted fields failed to round trip")
		}
		raw, _ := json.Marshal(r)
		if len(raw) == 0 {
			t.Fatal("could not serialize record")
		}
	}
	if !found {
		t.Fatal("saved record missing from list")
	}
	rec.HostedMode = "disabled"
	rec.HostedSyncStatus = "disabled"
	rec.HostedResourceID = ""
	rec.HostedAgentID = ""
	rec.HostedCreationIntentID = ""
	rec.HostedCreationAttempted = false
	rec.HostedCreationConfig = nil
	rec.HostedProxyCredential = nil
	if _, err := store.UpsertMCPServer(ctx, rec); err != nil {
		t.Fatal(err)
	}
	saved, err = store.GetMCPServer(ctx, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.HostedProxyCredential) != 0 || saved.HostedCreationAttempted {
		t.Fatal("revocation failed to clear persisted credentials")
	}
}

func TestHostedSyncOptionsMigrationDefaults(t *testing.T) {
	dsn := os.Getenv("HOSTED_MCP_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires an isolated empty PostgreSQL database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	migrations, err := filepath.Glob(filepath.Join("..", "..", "..", "db", "migrations", "*.up.sql"))
	if err != nil || len(migrations) == 0 {
		t.Fatalf("could not find persistence migrations: %v", err)
	}
	for _, path := range migrations {
		if filepath.Base(path) == "000010_hosted_sync_options.up.sql" {
			continue
		}
		migration, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(migration)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO agent_configs (name, mode) VALUES ('upgraded_fixture', 'cascaded')`); err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile(filepath.Join("..", "..", "..", "db", "migrations", "000010_hosted_sync_options.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(migration)); err != nil {
		t.Fatal(err)
	}
	var ragEnabled, skillsEnabled bool
	if err := pool.QueryRow(ctx, `
		SELECT elevenlabs_rag_sync_enabled, elevenlabs_agent_skills_sync_enabled
		FROM agent_configs WHERE name = 'upgraded_fixture'
	`).Scan(&ragEnabled, &skillsEnabled); err != nil {
		t.Fatal(err)
	}
	if !ragEnabled || !skillsEnabled {
		t.Fatal("existing agent configuration did not retain enabled hosted synchronization defaults")
	}
	store := NewPGAdminStore(pool)
	config, err := store.GetAgentConfig(ctx)
	if err != nil || !config.ElevenLabsRAGSyncEnabled || !config.ElevenLabsSkillsSyncEnabled {
		t.Fatalf("zero-row configuration did not default both policies to enabled: %#v err=%v", config, err)
	}
	for _, ragEnabled := range []bool{true, false} {
		for _, skillsEnabled := range []bool{true, false} {
			config.ElevenLabsRAGSyncEnabled = ragEnabled
			config.ElevenLabsSkillsSyncEnabled = skillsEnabled
			if err := store.UpsertAgentConfig(ctx, config); err != nil {
				t.Fatal(err)
			}
			saved, err := store.GetAgentConfig(ctx)
			if err != nil || saved.ElevenLabsRAGSyncEnabled != ragEnabled || saved.ElevenLabsSkillsSyncEnabled != skillsEnabled {
				t.Fatalf("policy round trip failed: %#v err=%v", saved, err)
			}
			config = saved
		}
	}
	if _, err := pool.Exec(ctx, string(migration)); err != nil {
		t.Fatal(err)
	}
	saved, err := store.GetAgentConfig(ctx)
	if err != nil || saved.ElevenLabsRAGSyncEnabled || saved.ElevenLabsSkillsSyncEnabled {
		t.Fatalf("repeated migration changed explicit false values: %#v err=%v", saved, err)
	}
}
