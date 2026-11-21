"use client";

import { useEffect, useState, FormEvent, DragEvent } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import {
  Users,
  FileText,
  Bot,
  Plug,
  BookOpen,
  ChevronDown,
  ChevronUp,
  ShieldAlert,
  ArrowLeft,
  LogOut,
  Upload,
  Trash2,
  CheckCircle2,
  AlertCircle,
  Loader2,
  RefreshCw,
} from "lucide-react";
import { apiFetch, fetchSession, AuthUser } from "@/lib/api-client";
import {
  addLanguage,
  availableCatalogLanguages,
  canRemoveLanguage,
  includeSavedOption,
  partitionVoicesByLanguage,
  removeLanguage,
  type VoiceLanguageSettings,
} from "@/lib/voice-languages";
import {
  formatSkillMetadata,
  getSkillToolReadiness,
  type AgentSkill,
} from "@/lib/agent-skills";

type Tab = "users" | "rag" | "agent" | "mcp" | "skills" | "audit";

interface DocRecord {
  id: string;
  title: string;
  filename: string;
  sizeBytes: number;
  status: "processing" | "ready" | "failed";
  errorMessage?: string;
  chunkCount: number;
  createdAt: string;
}

interface AgentConfig {
  mode: "cascaded" | "realtime";
  assistantName: string;
  wakeAliases: string[];
  realtimeWsUrl: string;
  realtimeModel: string;
  sttBaseUrl: string;
  sttModel: string;
  llmBaseUrl: string;
  llmModel: string;
  ttsBaseUrl: string;
  ttsModel: string;
  ttsVoice: string;
  embeddingBaseUrl: string;
  embeddingModel: string;
  embeddingDim: number;
  systemInstructions: string;
  apiKeyPreview: string;
  ragTopK: number;
  ragMinSimilarity: number;
  audioFocusEnabled: boolean;
}

function parseWakeAliasesInput(raw: string): string[] {
  return raw
    .split(/[,，、\n]+/)
    .map((item) => item.trim())
    .filter(Boolean);
}

interface TTSVoiceDiscovery {
  voices: string[];
  supported: boolean;
}

interface STTModelDiscovery {
  models: string[];
  supported: boolean;
}

interface MCPServer {
  id: string;
  name: string;
  transport: "streamable_http" | "sse";
  endpointUrl: string;
  authHeaderPreview: string;
  enabled: boolean;
  allowedTools: string[];
  discoveredTools: { name: string; description?: string }[];
  timeoutMs: number;
}

interface AuditLog {
  id: number;
  actorEmail: string;
  action: string;
  targetType: string;
  targetId: string;
  ipAddress: string;
  outcome: string;
  createdAt: string;
}

