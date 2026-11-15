import { act, createElement, type ReactNode } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

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
  apiFetch: async (path: string) => {
    if (path.endsWith("/users")) return { users: [] };
    if (path.endsWith("/documents")) return { documents: [] };
    if (path.endsWith("/voice-languages")) {
      return { languages: [], defaultLanguage: "en", saved: true, catalog: [] };
    }
    if (path.endsWith("/mcp-servers")) return { servers: [] };
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
});
