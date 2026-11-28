import { act, createElement, type ReactNode } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

type TestMCPServer = {
  id: string;
  name: string;
  transport: "streamable_http" | "sse";
  endpointUrl: string;
  authHeaderPreview: string;
  enabled: boolean;
  allowedTools: string[];
  discoveredTools: { name: string; description?: string }[];
  timeoutMs: number;
};

const mcpMocks = vi.hoisted(() => ({
  servers: [] as TestMCPServer[],
  updates: [] as { path: string; options?: RequestInit }[],
  agentConfigSaves: [] as Record<string, unknown>[],
  agentConfig: {} as Record<string, unknown>,
  connectionTests: [] as Record<string, unknown>[],
  connectionError: "",
  saveConfigError: "",
  documents: [] as Record<string, unknown>[],
  hostedSyncCalls: 0,
  hostedSyncError: "",
  documentSyncCalls: [] as string[],
  documentSyncStatus: "synced",
}));

vi.mock("next/navigation", () => {
  const router = { push: vi.fn() };
  return { useRouter: () => router };
});

vi.mock("next/link", () => ({
  default: ({ children }: { children: ReactNode }) => createElement("a", null, children),
}));

vi.mock("@/lib/api-client", () => ({
  fetchSession: async () => ({
    user: { id: "admin", email: "admin@example.com", role: "admin" },
  }),
  apiFetch: async (path: string, options?: RequestInit) => {
    if (path.endsWith("/agent-config/test")) {
      mcpMocks.connectionTests.push(JSON.parse(String(options?.body || "{}")));
      if (mcpMocks.connectionError) throw new Error(mcpMocks.connectionError);
      return { ok: true, message: "Private RAG-only agent verified. Test grounding and playback separately." };
    }
    if (path.endsWith("/users")) return { users: [] };
    if (path.endsWith("/documents")) return { documents: mcpMocks.documents };
    if (path.endsWith("/sync") && path.includes("/documents/")) {
      const docId = path.split("/").at(-2) || "";
      mcpMocks.documentSyncCalls.push(docId);
      const updated = mcpMocks.documents.map((doc) =>
        doc.id === docId ? { ...doc, elevenLabsSyncStatus: mcpMocks.documentSyncStatus } : doc
      );
      mcpMocks.documents = updated;
      return { document: updated.find((doc) => doc.id === docId) };
    }
    if (path.endsWith("/agent-config/sync")) {
      mcpMocks.hostedSyncCalls++;
      if (mcpMocks.hostedSyncError) throw new Error(mcpMocks.hostedSyncError);
      return {};
    }
    if (path.endsWith("/voice-languages")) {
      return {
        languages: [
          {
            code: "en",
            name: "English",
            voice: "af_heart",
            elevenLabsVoice: "",
          },
        ],
        defaultLanguage: "en",
        saved: true,
        catalog: [],
      };
    }
    if (options?.method === "PUT" && path.endsWith("/agent-config")) {
      if (mcpMocks.saveConfigError) throw new Error(mcpMocks.saveConfigError);
      const body = JSON.parse(String(options.body)) as Record<string, unknown>;
      mcpMocks.agentConfigSaves.push(body);
      mcpMocks.agentConfig = { ...mcpMocks.agentConfig, ...body };
      return {
        config: {
          mode: body.mode || "cascaded",
          assistantName: body.assistantName,
          wakeAliases: body.wakeAliases,
          audioFocusEnabled: body.audioFocusEnabled,
          elevenLabsRagSyncEnabled: body.elevenLabsRagSyncEnabled,
          elevenLabsAgentSkillsSyncEnabled: body.elevenLabsAgentSkillsSyncEnabled,
        },
      };
    }
    if (path.endsWith("/agent-config")) return { config: mcpMocks.agentConfig };
    if (options?.method === "PUT" && path.includes("/mcp-servers/")) {
      mcpMocks.updates.push({ path, options });
      const serverId = path.split("/").pop();
      const body = JSON.parse(String(options.body)) as { allowedTools: string[] };
      mcpMocks.servers = mcpMocks.servers.map((server) =>
        server.id === serverId ? { ...server, allowedTools: body.allowedTools } : server
      );
      return {};
    }
    if (path.endsWith("/mcp-servers")) return { servers: mcpMocks.servers };
    if (path.endsWith("/audit-logs")) return { logs: [] };
    if (path.includes("/tts-voices")) return { voices: [], supported: false };
    if (path.endsWith("/stt-models")) return { models: [], supported: false };
    return {};
  },
}));

