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
    if (path.endsWith("/users")) return { users: [] };
    if (path.endsWith("/documents")) return { documents: [] };
    if (path.endsWith("/voice-languages")) {
      return { languages: [], defaultLanguage: "en", saved: true, catalog: [] };
    }
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
    if (path.endsWith("/tts-voices")) return { voices: [], supported: false };
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