export default function AdminPage() {
  const router = useRouter();
  const [tab, setTab] = useState<Tab>("users");
  const [me, setMe] = useState<AuthUser | null>(null);
  const [notice, setNotice] = useState<{ type: "ok" | "err"; text: string } | null>(null);

  // Users state
  const [users, setUsers] = useState<AuthUser[]>([]);
  const [newEmail, setNewEmail] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [newRole, setNewRole] = useState<"user" | "admin">("user");

  // RAG state
  const [docs, setDocs] = useState<DocRecord[]>([]);
  const [uploading, setUploading] = useState(false);

  // Agent config state
  const [cfg, setCfg] = useState<AgentConfig>({
    mode: "cascaded",
    assistantName: "Aegis",
    wakeAliases: [],
    realtimeWsUrl: "",
    realtimeModel: "",
    sttBaseUrl: "",
    sttModel: "whisper-1",
    llmBaseUrl: "",
    llmModel: "gpt-4o",
    ttsBaseUrl: "",
    ttsModel: "tts-1",
    ttsVoice: "alloy",
    embeddingBaseUrl: "",
    embeddingModel: "text-embedding-3-small",
    embeddingDim: 1536,
    systemInstructions: "",
    apiKeyPreview: "",
    ragTopK: 5,
    ragMinSimilarity: 0.35,
    audioFocusEnabled: true,
  });
  const [wakeAliasesInput, setWakeAliasesInput] = useState("");
  const [newApiKey, setNewApiKey] = useState("");
  const [testingConn, setTestingConn] = useState(false);
  const [ttsVoiceDiscovery, setTTSVoiceDiscovery] = useState<TTSVoiceDiscovery>({
    voices: [],
    supported: false,
  });
  const [sttModelDiscovery, setSTTModelDiscovery] = useState<STTModelDiscovery>({
    models: [],
    supported: false,
  });

  // Voice languages state
  const [voiceLangs, setVoiceLangs] = useState<VoiceLanguageSettings>({
    languages: [],
    defaultLanguage: "en",
    saved: false,
    catalog: [],
  });
  const [selectedAddLang, setSelectedAddLang] = useState("");
  const [savingVoiceLangs, setSavingVoiceLangs] = useState(false);

  // MCP state
  const [mcpServers, setMcpServers] = useState<MCPServer[]>([]);
  const [mcpName, setMcpName] = useState("");
  const [mcpUrl, setMcpUrl] = useState("");
  const [mcpAuth, setMcpAuth] = useState("");
  const [mcpTransport, setMcpTransport] = useState<"streamable_http" | "sse">("streamable_http");
  const [hiddenMCPToolServers, setHiddenMCPToolServers] = useState<Set<string>>(
    () => new Set()
  );

  // Agent Skills state
  const [agentSkills, setAgentSkills] = useState<AgentSkill[]>([]);
  const [uploadingSkill, setUploadingSkill] = useState(false);
  const [skillDropActive, setSkillDropActive] = useState(false);
  const [expandedSkills, setExpandedSkills] = useState<Set<string>>(() => new Set());

  // Audit state
  const [logs, setLogs] = useState<AuditLog[]>([]);

  useEffect(() => {
    fetchSession().then((res) => {
      if (!res) {
        router.push("/login");
        return;
      }
      if (res.user.role !== "admin") {
        router.push("/");
        return;
      }
      setMe(res.user);
      loadAll();
    });
  }, [router]);

  async function loadAll() {
    try {
      const [uRes, dRes, cRes, vlRes, mRes, skillsRes, aRes, voicesRes, modelsRes] = await Promise.all([
        apiFetch<{ users: AuthUser[] }>("/api/v1/admin/users"),
        apiFetch<{ documents: DocRecord[] }>("/api/v1/admin/documents"),
        apiFetch<{ config: AgentConfig }>("/api/v1/admin/agent-config"),
        apiFetch<VoiceLanguageSettings>("/api/v1/admin/voice-languages"),
        apiFetch<{ servers: MCPServer[] }>("/api/v1/admin/mcp-servers"),
        apiFetch<{ skills: AgentSkill[] }>("/api/v1/admin/skills"),
        apiFetch<{ logs: AuditLog[] }>("/api/v1/admin/audit-logs"),
        apiFetch<TTSVoiceDiscovery>("/api/v1/admin/tts-voices"),
        apiFetch<STTModelDiscovery>("/api/v1/admin/stt-models"),
      ]);
      setUsers(uRes.users || []);
      setDocs(dRes.documents || []);
      if (cRes.config) {
        const loadedAssistantName =
          typeof cRes.config.assistantName === "string" && cRes.config.assistantName.trim() !== ""
            ? cRes.config.assistantName
            : "Aegis";
        const loadedAliases = Array.isArray(cRes.config.wakeAliases) ? cRes.config.wakeAliases : [];
        setCfg((prev) => ({
          ...prev,
          ...cRes.config,
          assistantName: loadedAssistantName,
          wakeAliases: loadedAliases,
        }));
        setWakeAliasesInput(loadedAliases.join(", "));
      }
      if (vlRes) {
        setVoiceLangs(vlRes);
        const avail = availableCatalogLanguages(vlRes.languages || [], vlRes.catalog || []);
        setSelectedAddLang(avail[0]?.code || "");
      }
      setMcpServers(mRes.servers || []);
      setAgentSkills(skillsRes.skills || []);
      setLogs(aRes.logs || []);
      setTTSVoiceDiscovery(voicesRes);
      setSTTModelDiscovery(modelsRes);
    } catch (err) {
      setNotice({ type: "err", text: err instanceof Error ? err.message : "Failed to load admin data" });
    }
  }

  async function handleLogout() {
    try {
      await apiFetch("/api/v1/auth/logout", { method: "POST" });
    } finally {
      router.push("/login");
    }
  }

  async function handleCreateUser(e: FormEvent) {
    e.preventDefault();
    setNotice(null);
    try {
      await apiFetch("/api/v1/admin/users", {
        method: "POST",
        body: JSON.stringify({ email: newEmail, password: newPassword, role: newRole }),
      });
      setNewEmail("");
      setNewPassword("");
      setNotice({ type: "ok", text: "User account created." });
      await loadAll();
    } catch (err) {
      setNotice({ type: "err", text: err instanceof Error ? err.message : "Create user failed" });
    }
  }

  async function handleToggleUser(u: AuthUser, role: "user" | "admin", isActive: boolean) {
    setNotice(null);
    try {
      await apiFetch(`/api/v1/admin/users/${u.id}`, {
        method: "PATCH",
        body: JSON.stringify({ role, isActive }),
      });
      setNotice({ type: "ok", text: `Updated ${u.email}.` });
      await loadAll();
    } catch (err) {
      setNotice({ type: "err", text: err instanceof Error ? err.message : "Update failed" });
    }
  }

  async function uploadFiles(files: FileList | null) {
    if (!files || files.length === 0) return;
    setUploading(true);
    setNotice(null);
    try {
      for (const file of Array.from(files)) {
        const fd = new FormData();
        fd.append("file", file);
        fd.append("title", file.name);
        await apiFetch("/api/v1/admin/documents", { method: "POST", body: fd });
      }
      setNotice({ type: "ok", text: "Document(s) uploaded and indexed." });
      await loadAll();
    } catch (err) {
      setNotice({ type: "err", text: err instanceof Error ? err.message : "Upload failed" });
    } finally {
      setUploading(false);
    }
  }

  async function uploadAgentSkill(file: File | null) {
    if (!file) return;
    setUploadingSkill(true);
    setNotice(null);
    try {
      const formData = new FormData();
      formData.append("file", file);
      await apiFetch("/api/v1/admin/skills", { method: "POST", body: formData });
      setNotice({ type: "ok", text: `Agent Skill "${file.name}" uploaded or updated.` });
      await loadAll();
    } catch (err) {
      setNotice({ type: "err", text: err instanceof Error ? err.message : "Agent Skill upload failed" });
    } finally {
      setUploadingSkill(false);
    }
  }

  async function handleToggleAgentSkill(skill: AgentSkill) {
    setNotice(null);
    try {
      await apiFetch(`/api/v1/admin/skills/${skill.id}`, {
        method: "PATCH",
        body: JSON.stringify({ enabled: !skill.enabled }),
      });
      setNotice({ type: "ok", text: `${skill.name} ${skill.enabled ? "disabled" : "enabled"}.` });
      await loadAll();
    } catch (err) {
      setNotice({ type: "err", text: err instanceof Error ? err.message : "Failed to update Agent Skill" });
    }
  }

  async function handleDeleteAgentSkill(skill: AgentSkill) {
    if (!window.confirm(`Delete the "${skill.name}" Agent Skill?`)) return;
    setNotice(null);
    try {
      await apiFetch(`/api/v1/admin/skills/${skill.id}`, { method: "DELETE" });
      setNotice({ type: "ok", text: `${skill.name} deleted.` });
      await loadAll();
    } catch (err) {
      setNotice({ type: "err", text: err instanceof Error ? err.message : "Failed to delete Agent Skill" });
    }
  }

  async function handleDeleteDoc(id: string) {
    setNotice(null);
    try {
      await apiFetch(`/api/v1/admin/documents/${id}`, { method: "DELETE" });
      setNotice({ type: "ok", text: "Document deleted." });
      await loadAll();
    } catch (err) {
      setNotice({ type: "err", text: err instanceof Error ? err.message : "Delete failed" });
    }
  }

  async function handleSaveConfig(e: FormEvent) {
    e.preventDefault();
    setNotice(null);
    try {
      const payload = {
        mode: cfg.mode,
        assistantName: cfg.assistantName,
        wakeAliases: parseWakeAliasesInput(wakeAliasesInput),
        realtimeWsUrl: cfg.realtimeWsUrl,
        realtimeModel: cfg.realtimeModel,
        sttBaseUrl: cfg.sttBaseUrl,
        sttModel: cfg.sttModel,
        llmBaseUrl: cfg.llmBaseUrl,
        llmModel: cfg.llmModel,
        ttsBaseUrl: cfg.ttsBaseUrl,
        ttsModel: cfg.ttsModel,
        ttsVoice: cfg.ttsVoice,
        embeddingBaseUrl: cfg.embeddingBaseUrl,
        embeddingModel: cfg.embeddingModel,
        embeddingDim: Number(cfg.embeddingDim) || 1536,
        systemInstructions: cfg.systemInstructions,
        apiKey: newApiKey,
        ragTopK: Number(cfg.ragTopK) || 5,
        ragMinSimilarity: Number(cfg.ragMinSimilarity) || 0.35,
        audioFocusEnabled: cfg.audioFocusEnabled !== false,
      };
      const res = await apiFetch<{ config: AgentConfig }>("/api/v1/admin/agent-config", {
        method: "PUT",
        body: JSON.stringify(payload),
      });
      if (res?.config) {
        const savedAliases = Array.isArray(res.config.wakeAliases) ? res.config.wakeAliases : [];
        setCfg((prev) => ({
          ...prev,
          ...res.config,
          assistantName: res.config.assistantName || "Aegis",
          wakeAliases: savedAliases,
        }));
        setWakeAliasesInput(savedAliases.join(", "));
      }
      setNewApiKey("");
      setNotice({ type: "ok", text: "AI Agent configuration saved and encrypted." });
      try {
        const [voicesRes, modelsRes] = await Promise.all([
          apiFetch<TTSVoiceDiscovery>("/api/v1/admin/tts-voices"),
          apiFetch<STTModelDiscovery>("/api/v1/admin/stt-models"),
        ]);
        setTTSVoiceDiscovery(voicesRes);
        setSTTModelDiscovery(modelsRes);
      } catch (err) {
        setNotice({
          type: "err",
          text: `Configuration saved, but provider options could not be refreshed: ${
            err instanceof Error ? err.message : "request failed"
          }`,
        });
      }
    } catch (err) {
      setNotice({ type: "err", text: err instanceof Error ? err.message : "Save failed" });
    }
  }

  async function handleTestConnection() {
    setTestingConn(true);
    setNotice(null);
    try {
      const res = await apiFetch<{ ok: boolean; message?: string; statusCode?: number }>(
        "/api/v1/admin/agent-config/test",
        { method: "POST" }
      );
      setNotice({
        type: res.ok ? "ok" : "err",
        text: res.ok
          ? `Connection verified (status ${res.statusCode ?? 200}).`
          : res.message || "Connection check failed.",
      });
    } catch (err) {
      setNotice({ type: "err", text: err instanceof Error ? err.message : "Connection test failed" });
    } finally {
      setTestingConn(false);
    }
  }

  async function handleSaveVoiceLanguages() {
    setSavingVoiceLangs(true);
    setNotice(null);
    try {
      const res = await apiFetch<VoiceLanguageSettings>("/api/v1/admin/voice-languages", {
        method: "PUT",
        body: JSON.stringify({
          languages: voiceLangs.languages,
          defaultLanguage: voiceLangs.defaultLanguage,
        }),
      });
      setVoiceLangs(res);
      const avail = availableCatalogLanguages(res.languages || [], res.catalog || []);
      setSelectedAddLang(avail[0]?.code || "");
      setNotice({ type: "ok", text: "Voice languages saved." });
    } catch (err) {
      setNotice({ type: "err", text: err instanceof Error ? err.message : "Failed to save voice languages" });
    } finally {
      setSavingVoiceLangs(false);
    }
  }

  async function handleAddMCP(e: FormEvent) {
    e.preventDefault();
    setNotice(null);
    try {
      await apiFetch("/api/v1/admin/mcp-servers", {
        method: "POST",
        body: JSON.stringify({
          name: mcpName,
          transport: mcpTransport,
          endpointUrl: mcpUrl,
          authHeader: mcpAuth,
          enabled: true,
          allowedTools: [],
          timeoutMs: 10000,
        }),
      });
      setMcpName("");
      setMcpUrl("");
      setMcpAuth("");
      setNotice({ type: "ok", text: "MCP server registered." });
      await loadAll();
    } catch (err) {
      setNotice({ type: "err", text: err instanceof Error ? err.message : "Failed to register MCP server" });
    }
  }

  async function handleDiscoverMCP(id: string) {
    setNotice(null);
    try {
      await apiFetch(`/api/v1/admin/mcp-servers/${id}/discover`, { method: "POST" });
      setNotice({ type: "ok", text: "Discovered MCP tools." });
      await loadAll();
    } catch (err) {
      setNotice({ type: "err", text: err instanceof Error ? err.message : "MCP discovery failed" });
    }
  }

  async function handleToggleMCPTool(srv: MCPServer, toolName: string) {
    const exists = srv.allowedTools?.includes(toolName);
    const updated = exists
      ? srv.allowedTools.filter((t) => t !== toolName)
      : [...(srv.allowedTools || []), toolName];
    try {
      await apiFetch(`/api/v1/admin/mcp-servers/${srv.id}`, {
        method: "PUT",
        body: JSON.stringify({
          name: srv.name,
          transport: srv.transport,
          endpointUrl: srv.endpointUrl,
          enabled: srv.enabled,
          allowedTools: updated,
          timeoutMs: srv.timeoutMs,
        }),
      });
      await loadAll();
    } catch (err) {
      setNotice({ type: "err", text: err instanceof Error ? err.message : "Failed to update tool allowlist" });
    }
  }

  function handleToggleMCPToolsVisibility(serverId: string) {
    setHiddenMCPToolServers((current) => {
      const next = new Set(current);
      if (next.has(serverId)) {
        next.delete(serverId);
      } else {
        next.add(serverId);
      }
      return next;
    });
  }

  async function handleToggleMCPServer(srv: MCPServer) {
    setNotice(null);
    try {
      await apiFetch(`/api/v1/admin/mcp-servers/${srv.id}`, {
        method: "PUT",
        body: JSON.stringify({
          name: srv.name,
          transport: srv.transport,
          endpointUrl: srv.endpointUrl,
          enabled: !srv.enabled,
          allowedTools: srv.allowedTools || [],
          timeoutMs: srv.timeoutMs,
        }),
      });
      setNotice({ type: "ok", text: `${srv.name} ${srv.enabled ? "disabled" : "enabled"}.` });
      await loadAll();
    } catch (err) {
      setNotice({ type: "err", text: err instanceof Error ? err.message : "Failed to update MCP server" });
    }
  }

  async function handleDeleteMCP(id: string) {
    try {
      await apiFetch(`/api/v1/admin/mcp-servers/${id}`, { method: "DELETE" });
      await loadAll();
    } catch (err) {
      setNotice({ type: "err", text: err instanceof Error ? err.message : "Failed to delete MCP server" });
    }
  }

  if (!me) {
    return (
      <main className="flex min-h-screen items-center justify-center bg-[#05070F] text-slate-400">
        <Loader2 className="h-6 w-6 animate-spin" />
      </main>
    );
  }

  const tabs: { id: Tab; label: string; icon: typeof Users }[] = [
    { id: "users", label: "Users & Roles", icon: Users },
    { id: "rag", label: "RAG Knowledge", icon: FileText },
    { id: "agent", label: "AI Agent", icon: Bot },
    { id: "mcp", label: "MCP Servers", icon: Plug },
    { id: "skills", label: "Agent Skills", icon: BookOpen },
    { id: "audit", label: "Audit Logs", icon: ShieldAlert },
  ];
  const sttModelOptions = includeSavedOption(sttModelDiscovery.models, cfg.sttModel);

  return (
    <div className="min-h-screen bg-[#05070F] text-slate-100">
      <header className="border-b border-slate-800/80 bg-slate-950/70 backdrop-blur-xl">
        <div className="mx-auto flex max-w-6xl items-center justify-between px-6 py-4">
          <div className="flex items-center gap-4">
            <Link
              href="/"
              className="flex items-center gap-1.5 rounded-lg border border-slate-700/60 bg-slate-900/70 px-3 py-1.5 text-xs font-medium text-slate-300 hover:border-cyan-500/40 hover:text-white"
            >
              <ArrowLeft className="h-3.5 w-3.5" />
              Assistant
            </Link>
            <div>
              <h1 className="text-lg font-semibold text-white">Governance Console</h1>
              <p className="text-xs text-slate-400">{me.email} • Administrator</p>
            </div>
          </div>
          <button
            onClick={handleLogout}
            className="flex items-center gap-1.5 rounded-lg border border-slate-700/60 px-3 py-1.5 text-xs text-slate-300 hover:bg-slate-800"
          >
            <LogOut className="h-3.5 w-3.5" />
            Sign Out
          </button>
        </div>
      </header>

      <div className="mx-auto max-w-6xl px-6 py-8">
        <nav className="mb-6 flex flex-wrap gap-2">
          {tabs.map((t) => {
            const Icon = t.icon;
            const active = tab === t.id;
            return (
              <button
                key={t.id}
                onClick={() => setTab(t.id)}
                className={`flex items-center gap-2 rounded-xl px-4 py-2 text-sm font-medium transition ${
                  active
                    ? "border border-cyan-400/40 bg-cyan-500/15 text-cyan-200"
                    : "border border-slate-800 bg-slate-900/50 text-slate-400 hover:text-slate-200"
                }`}
              >
                <Icon className="h-4 w-4" />
                {t.label}
              </button>
            );
          })}
        </nav>

        {notice && (
          <div
            className={`mb-6 flex items-center gap-2.5 rounded-xl border px-4 py-3 text-sm ${
              notice.type === "ok"
                ? "border-emerald-500/30 bg-emerald-500/10 text-emerald-200"
                : "border-rose-500/30 bg-rose-500/10 text-rose-200"
            }`}
          >
            {notice.type === "ok" ? (
              <CheckCircle2 className="h-4 w-4 shrink-0 text-emerald-400" />
            ) : (
              <AlertCircle className="h-4 w-4 shrink-0 text-rose-400" />
            )}
            <span>{notice.text}</span>
          </div>
        )}

        {tab === "users" && (
          <div className="space-y-6">
            <form
              onSubmit={handleCreateUser}
              autoComplete="off"
              className="grid gap-4 rounded-2xl border border-slate-800 bg-slate-900/50 p-5 md:grid-cols-4"
            >
              <input
                type="email"
                name="new-user-email"
                autoComplete="off"
                required
                value={newEmail}
                onChange={(e) => setNewEmail(e.target.value)}
                placeholder="user@organization.com"
                className="rounded-xl border border-slate-700/60 bg-slate-950/70 px-3 py-2 text-sm text-white"
              />
              <input
                type="password"
                name="new-user-password"
                autoComplete="new-password"
                required
                minLength={12}
                value={newPassword}
                onChange={(e) => setNewPassword(e.target.value)}
                placeholder="Password (min 12 chars)"
                className="rounded-xl border border-slate-700/60 bg-slate-950/70 px-3 py-2 text-sm text-white"
              />
              <select
                value={newRole}
                onChange={(e) => setNewRole(e.target.value as "user" | "admin")}
                className="rounded-xl border border-slate-700/60 bg-slate-950/70 px-3 py-2 text-sm text-white"
              >
                <option value="user">User</option>
                <option value="admin">Admin</option>
              </select>
              <button
                type="submit"
                className="rounded-xl bg-cyan-600 px-4 py-2 text-sm font-medium text-white hover:bg-cyan-500"
              >
                Add User
              </button>
            </form>

            <div className="overflow-hidden rounded-2xl border border-slate-800 bg-slate-900/40">
              <table className="w-full text-left text-sm">
                <thead className="border-b border-slate-800 bg-slate-950/60 text-xs uppercase text-slate-400">
                  <tr>
                    <th className="px-4 py-3">Email</th>
                    <th className="px-4 py-3">Role</th>
                    <th className="px-4 py-3">Status</th>
                    <th className="px-4 py-3 text-right">Actions</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-800/60">
                  {users.map((u) => (
                    <tr key={u.id}>
                      <td className="px-4 py-3 font-medium text-white">{u.email}</td>
                      <td className="px-4 py-3">
                        <select
                          value={u.role}
                          onChange={(e) =>
                            handleToggleUser(u, e.target.value as "user" | "admin", u.isActive)
                          }
                          className="rounded-lg border border-slate-700 bg-slate-950 px-2 py-1 text-xs"
                        >
                          <option value="user">user</option>
                          <option value="admin">admin</option>
                        </select>
                      </td>
                      <td className="px-4 py-3">
                        <span
                          className={`rounded-full px-2.5 py-0.5 text-xs ${
                            u.isActive
                              ? "bg-emerald-500/15 text-emerald-300"
                              : "bg-rose-500/15 text-rose-300"
                          }`}
                        >
                          {u.isActive ? "Active" : "Disabled"}
                        </span>
                      </td>
                      <td className="px-4 py-3 text-right">
                        <button
                          onClick={() => handleToggleUser(u, u.role, !u.isActive)}
                          className="rounded-lg border border-slate-700 px-2.5 py-1 text-xs text-slate-300 hover:bg-slate-800"
                        >
                          {u.isActive ? "Deactivate" : "Activate"}
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}

        {tab === "rag" && (
          <div className="space-y-6">
            <label
              onDragOver={(e: DragEvent) => e.preventDefault()}
              onDrop={(e: DragEvent) => {
                e.preventDefault();
                uploadFiles(e.dataTransfer.files);
              }}
              className="flex cursor-pointer flex-col items-center justify-center rounded-2xl border-2 border-dashed border-slate-700/70 bg-slate-900/40 p-8 text-center transition hover:border-cyan-400/50"
            >
              <Upload className="mb-2 h-7 w-7 text-cyan-400" />
              <p className="text-sm font-medium text-white">
                {uploading ? "Uploading & Indexing..." : "Drop PDF, DOCX, Markdown, or TXT files here"}
              </p>
              <p className="mt-1 text-xs text-slate-400">Max 20 MB per file • Magic-byte validated</p>
              <input
                type="file"
                multiple
                accept=".pdf,.docx,.md,.markdown,.txt"
                onChange={(e) => uploadFiles(e.target.files)}
                className="hidden"
              />
            </label>

            <div className="overflow-hidden rounded-2xl border border-slate-800 bg-slate-900/40">
              <table className="w-full text-left text-sm">
                <thead className="border-b border-slate-800 bg-slate-950/60 text-xs uppercase text-slate-400">
                  <tr>
                    <th className="px-4 py-3">Document</th>
                    <th className="px-4 py-3">Status</th>
                    <th className="px-4 py-3">Chunks</th>
                    <th className="px-4 py-3 text-right">Actions</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-800/60">
                  {docs.map((d) => (
                    <tr key={d.id}>
                      <td className="px-4 py-3">
                        <div className="font-medium text-white">{d.title}</div>
                        <div className="text-xs text-slate-400">{d.filename}</div>
                      </td>
                      <td className="px-4 py-3">
                        <span
                          className={`rounded-full px-2.5 py-0.5 text-xs ${
                            d.status === "ready"
                              ? "bg-emerald-500/15 text-emerald-300"
                              : d.status === "failed"
                              ? "bg-rose-500/15 text-rose-300"
                              : "bg-amber-500/15 text-amber-300"
                          }`}
                        >
                          {d.status}
                        </span>
                        {d.errorMessage && (
                          <p className="mt-1 text-xs text-rose-300">{d.errorMessage}</p>
                        )}
                      </td>
                      <td className="px-4 py-3 text-slate-300">{d.chunkCount}</td>
                      <td className="px-4 py-3 text-right">
                        <button
                          onClick={() => handleDeleteDoc(d.id)}
                          aria-label={`Delete ${d.title}`}
                          className="rounded-lg border border-rose-500/30 p-1.5 text-rose-300 hover:bg-rose-500/15"
                        >
                          <Trash2 className="h-4 w-4" />
                        </button>
                      </td>
                    </tr>
                  ))}
                  {docs.length === 0 && (
                    <tr>
                      <td colSpan={4} className="px-4 py-8 text-center text-slate-500">
                        No documents indexed yet.
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          </div>
        )}

        {tab === "agent" && (
          <div className="space-y-6">
          <form
            onSubmit={handleSaveConfig}
            className="space-y-5 rounded-2xl border border-slate-800 bg-slate-900/50 p-6"
          >
            <div className="grid gap-4 md:grid-cols-2">
              <div>
                <label
                  htmlFor="assistant-name"
                  className="mb-1 block text-xs uppercase text-slate-400"
                >
                  Assistant Name
                </label>
                <input
                  id="assistant-name"
                  name="assistant-name"
                  type="text"
                  autoComplete="off"
                  value={cfg.assistantName}
                  onChange={(e) => setCfg({ ...cfg, assistantName: e.target.value })}
                  placeholder="Aegis / 小智"
                  className="w-full rounded-xl border border-slate-700/60 bg-slate-950/70 px-3 py-2 text-sm text-white"
                />
                <p className="mt-1 text-[11px] text-slate-400">
                  Primary wake name required at the start of voice turns (English or Chinese, e.g. Aegis, Jarvis, 小智).
                </p>
              </div>
              <div>
                <label
                  htmlFor="wake-aliases"
                  className="mb-1 block text-xs uppercase text-slate-400"
                >
                  Wake Aliases
                </label>
                <input
                  id="wake-aliases"
                  name="wake-aliases"
                  type="text"
                  autoComplete="off"
                  value={wakeAliasesInput}
                  onChange={(e) => setWakeAliasesInput(e.target.value)}
                  placeholder="Optional comma-separated aliases, e.g. 小智, Aegis"
                  className="w-full rounded-xl border border-slate-700/60 bg-slate-950/70 px-3 py-2 text-sm text-white"
                />
                <p className="mt-1 text-[11px] text-slate-400">
                  Optional alternate wake names in English or Chinese separated by commas.
                </p>
              </div>

              <div>
                <label className="mb-1 block text-xs uppercase text-slate-400">Pipeline Mode</label>
                <select
                  value={cfg.mode}
                  onChange={(e) => setCfg({ ...cfg, mode: e.target.value as "cascaded" | "realtime" })}
                  className="w-full rounded-xl border border-slate-700/60 bg-slate-950/70 px-3 py-2 text-sm text-white"
                >
                  <option value="cascaded">Cascaded (STT → Vision/RAG LLM → TTS)</option>
                  <option value="realtime">Realtime Multimodal WebSocket</option>
                </select>
              </div>
              <div>
                <label className="mb-1 block text-xs uppercase text-slate-400">
                  Provider API Key (Write-Only {cfg.apiKeyPreview ? `• Current: ${cfg.apiKeyPreview}` : ""})
                </label>
                <input
                  type="password"
                  value={newApiKey}
                  onChange={(e) => setNewApiKey(e.target.value)}
                  placeholder={cfg.apiKeyPreview || "Enter new API key (leave blank to keep existing)"}
                  className="w-full rounded-xl border border-slate-700/60 bg-slate-950/70 px-3 py-2 text-sm text-white"
                />
              </div>

              {cfg.mode === "realtime" ? (
                <>
                  <div>
                    <label className="mb-1 block text-xs uppercase text-slate-400">Realtime WebSocket URL</label>
                    <input
                      type="url"
                      value={cfg.realtimeWsUrl}
                      onChange={(e) => setCfg({ ...cfg, realtimeWsUrl: e.target.value })}
                      placeholder="wss://api.openai.com/v1/realtime?model=gpt-4o-realtime-preview"
                      className="w-full rounded-xl border border-slate-700/60 bg-slate-950/70 px-3 py-2 text-sm text-white"
                    />
                  </div>
                  <div>
                    <label className="mb-1 block text-xs uppercase text-slate-400">Realtime Model</label>
                    <input
                      type="text"
                      value={cfg.realtimeModel}
                      onChange={(e) => setCfg({ ...cfg, realtimeModel: e.target.value })}
                      className="w-full rounded-xl border border-slate-700/60 bg-slate-950/70 px-3 py-2 text-sm text-white"
                    />
                  </div>
                </>
              ) : (
                <>
                  <div>
                    <label className="mb-1 block text-xs uppercase text-slate-400">LLM Base URL</label>
                    <input
                      type="url"
                      value={cfg.llmBaseUrl}
                      onChange={(e) => setCfg({ ...cfg, llmBaseUrl: e.target.value })}
                      placeholder="https://api.openai.com/v1"
                      className="w-full rounded-xl border border-slate-700/60 bg-slate-950/70 px-3 py-2 text-sm text-white"
                    />
                  </div>
                  <div>
                    <label className="mb-1 block text-xs uppercase text-slate-400">LLM Model</label>
                    <input
                      type="text"
                      value={cfg.llmModel}
                      onChange={(e) => setCfg({ ...cfg, llmModel: e.target.value })}
                      className="w-full rounded-xl border border-slate-700/60 bg-slate-950/70 px-3 py-2 text-sm text-white"
                    />
                  </div>
                  <div>
                    <label className="mb-1 block text-xs uppercase text-slate-400">STT Base URL</label>
                    <input
                      type="url"
                      value={cfg.sttBaseUrl}
                      onChange={(e) => setCfg({ ...cfg, sttBaseUrl: e.target.value })}
                      placeholder="https://api.openai.com/v1"
                      className="w-full rounded-xl border border-slate-700/60 bg-slate-950/70 px-3 py-2 text-sm text-white"
                    />
                  </div>
                  <div>
                    <label className="mb-1 block text-xs uppercase text-slate-400">STT Model</label>
                    {sttModelDiscovery.supported && sttModelDiscovery.models.length > 0 ? (
                      <select
                        value={cfg.sttModel}
                        onChange={(e) => setCfg({ ...cfg, sttModel: e.target.value })}
                        className="w-full rounded-xl border border-slate-700/60 bg-slate-950/70 px-3 py-2 text-sm text-white"
                      >
                        {sttModelOptions.map((model) => (
                          <option key={model} value={model}>
                            {model}
                          </option>
                        ))}
                      </select>
                    ) : (
                      <input
                        type="text"
                        value={cfg.sttModel}
                        onChange={(e) => setCfg({ ...cfg, sttModel: e.target.value })}
                        placeholder="whisper-1"
                        className="w-full rounded-xl border border-slate-700/60 bg-slate-950/70 px-3 py-2 text-sm text-white"
                      />
                    )}
                  </div>
                  <div>
                    <label className="mb-1 block text-xs uppercase text-slate-400">TTS Base URL</label>
                    <input
                      type="url"
                      value={cfg.ttsBaseUrl}
                      onChange={(e) => setCfg({ ...cfg, ttsBaseUrl: e.target.value })}
                      placeholder="https://api.openai.com/v1"
                      className="w-full rounded-xl border border-slate-700/60 bg-slate-950/70 px-3 py-2 text-sm text-white"
                    />
                  </div>
                </>
              )}

              <div>
                <label className="mb-1 block text-xs uppercase text-slate-400">Embedding Base URL</label>
                <input
                  type="url"
                  value={cfg.embeddingBaseUrl}
                  onChange={(e) => setCfg({ ...cfg, embeddingBaseUrl: e.target.value })}
                  placeholder="https://api.openai.com/v1"
                  className="w-full rounded-xl border border-slate-700/60 bg-slate-950/70 px-3 py-2 text-sm text-white"
                />
              </div>
              <div>
                <label className="mb-1 block text-xs uppercase text-slate-400">Embedding Model</label>
                <input
                  type="text"
                  value={cfg.embeddingModel}
                  onChange={(e) => setCfg({ ...cfg, embeddingModel: e.target.value })}
                  className="w-full rounded-xl border border-slate-700/60 bg-slate-950/70 px-3 py-2 text-sm text-white"
                />
              </div>
            </div>

            <div>
              <label className="mb-1 block text-xs uppercase text-slate-400">System Instructions</label>
              <textarea
                rows={3}
                value={cfg.systemInstructions}
                onChange={(e) => setCfg({ ...cfg, systemInstructions: e.target.value })}
                className="w-full rounded-xl border border-slate-700/60 bg-slate-950/70 px-3 py-2 text-sm text-white"
              />
            </div>

            <label className="flex cursor-pointer items-start gap-3 rounded-xl border border-slate-700/60 bg-slate-950/50 p-4">
              <input
                type="checkbox"
                checked={cfg.audioFocusEnabled !== false}
                onChange={(e) => setCfg({ ...cfg, audioFocusEnabled: e.target.checked })}
                className="mt-0.5"
              />
              <span>
                <span className="block text-sm font-medium text-white">Audio Focus</span>
                <span className="mt-1 block text-xs leading-relaxed text-slate-400">
                  Isolate the near-field speaker and filter ambient environmental noise before
                  WebRTC audio transmission. Enabled by default and used by new voice sessions.
                </span>
              </span>
            </label>

            <div className="flex flex-wrap gap-3">
              <button
                type="submit"
                className="rounded-xl bg-cyan-600 px-5 py-2 text-sm font-medium text-white hover:bg-cyan-500"
              >
                Save Encrypted Config
              </button>
              <button
                type="button"
                onClick={handleTestConnection}
                disabled={testingConn}
                className="flex items-center gap-2 rounded-xl border border-slate-700 px-4 py-2 text-sm text-slate-200 hover:bg-slate-800"
              >
                {testingConn && <Loader2 className="h-4 w-4 animate-spin" />}
                Test Connection
              </button>
            </div>
          </form>

          <div className="space-y-4 rounded-2xl border border-slate-800 bg-slate-900/50 p-6">
            <div>
              <h2 className="text-base font-semibold text-white">Voice languages</h2>
              <p className="mt-1 text-xs text-slate-400">
                Choose which spoken languages the assistant handles, assign a speech voice per language, and pick one default language for fallback replies.
              </p>
            </div>

            <div className="space-y-2">
              {voiceLangs.languages.map((lang) => {
                const isDefault = lang.code === voiceLangs.defaultLanguage;
                const removable = canRemoveLanguage(lang.code, voiceLangs.defaultLanguage);
                const voiceGroups = partitionVoicesByLanguage(ttsVoiceDiscovery.voices, lang.code);
                const listedVoices = includeSavedOption(
                  [...voiceGroups.recommended, ...voiceGroups.others],
                  lang.voice
                );
                const hasUnlistedSavedVoice =
                  lang.voice !== "" && !ttsVoiceDiscovery.voices.includes(lang.voice);
                return (
                  <div
                    key={lang.code}
                    className="flex flex-wrap items-center gap-3 rounded-xl border border-slate-800/80 bg-slate-950/60 px-3 py-2.5"
                  >
                    <div className="min-w-[160px]">
                      <span className="text-sm font-medium text-white">{lang.name}</span>
                      <span className="ml-2 rounded bg-slate-800 px-1.5 py-0.5 text-[11px] uppercase text-slate-400">
                        {lang.code}
                      </span>
                    </div>

                    <div className="flex-1 min-w-[180px]">
                      {ttsVoiceDiscovery.supported && ttsVoiceDiscovery.voices.length > 0 ? (
                        <select
                          value={lang.voice}
                          onChange={(e) => {
                            const voice = e.target.value;
                            setVoiceLangs((prev) => ({
                              ...prev,
                              languages: prev.languages.map((item) =>
                                item.code === lang.code ? { ...item, voice } : item
                              ),
                            }));
                          }}
                          aria-label={`Voice for ${lang.name}`}
                          className="w-full rounded-lg border border-slate-700/60 bg-slate-900 px-2.5 py-1.5 text-sm text-white"
                        >
                          <option value="">None (fallback to default)</option>
                          {hasUnlistedSavedVoice && (
                            <option value={lang.voice}>Saved custom voice: {lang.voice}</option>
                          )}
                          {voiceGroups.recommended.length > 0 && (
                            <optgroup label="Recommended">
                              {voiceGroups.recommended.map((voice) => (
                                <option key={voice} value={voice}>
                                  {voice}
                                </option>
                              ))}
                            </optgroup>
                          )}
                          <optgroup label="All voices">
                            {listedVoices
                              .filter(
                                (voice) =>
                                  !voiceGroups.recommended.includes(voice) &&
                                  !(hasUnlistedSavedVoice && voice === lang.voice)
                              )
                              .map((voice) => (
                                <option key={voice} value={voice}>
                                  {voice}
                                </option>
                              ))}
                          </optgroup>
                        </select>
                      ) : (
                        <input
                          type="text"
                          value={lang.voice}
                          onChange={(e) => {
                            const voice = e.target.value;
                            setVoiceLangs((prev) => ({
                              ...prev,
                              languages: prev.languages.map((item) =>
                                item.code === lang.code ? { ...item, voice } : item
                              ),
                            }));
                          }}
                          placeholder="Voice name (e.g. af_heart, zf_xiaoxiao)"
                          aria-label={`Voice for ${lang.name}`}
                          className="w-full rounded-lg border border-slate-700/60 bg-slate-900 px-2.5 py-1.5 text-sm text-white"
                        />
                      )}
                      {lang.voice.trim() === "" && (
                        <p className="mt-1 text-[11px] text-amber-400/90">
                          No voice assigned — replies will be spoken in the default language.
                        </p>
                      )}
                    </div>

                    <label className="flex items-center gap-1.5 text-xs text-slate-300">
                      <input
                        type="radio"
                        name="defaultVoiceLanguage"
                        checked={isDefault}
                        onChange={() =>
                          setVoiceLangs((prev) => ({ ...prev, defaultLanguage: lang.code }))
                        }
                      />
                      Default
                    </label>

                    <button
                      type="button"
                      disabled={!removable}
                      title={
                        removable
                          ? `Remove ${lang.name}`
                          : "Choose another default language before removing this one"
                      }
                      onClick={() => {
                        setVoiceLangs((prev) => {
                          const nextList = removeLanguage(prev.languages, prev.defaultLanguage, lang.code);
                          const avail = availableCatalogLanguages(nextList, prev.catalog);
                          if (!selectedAddLang && avail[0]) setSelectedAddLang(avail[0].code);
                          return { ...prev, languages: nextList };
                        });
                      }}
                      className="rounded-lg border border-slate-700 px-2.5 py-1 text-xs text-slate-300 hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-40"
                    >
                      Remove
                    </button>
                  </div>
                );
              })}
            </div>

            <div className="flex flex-wrap items-center gap-2 pt-1">
              <select
                value={selectedAddLang}
                onChange={(e) => setSelectedAddLang(e.target.value)}
                aria-label="Language to add"
                className="rounded-xl border border-slate-700/60 bg-slate-950/70 px-3 py-2 text-sm text-white"
              >
                {availableCatalogLanguages(voiceLangs.languages, voiceLangs.catalog).map((item) => (
                  <option key={item.code} value={item.code}>
                    {item.name} ({item.code})
                  </option>
                ))}
              </select>
              <button
                type="button"
                disabled={!selectedAddLang}
                onClick={() => {
                  if (!selectedAddLang) return;
                  setVoiceLangs((prev) => {
                    const nextList = addLanguage(prev.languages, prev.catalog, selectedAddLang);
                    const avail = availableCatalogLanguages(nextList, prev.catalog);
                    setSelectedAddLang(avail[0]?.code || "");
                    return { ...prev, languages: nextList };
                  });
                }}
                className="rounded-xl border border-slate-700 px-4 py-2 text-sm text-slate-200 hover:bg-slate-800 disabled:opacity-40"
              >
                Add Language
              </button>
              <button
                type="button"
                disabled={savingVoiceLangs}
                onClick={handleSaveVoiceLanguages}
                className="ml-auto flex items-center gap-2 rounded-xl bg-cyan-600 px-5 py-2 text-sm font-medium text-white hover:bg-cyan-500 disabled:opacity-60"
              >
                {savingVoiceLangs && <Loader2 className="h-4 w-4 animate-spin" />}
                Save Voice Languages
              </button>
            </div>
          </div>
          </div>
        )}

        {tab === "mcp" && (
          <div className="space-y-6">
            <form
              onSubmit={handleAddMCP}
              autoComplete="off"
              className="grid gap-3 rounded-2xl border border-slate-800 bg-slate-900/50 p-5 md:grid-cols-5"
            >
              <input
                type="text"
                name="mcp-server-name"
                autoComplete="off"
                required
                value={mcpName}
                onChange={(e) => setMcpName(e.target.value)}
                placeholder="Server Name"
                className="rounded-xl border border-slate-700/60 bg-slate-950/70 px-3 py-2 text-sm text-white"
              />
              <select
                value={mcpTransport}
                onChange={(e) => setMcpTransport(e.target.value as "streamable_http" | "sse")}
                className="rounded-xl border border-slate-700/60 bg-slate-950/70 px-3 py-2 text-sm text-white"
              >
                <option value="streamable_http">Streamable HTTP</option>
                <option value="sse">SSE</option>
              </select>
              <input
                type="url"
                name="mcp-server-url"
                autoComplete="off"
                required
                value={mcpUrl}
                onChange={(e) => setMcpUrl(e.target.value)}
                placeholder="https://mcp.example.com/mcp"
                className="rounded-xl border border-slate-700/60 bg-slate-950/70 px-3 py-2 text-sm text-white"
              />
              <input
                type="password"
                name="mcp-bearer-token"
                autoComplete="new-password"
                value={mcpAuth}
                onChange={(e) => setMcpAuth(e.target.value)}
                placeholder="Bearer token (optional)"
                className="rounded-xl border border-slate-700/60 bg-slate-950/70 px-3 py-2 text-sm text-white"
              />
              <button
                type="submit"
                className="rounded-xl bg-cyan-600 px-4 py-2 text-sm font-medium text-white hover:bg-cyan-500"
              >
                Register MCP
              </button>
            </form>

            <div className="space-y-4">
              {mcpServers.map((s) => {
                const toolsHidden = hiddenMCPToolServers.has(s.id);
                const toolsPanelId = `mcp-tools-${s.id}`;
                return (
                  <div
                    key={s.id}
                    className="rounded-2xl border border-slate-800 bg-slate-900/40 p-5"
                  >
                  <div className="flex flex-wrap items-center justify-between gap-3">
                    <div>
                      <h3 className="font-medium text-white">{s.name}</h3>
                      <p className="text-xs text-slate-400">
                        {s.transport} • {s.endpointUrl}
                      </p>
                    </div>
                    <div className="flex gap-2">
                      <button
                        type="button"
                        onClick={() => handleToggleMCPServer(s)}
                        className={`rounded-lg border px-3 py-1.5 text-xs ${
                          s.enabled
                            ? "border-amber-500/30 text-amber-200 hover:bg-amber-500/10"
                            : "border-emerald-500/30 text-emerald-200 hover:bg-emerald-500/10"
                        }`}
                      >
                        {s.enabled ? "Disable Server" : "Enable Server"}
                      </button>
                      <button
                        type="button"
                        onClick={() => handleDiscoverMCP(s.id)}
                        className="flex items-center gap-1.5 rounded-lg border border-cyan-500/40 px-3 py-1.5 text-xs text-cyan-200 hover:bg-cyan-500/15"
                      >
                        <RefreshCw className="h-3.5 w-3.5" />
                        Discover Tools
                      </button>
                      {s.discoveredTools && s.discoveredTools.length > 0 && (
                        <button
                          type="button"
                          onClick={() => handleToggleMCPToolsVisibility(s.id)}
                          aria-expanded={!toolsHidden}
                          aria-controls={toolsPanelId}
                          className="rounded-lg border border-slate-600/60 px-3 py-1.5 text-xs text-slate-200 hover:bg-slate-700/30"
                        >
                          {toolsHidden ? "Show Tools" : "Hide Tools"}
                        </button>
                      )}
                      <button
                        type="button"
                        onClick={() => handleDeleteMCP(s.id)}
                        className="rounded-lg border border-rose-500/30 px-3 py-1.5 text-xs text-rose-300 hover:bg-rose-500/15"
                      >
                        Delete
                      </button>
                    </div>
                  </div>
                  {s.discoveredTools && s.discoveredTools.length > 0 && !toolsHidden && (
                    <div id={toolsPanelId} className="mt-4 flex flex-wrap gap-2">
                      {s.discoveredTools.map((tool) => {
                        const checked = s.allowedTools?.includes(tool.name);
                        return (
                          <label
                            key={tool.name}
                            className="flex cursor-pointer items-start gap-2 rounded-lg border border-slate-700/70 bg-slate-950/60 px-3 py-2 text-xs"
                          >
                            <input
                              type="checkbox"
                              checked={checked}
                              onChange={() => handleToggleMCPTool(s, tool.name)}
                              className="mt-0.5"
                            />
                            <span>
                              <span className="block font-medium text-slate-200">{tool.name}</span>
                              <span className="mt-0.5 block max-w-sm text-slate-400">
                                {tool.description || "No description provided."}
                              </span>
                            </span>
                          </label>
                        );
                      })}
                    </div>
                  )}
                  </div>
                );
              })}
            </div>
          </div>
        )}

        {tab === "skills" && (
          <div className="space-y-6">
            <label
              onDragEnter={(e: DragEvent<HTMLLabelElement>) => {
                e.preventDefault();
                setSkillDropActive(true);
              }}
              onDragOver={(e: DragEvent<HTMLLabelElement>) => e.preventDefault()}
              onDragLeave={() => setSkillDropActive(false)}
              onDrop={(e: DragEvent<HTMLLabelElement>) => {
                e.preventDefault();
                setSkillDropActive(false);
                void uploadAgentSkill(e.dataTransfer.files[0] || null);
              }}
              className={`flex cursor-pointer flex-col items-center justify-center rounded-2xl border-2 border-dashed bg-slate-900/40 p-8 text-center transition ${
                skillDropActive
                  ? "border-cyan-400 bg-cyan-500/10"
                  : "border-slate-700/70 hover:border-cyan-400/50"
              }`}
            >
              {uploadingSkill ? (
                <Loader2 className="mb-2 h-7 w-7 animate-spin text-cyan-400" />
              ) : (
                <Upload className="mb-2 h-7 w-7 text-cyan-400" />
              )}
              <p className="text-sm font-medium text-white">
                {uploadingSkill ? "Uploading Agent Skill..." : "Drop a SKILL.md or ZIP bundle here"}
              </p>
              <p className="mt-1 text-xs text-slate-400">
                Supports .md, .markdown, and .zip • Maximum upload size 2 MB
              </p>
              <input
                type="file"
                accept=".md,.markdown,.zip"
                disabled={uploadingSkill}
                onChange={(e) => {
                  void uploadAgentSkill(e.target.files?.[0] || null);
                  e.currentTarget.value = "";
                }}
                className="hidden"
              />
            </label>

            {agentSkills.length === 0 ? (
              <div className="rounded-2xl border border-slate-800 bg-slate-900/40 p-8 text-center text-sm text-slate-400">
                No Agent Skills uploaded yet.
              </div>
            ) : (
              <div className="space-y-4">
                {agentSkills.map((skill) => {
                  const metadata = formatSkillMetadata(skill);
                  const toolReadiness = getSkillToolReadiness(skill.allowedTools || [], mcpServers);
                  const isExpanded = expandedSkills.has(skill.id);
                  return (
                    <article
                      key={skill.id}
                      className="rounded-2xl border border-slate-800 bg-slate-900/40 p-5"
                    >
                      <div className="flex flex-wrap items-start justify-between gap-4">
                        <div className="min-w-0 flex-1">
                          <div className="flex flex-wrap items-center gap-2">
                            <h3 className="font-medium text-white">{skill.name}</h3>
                            <span
                              className={`rounded-full px-2 py-0.5 text-[10px] ${
                                skill.enabled
                                  ? "bg-emerald-500/15 text-emerald-300"
                                  : "bg-slate-700/70 text-slate-300"
                              }`}
                            >
                              {skill.enabled ? "Enabled" : "Disabled"}
                            </span>
                          </div>
                          <p className="mt-1 text-sm text-slate-300">{skill.description}</p>
                          <p className="mt-2 text-xs text-slate-500">
                            {metadata.sourceLabel} · {metadata.referenceFileLabel} · {metadata.fileSizeLabel} · {skill.filename}
                          </p>
                        </div>
                        <div className="flex shrink-0 gap-2">
                          <button
                            type="button"
                            onClick={() => handleToggleAgentSkill(skill)}
                            className={`rounded-lg border px-3 py-1.5 text-xs ${
                              skill.enabled
                                ? "border-amber-500/30 text-amber-200 hover:bg-amber-500/10"
                                : "border-emerald-500/30 text-emerald-200 hover:bg-emerald-500/10"
                            }`}
                          >
                            {skill.enabled ? "Disable" : "Enable"}
                          </button>
                          <button
                            type="button"
                            onClick={() => handleDeleteAgentSkill(skill)}
                            className="rounded-lg border border-rose-500/30 px-3 py-1.5 text-xs text-rose-300 hover:bg-rose-500/15"
                          >
                            Delete
                          </button>
                        </div>
                      </div>

                      {toolReadiness.length > 0 && (
                        <div className="mt-4 flex flex-wrap gap-2">
                          {toolReadiness.map((tool) => (
                            <span
                              key={tool.toolName}
                              title={tool.serverNames.length > 0 ? `Available on ${tool.serverNames.join(", ")}` : "No enabled MCP server currently allows this tool"}
                              className={`rounded-lg border px-2.5 py-1 text-xs ${
                                tool.status === "ready"
                                  ? "border-emerald-500/30 bg-emerald-500/10 text-emerald-200"
                                  : "border-amber-500/30 bg-amber-500/10 text-amber-200"
                              }`}
                            >
                              {tool.status === "ready" ? "Ready" : "Unconfigured"} · {tool.toolName}
                            </span>
                          ))}
                        </div>
                      )}
                      {toolReadiness.length === 0 && (
                        <p className="mt-4 text-xs text-slate-500">No MCP tools referenced by this skill.</p>
                      )}

                      <button
                        type="button"
                        onClick={() => {
                          setExpandedSkills((current) => {
                            const next = new Set(current);
                            if (next.has(skill.id)) next.delete(skill.id);
                            else next.add(skill.id);
                            return next;
                          });
                        }}
                        aria-expanded={isExpanded}
                        className="mt-4 flex items-center gap-1.5 text-xs font-medium text-cyan-300 hover:text-cyan-200"
                      >
                        {isExpanded ? <ChevronUp className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
                        {isExpanded ? "Hide instructions" : "Preview instructions"}
                      </button>
                      {isExpanded && (
                        <div className="mt-3 space-y-3 rounded-xl border border-slate-800 bg-slate-950/60 p-4">
                          <pre className="whitespace-pre-wrap break-words font-sans text-xs leading-relaxed text-slate-300">
                            {skill.instructions}
                          </pre>
                          {skill.referenceFilePaths.length > 0 && (
                            <div className="border-t border-slate-800 pt-3">
                              <p className="mb-1 text-[10px] font-semibold uppercase tracking-wide text-slate-500">
                                Reference files
                              </p>
                              <ul className="space-y-1 text-xs text-slate-400">
                                {skill.referenceFilePaths.map((referencePath) => (
                                  <li key={referencePath} className="font-mono">{referencePath}</li>
                                ))}
                              </ul>
                            </div>
                          )}
                        </div>
                      )}
                    </article>
                  );
                })}
              </div>
            )}
          </div>
        )}

        {tab === "audit" && (
          <div className="overflow-hidden rounded-2xl border border-slate-800 bg-slate-900/40">
            <table className="w-full text-left text-xs">
              <thead className="border-b border-slate-800 bg-slate-950/60 uppercase text-slate-400">
                <tr>
                  <th className="px-4 py-3">Time</th>
                  <th className="px-4 py-3">Actor</th>
                  <th className="px-4 py-3">Action</th>
                  <th className="px-4 py-3">Outcome</th>
                  <th className="px-4 py-3">IP</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-800/60">
                {logs.map((l) => (
                  <tr key={l.id}>
                    <td className="px-4 py-2.5 text-slate-400">
                      {new Date(l.createdAt).toLocaleString()}
                    </td>
                    <td className="px-4 py-2.5 text-white">{l.actorEmail || "system"}</td>
                    <td className="px-4 py-2.5 font-mono text-cyan-300">{l.action}</td>
                    <td className="px-4 py-2.5">
                      <span
                        className={`rounded px-2 py-0.5 ${
                          l.outcome === "success"
                            ? "bg-emerald-500/15 text-emerald-300"
                            : "bg-rose-500/15 text-rose-300"
                        }`}
                      >
                        {l.outcome}
                      </span>
                    </td>
                    <td className="px-4 py-2.5 text-slate-400">{l.ipAddress}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
}