import AdminPage from "@/app/admin/page";

describe("admin form autofill settings", () => {
  let container: HTMLDivElement;
  let root: ReturnType<typeof createRoot>;

  beforeEach(() => {
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
    mcpMocks.servers = [];
    mcpMocks.updates = [];
    mcpMocks.agentConfigSaves = [];
    mcpMocks.agentConfig = {};
    mcpMocks.connectionTests = [];
    mcpMocks.connectionError = "";
    mcpMocks.saveConfigError = "";
    mcpMocks.documents = [];
    mcpMocks.hostedSyncCalls = 0;
    mcpMocks.hostedSyncError = "";
    mcpMocks.documentSyncCalls = [];
    mcpMocks.documentSyncStatus = "synced";
  });

  afterEach(async () => {
    if (root) {
      await act(async () => root.unmount());
    }
    container?.remove();
    vi.unstubAllGlobals();
  });

  it("renders empty Add User and Register MCP inputs with explicit autocomplete settings", async () => {
    container = document.createElement("div");
    document.body.append(container);
    root = createRoot(container);

    await act(async () => {
      root.render(createElement(AdminPage));
    });

    const userForm = container.querySelector("form");
    expect(userForm?.getAttribute("autocomplete")).toBe("off");

    const userEmail = userForm?.querySelector<HTMLInputElement>('input[name="new-user-email"]');
    const userPassword = userForm?.querySelector<HTMLInputElement>('input[name="new-user-password"]');
    expect(userEmail?.value).toBe("");
    expect(userEmail?.getAttribute("autocomplete")).toBe("off");
    expect(userPassword?.value).toBe("");
    expect(userPassword?.getAttribute("autocomplete")).toBe("new-password");

    const mcpTab = Array.from(container.querySelectorAll("button")).find((button) =>
      button.textContent?.includes("MCP Servers")
    );
    expect(mcpTab).toBeDefined();
    await act(async () => {
      mcpTab?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });

    const mcpForm = container.querySelector("form");
    expect(mcpForm?.getAttribute("autocomplete")).toBe("off");

    const serverName = mcpForm?.querySelector<HTMLInputElement>('input[name="mcp-server-name"]');
    const serverUrl = mcpForm?.querySelector<HTMLInputElement>('input[name="mcp-server-url"]');
    const bearerToken = mcpForm?.querySelector<HTMLInputElement>('input[name="mcp-bearer-token"]');
    expect(serverName?.value).toBe("");
    expect(serverName?.getAttribute("autocomplete")).toBe("off");
    expect(serverUrl?.value).toBe("");
    expect(serverUrl?.getAttribute("autocomplete")).toBe("off");
    expect(bearerToken?.value).toBe("");
    expect(bearerToken?.getAttribute("autocomplete")).toBe("new-password");
  });

  it("explains disabled per-document sync policy", async () => {
    mcpMocks.agentConfig = {
      mode: "elevenlabs_agent",
      elevenLabsRagSyncEnabled: false,
      elevenLabsAgentSkillsSyncEnabled: true,
    };
    mcpMocks.documents = [{
      id: "doc-1",
      title: "Support policy",
      filename: "support.pdf",
      status: "ready",
      chunkCount: 2,
      elevenLabsSyncStatus: "failed",
      elevenLabsSyncError: "Provider unavailable.",
    }];
    container = document.createElement("div");
    document.body.append(container);
    root = createRoot(container);

    await act(async () => root.render(createElement(AdminPage)));
    await act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
    const ragTab = Array.from(container.querySelectorAll("button")).find((button) =>
      button.textContent?.includes("RAG Knowledge")
    );
    await act(async () => ragTab?.click());

    const syncButton = Array.from(container.querySelectorAll("button")).find((button) =>
      button.textContent?.trim() === "Sync / retry"
    );
    expect(syncButton?.disabled).toBe(true);
    expect(container.textContent).toContain("Enable and save in AI Agent settings");
    expect(container.textContent).toContain("Application sync disabled");
    expect(mcpMocks.documentSyncCalls).toHaveLength(0);
  });

  it.each(["synced", "indexing", "failed"] as const)("syncs one ready document and refreshes the %s status", async (status) => {
    mcpMocks.agentConfig = { mode: "elevenlabs_agent", elevenLabsRagSyncEnabled: true };
    mcpMocks.documents = [{
      id: "doc-2",
      title: "Operations policy",
      filename: "operations.pdf",
      status: "ready",
      chunkCount: 1,
      elevenLabsSyncStatus: "not_synced",
    }];
    mcpMocks.documentSyncStatus = status;
    container = document.createElement("div");
    document.body.append(container);
    root = createRoot(container);
    await act(async () => root.render(createElement(AdminPage)));
    await act(async () => new Promise((resolve) => setTimeout(resolve, 0)));
    await act(async () => {
      Array.from(container.querySelectorAll("button"))
        .find((button) => button.textContent?.includes("RAG Knowledge"))
        ?.click();
    });
    const syncButton = Array.from(container.querySelectorAll("button")).find((button) =>
      button.textContent?.trim() === "Sync / retry"
    );
    await act(async () => syncButton?.click());
    expect(mcpMocks.documentSyncCalls).toEqual(["doc-2"]);
    expect(container.textContent).toContain(status);
  });

  it("renders Audio Focus enabled by default in AI Agent settings", async () => {
    container = document.createElement("div");
    document.body.append(container);
    root = createRoot(container);

    await act(async () => {
      root.render(createElement(AdminPage));
    });

    const agentTab = Array.from(container.querySelectorAll("button")).find((button) =>
      button.textContent?.includes("AI Agent")
    );
    await act(async () => {
      agentTab?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });

    expect(container.textContent).toContain("Audio Focus");
    const audioFocusCheckbox = Array.from(container.querySelectorAll<HTMLInputElement>("input[type='checkbox']"))
      .find((input) => input.closest("label")?.textContent?.includes("Audio Focus"));
    expect(audioFocusCheckbox?.checked).toBe(true);

    const assistantNameInput = container.querySelector<HTMLInputElement>(
      'input[name="assistant-name"]'
    );
    const wakeAliasesInput = container.querySelector<HTMLInputElement>(
      'input[name="wake-aliases"]'
    );
    expect(assistantNameInput?.value).toBe("Aegis");
    expect(assistantNameInput?.getAttribute("autocomplete")).toBe("off");
    expect(wakeAliasesInput?.value).toBe("");
    expect(wakeAliasesInput?.getAttribute("autocomplete")).toBe("off");
  });

  it("submits English and Chinese assistantName and wakeAliases from AI Agent settings", async () => {
    container = document.createElement("div");
    document.body.append(container);
    root = createRoot(container);

    await act(async () => {
      root.render(createElement(AdminPage));
    });

    const agentTab = Array.from(container.querySelectorAll("button")).find((button) =>
      button.textContent?.includes("AI Agent")
    );
    await act(async () => {
      agentTab?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });

    const assistantNameInput = container.querySelector<HTMLInputElement>(
      'input[name="assistant-name"]'
    );
    const wakeAliasesInput = container.querySelector<HTMLInputElement>(
      'input[name="wake-aliases"]'
    );
    expect(assistantNameInput).toBeDefined();
    expect(wakeAliasesInput).toBeDefined();

    const setNativeValue = (input: HTMLInputElement, value: string) => {
      const setter = Object.getOwnPropertyDescriptor(
        window.HTMLInputElement.prototype,
        "value"
      )?.set;
      setter?.call(input, value);
      input.dispatchEvent(new Event("input", { bubbles: true }));
    };

    await act(async () => {
      setNativeValue(assistantNameInput!, "Jarvis");
      setNativeValue(wakeAliasesInput!, "小智， Aegis, 阿智");
    });

    const agentForm = assistantNameInput!.closest("form");
    await act(async () => {
      agentForm?.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
    });

    expect(mcpMocks.agentConfigSaves).toHaveLength(1);
    expect(mcpMocks.agentConfigSaves[0]).toMatchObject({
      assistantName: "Jarvis",
      wakeAliases: ["小智", "Aegis", "阿智"],
    });
  });

  it("hides OpenAI-compatible speech settings when ElevenLabs is selected", async () => {
    container = document.createElement("div");
    document.body.append(container);
    root = createRoot(container);

    await act(async () => {
      root.render(createElement(AdminPage));
    });

    const agentTab = Array.from(container.querySelectorAll("button")).find((button) =>
      button.textContent?.includes("AI Agent")
    );
    await act(async () => {
      agentTab?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });

    const providerSelect = Array.from(container.querySelectorAll("select")).find((select) =>
      Array.from(select.options).some((option) => option.text.includes("ElevenLabs STT + TTS"))
    );
    expect(providerSelect).toBeDefined();
    expect(container.textContent).toContain("STT Base URL");
    expect(container.textContent).toContain("TTS Base URL");

    await act(async () => {
      const setter = Object.getOwnPropertyDescriptor(
        window.HTMLSelectElement.prototype,
        "value"
      )?.set;
      setter?.call(providerSelect, "elevenlabs");
      providerSelect?.dispatchEvent(new Event("change", { bubbles: true }));
    });

    expect(container.textContent).not.toContain("STT Base URL");
    expect(container.textContent).not.toContain("TTS Base URL");
    expect(container.textContent).toContain("ElevenLabs API Key");
    expect(container.textContent).toContain("ElevenLabs STT Model");
    expect(container.textContent).toContain("LLM Base URL");
    expect(container.querySelector('[aria-label="ElevenLabs voice for English"]')).not.toBeNull();
    expect(container.querySelector('[aria-label="Voice for English"]')).toBeNull();
    expect(container.textContent).not.toContain("Existing provider voice");
  });

  it("saves hosted mode without a local default voice and preserves hidden settings", async () => {
    mcpMocks.agentConfig = {
      mode: "cascaded", speechProvider: "elevenlabs", elevenLabsAgentId: "agent_fixture",
      elevenLabsApiKeyPreview: "*****cret", llmBaseUrl: "https://llm.example/v1",
      llmModel: "retained-model", systemInstructions: "Retained backend prompt",
    };
    container = document.createElement("div");
    document.body.append(container);
    root = createRoot(container);
    await act(async () => root.render(createElement(AdminPage)));
    const agentTab = Array.from(container.querySelectorAll("button")).find((button) => button.textContent?.includes("AI Agent"));
    await act(async () => agentTab?.click());
    const mode = container.querySelector<HTMLSelectElement>("#pipeline-mode");
    await act(async () => {
      mode!.value = "elevenlabs_agent";
      mode!.dispatchEvent(new Event("change", { bubbles: true }));
    });
    expect(container.querySelector<HTMLInputElement>("#elevenlabs-api-key")?.value).toBe("");
    expect(container.querySelector<HTMLInputElement>("#elevenlabs-agent-id")?.value).toBe("agent_fixture");
    expect(container.querySelector("fieldset")?.disabled).toBe(true);
    const instructions = container.querySelector<HTMLTextAreaElement>("#system-instructions");
    expect(instructions?.disabled).toBe(false);
    expect(instructions?.value).toBe("Retained backend prompt");
    await act(async () => container.querySelector("form")?.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true })));
    expect(mcpMocks.agentConfigSaves[0]).toMatchObject({
      mode: "elevenlabs_agent", elevenLabsAgentId: "agent_fixture", elevenLabsApiKey: "",
      speechProvider: "elevenlabs", llmModel: "retained-model",
      llmBaseUrl: "https://llm.example/v1", systemInstructions: "Retained backend prompt",
    });
    expect(container.textContent).toContain("dashboard owns the LLM");
    expect(container.textContent).toContain("independently optional and enabled by default");
    expect(container.textContent).toContain("supported workflow and server-side tools");
    expect(container.textContent).toContain("cause side effects");
    const ragTab = Array.from(container.querySelectorAll("button")).find((button) => button.textContent?.includes("RAG Knowledge"));
    await act(async () => ragTab?.click());
    expect(container.textContent).toContain("Ready local documents sync additively");
    expect(container.textContent).toContain("dashboard-managed documents are preserved");
  });

  it.each([
    { rag: false, skills: true },
    { rag: true, skills: false },
    { rag: false, skills: false },
  ])("submits independent hosted policies including explicit false: %j", async ({ rag, skills }) => {
    mcpMocks.agentConfig = { mode: "elevenlabs_agent", elevenLabsAgentId: "agent_fixture" };
    container = document.createElement("div");
    document.body.append(container);
    root = createRoot(container);
    await act(async () => root.render(createElement(AdminPage)));
    const agentTab = Array.from(container.querySelectorAll("button")).find((button) => button.textContent?.includes("AI Agent"));
    await act(async () => agentTab?.click());
    const ragToggle = container.querySelector<HTMLInputElement>("#hosted-rag-sync")!;
    const skillsToggle = container.querySelector<HTMLInputElement>("#hosted-skills-sync")!;
    expect(ragToggle.checked).toBe(true);
    expect(skillsToggle.checked).toBe(true);
    if (!rag) await act(async () => ragToggle.click());
    if (!skills) await act(async () => skillsToggle.click());
    expect(ragToggle.checked).toBe(rag);
    expect(skillsToggle.checked).toBe(skills);
    expect(container.textContent).toContain("Local pgvector documents and dashboard-managed knowledge remain unchanged");
    expect(container.textContent).toContain("Local skills and unrelated dashboard-managed configuration remain unchanged");
    await act(async () => container.querySelector("form")?.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true })));
    expect(mcpMocks.agentConfigSaves[0]).toMatchObject({
      elevenLabsRagSyncEnabled: rag,
      elevenLabsAgentSkillsSyncEnabled: skills,
    });
  });

  it("shows per-document hosted sync states and surfaces retry errors", async () => {
    mcpMocks.agentConfig = { mode: "elevenlabs_agent", elevenLabsAgentId: "agent_fixture" };
    mcpMocks.documents = [
      { id: "doc-synced", title: "Synced", filename: "synced.txt", status: "ready", chunkCount: 1, elevenLabsSyncStatus: "synced" },
      { id: "doc-indexing", title: "Indexing", filename: "indexing.txt", status: "ready", chunkCount: 1, elevenLabsSyncStatus: "indexing" },
      { id: "doc-failed", title: "Failed", filename: "failed.txt", status: "ready", chunkCount: 1, elevenLabsSyncStatus: "failed", elevenLabsSyncError: "Provider unavailable." },
      { id: "doc-not-synced", title: "Not synced", filename: "local.txt", status: "ready", chunkCount: 1, elevenLabsSyncStatus: "not_synced" },
    ];
    mcpMocks.hostedSyncError = "Hosted synchronization failed safely.";
    container = document.createElement("div");
    document.body.append(container);
    root = createRoot(container);
    await act(async () => root.render(createElement(AdminPage)));
    const ragTab = Array.from(container.querySelectorAll("button")).find((button) => button.textContent?.includes("RAG Knowledge"));
    await act(async () => ragTab?.click());

    for (const status of ["synced", "indexing", "failed", "not_synced", "Provider unavailable."]) {
      expect(container.textContent).toContain(status);
    }
    const retryButton = Array.from(container.querySelectorAll("button")).find((button) => button.textContent?.includes("Sync / retry hosted agent"));
    expect(retryButton).toBeDefined();
    await act(async () => retryButton?.click());
    expect(mcpMocks.hostedSyncCalls).toBe(1);
    expect(container.textContent).toContain("Hosted synchronization failed safely.");
  });

  it.each([
    { rag: true, skills: true },
    { rag: false, skills: true },
    { rag: true, skills: false },
    { rag: false, skills: false },
  ])("reports saved hosted domain status without treating disabled sync as failed: %j", async ({ rag, skills }) => {
    mcpMocks.agentConfig = {
      mode: "elevenlabs_agent", elevenLabsAgentId: "agent_fixture",
      elevenLabsRagSyncEnabled: rag, elevenLabsAgentSkillsSyncEnabled: skills,
    };
    mcpMocks.documents = [{
      id: "historical", title: "Local document", filename: "local.txt", status: "ready", chunkCount: 1,
      elevenLabsSyncStatus: "failed", elevenLabsSyncError: "Historical provider error.",
    }];
    container = document.createElement("div");
    document.body.append(container);
    root = createRoot(container);
    await act(async () => root.render(createElement(AdminPage)));
    const clickTab = async (label: string) => {
      const button = Array.from(container.querySelectorAll("button")).find((button) => button.textContent?.includes(label));
      await act(async () => button?.click());
    };
    await clickTab("RAG Knowledge");
    expect(container.textContent).toContain(rag ? "Ready local documents sync additively" : "RAG application sync disabled");
    if (!rag) {
      expect(container.textContent).toContain("Last known hosted state: failed");
      expect(container.textContent).not.toContain("Historical provider error.");
    }
    const retry = Array.from(container.querySelectorAll("button")).find((button) => button.textContent?.includes("Sync / retry hosted agent"))!;
    expect(retry.disabled).toBe(false);
    await act(async () => retry.click());
    expect(mcpMocks.hostedSyncCalls).toBe(1);
    expect(container.textContent).toContain(`RAG documents: ${rag ? "synchronized" : "application sync disabled"}`);
    expect(container.textContent).toContain(`Agent Skills: ${skills ? "synchronized" : "application sync disabled"}`);
    expect(container.textContent).toContain("Hosted base prompt and selected MCP sync completed");
    await clickTab("Agent Skills");
    expect(container.textContent).toContain(skills ? "Agent Skill application sync enabled" : "Agent Skill application sync disabled");
  });

  it("keeps status on the saved policy when an unsaved disable fails", async () => {
    mcpMocks.agentConfig = { mode: "elevenlabs_agent", elevenLabsAgentId: "agent_fixture" };
    mcpMocks.saveConfigError = "Cleanup could not be verified.";
    container = document.createElement("div");
    document.body.append(container);
    root = createRoot(container);
    await act(async () => root.render(createElement(AdminPage)));
    const agentTab = Array.from(container.querySelectorAll("button")).find((button) => button.textContent?.includes("AI Agent"));
    await act(async () => agentTab?.click());
    await act(async () => container.querySelector<HTMLInputElement>("#hosted-rag-sync")!.click());
    await act(async () => container.querySelector("form")?.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true })));
    expect(container.textContent).toContain("Cleanup could not be verified.");
    const ragTab = Array.from(container.querySelectorAll("button")).find((button) => button.textContent?.includes("RAG Knowledge"));
    await act(async () => ragTab?.click());
    expect(container.textContent).toContain("Ready local documents sync additively");
    expect(container.textContent).not.toContain("RAG application sync disabled");
  });

  it("tests an unsaved hosted candidate and shows safe errors without saving", async () => {
    mcpMocks.agentConfig = { mode: "elevenlabs_agent", elevenLabsAgentId: "candidate_agent", elevenLabsApiKeyPreview: "*****cret" };
    container = document.createElement("div");
    document.body.append(container);
    root = createRoot(container);
    await act(async () => root.render(createElement(AdminPage)));
    const agentTab = Array.from(container.querySelectorAll("button")).find((button) => button.textContent?.includes("AI Agent"));
    await act(async () => agentTab?.click());
    const testButton = Array.from(container.querySelectorAll("button")).find((button) => button.textContent?.includes("Test Connection"));
    await act(async () => testButton?.click());
    expect(mcpMocks.connectionTests[0]).toEqual({ mode: "elevenlabs_agent", elevenLabsAgentId: "candidate_agent", elevenLabsApiKey: "" });
    expect(mcpMocks.agentConfigSaves).toHaveLength(0);
    expect(container.textContent).toContain("Test grounding and playback separately");
    mcpMocks.connectionError = "Enable private agent authentication.";
    await act(async () => testButton?.click());
    expect(container.textContent).toContain("Enable private agent authentication.");
    expect(container.querySelector<HTMLInputElement>("#elevenlabs-api-key")?.value).toBe("");
  });

  it("shows hosted activation errors without exposing a saved key", async () => {
    mcpMocks.agentConfig = {
      mode: "cascaded",
      elevenLabsAgentId: "candidate_agent",
      elevenLabsApiKeyPreview: "*****cret",
    };
    mcpMocks.saveConfigError = "Private agent validation failed.";
    container = document.createElement("div");
    document.body.append(container);
    root = createRoot(container);
    await act(async () => root.render(createElement(AdminPage)));
    const agentTab = Array.from(container.querySelectorAll("button")).find((button) => button.textContent?.includes("AI Agent"));
    await act(async () => agentTab?.click());
    const mode = container.querySelector<HTMLSelectElement>("#pipeline-mode");
    await act(async () => {
      mode!.value = "elevenlabs_agent";
      mode!.dispatchEvent(new Event("change", { bubbles: true }));
    });
    await act(async () => container.querySelector("form")?.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true })));
    expect(container.textContent).toContain("Private agent validation failed.");
    expect(container.querySelector<HTMLInputElement>("#elevenlabs-api-key")?.value).toBe("");
    expect(container.textContent).not.toContain("fixture-elevenlabs-secret");
    expect(mcpMocks.agentConfigSaves).toHaveLength(0);
  });

  it("hides discovered tools per server without changing server cards or allowlists", async () => {
    mcpMocks.servers = [
      {
        id: "server-one",
        name: "Search MCP",
        transport: "streamable_http",
        endpointUrl: "https://search.example/mcp",
        authHeaderPreview: "",
        enabled: true,
        allowedTools: ["web_search"],
        discoveredTools: [{ name: "web_search", description: "Search the web" }],
        timeoutMs: 10000,
      },
      {
        id: "server-two",
        name: "Data MCP",
        transport: "streamable_http",
        endpointUrl: "https://data.example/mcp",
        authHeaderPreview: "",
        enabled: true,
        allowedTools: [],
        discoveredTools: [{ name: "query_data", description: "Query records" }],
        timeoutMs: 10000,
      },
    ];

    container = document.createElement("div");
    document.body.append(container);
    root = createRoot(container);

    await act(async () => {
      root.render(createElement(AdminPage));
    });

    const mcpTab = Array.from(container.querySelectorAll("button")).find((button) =>
      button.textContent?.includes("MCP Servers")
    );
    await act(async () => {
      mcpTab?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });

    const searchCard = Array.from(container.querySelectorAll("h3"))
      .find((heading) => heading.textContent === "Search MCP")
      ?.closest("div.rounded-2xl");
    const dataCard = Array.from(container.querySelectorAll("h3"))
      .find((heading) => heading.textContent === "Data MCP")
      ?.closest("div.rounded-2xl");
    expect(searchCard).toBeDefined();
    expect(dataCard).toBeDefined();
    expect(searchCard?.textContent).toContain("web_search");
    expect(dataCard?.textContent).toContain("query_data");

    const hideSearch = Array.from(searchCard?.querySelectorAll("button") || []).find(
      (button) => button.textContent === "Hide Tools"
    );
    await act(async () => {
      hideSearch?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });

    expect(searchCard?.textContent).toContain("Search MCP");
    expect(searchCard?.textContent).not.toContain("web_search");
    expect(dataCard?.textContent).toContain("query_data");
    expect(mcpMocks.updates).toHaveLength(0);

    const dataCheckbox = dataCard?.querySelector<HTMLInputElement>('input[type="checkbox"]');
    expect(dataCheckbox?.checked).toBe(false);
    await act(async () => {
      dataCheckbox?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });

    const refreshedSearchCard = Array.from(container.querySelectorAll("h3"))
      .find((heading) => heading.textContent === "Search MCP")
      ?.closest("div.rounded-2xl");
    const refreshedDataCard = Array.from(container.querySelectorAll("h3"))
      .find((heading) => heading.textContent === "Data MCP")
      ?.closest("div.rounded-2xl");
    expect(refreshedSearchCard?.textContent).not.toContain("web_search");
    expect(refreshedSearchCard?.textContent).toContain("Show Tools");
    expect(refreshedDataCard?.textContent).toContain("query_data");
    expect(refreshedDataCard?.textContent).toContain("Hide Tools");
    expect(mcpMocks.updates).toHaveLength(1);
    expect(JSON.parse(String(mcpMocks.updates[0].options?.body))).toMatchObject({
      allowedTools: ["query_data"],
    });
  });
});
