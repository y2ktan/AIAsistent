import { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { HostedMCPControls } from "@/components/HostedMCPControls";
import { getSkillToolReadiness } from "@/lib/agent-skills";

describe("hosted MCP governance controls", () => {
  let container: HTMLDivElement;
  let root: ReturnType<typeof createRoot>;
  const save = vi.fn();
  const retry = vi.fn();
  beforeEach(() => {
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
    save.mockReset().mockResolvedValue(undefined);
    retry.mockReset().mockResolvedValue(undefined);
    container = document.createElement("div");
    document.body.append(container);
    root = createRoot(container);
  });
  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
    vi.unstubAllGlobals();
  });
  async function render(props: Partial<Parameters<typeof HostedMCPControls>[0]> = {}): Promise<void> {
    await act(async () => root.render(<HostedMCPControls allowedTools={["lookup"]} proxyAvailable onSave={save} onRetry={retry} {...props} />));
  }
  async function select(mode: string): Promise<void> {
    const input = container.querySelector("select")!;
    await act(async () => {
      input.value = mode;
      input.dispatchEvent(new Event("change", { bubbles: true }));
    });
  }
  it("defaults existing and new records to not shared", async () => {
    await render();
    expect(container.querySelector("select")?.value).toBe("disabled");
    expect(container.textContent).toContain("Not shared with hosted agents");
    expect(save).not.toHaveBeenCalled();
  });
  it("requires explicit direct credential and external-action consent before saving", async () => {
    await render();
    await select("direct");
    const button = container.querySelector("button")!;
    expect(button.disabled).toBe(true);
    expect(container.textContent).toContain("authentication with ElevenLabs");
    expect(container.textContent).toContain("Every currently discovered tool");
    await act(async () => container.querySelector<HTMLInputElement>('input[type="checkbox"]')!.click());
    await act(async () => button.click());
    expect(save).toHaveBeenCalledWith("direct", true);
  });
  it("explains proxy credentials and excludes unavailable deployments", async () => {
    await render();
    await select("proxy");
    expect(container.textContent).toContain("original MCP credential stays in the application");
    expect(container.textContent).toContain("Allowlisted tools: lookup");
    await render({ proxyAvailable: false });
    expect(container.querySelector<HTMLOptionElement>('option[value="proxy"]')?.disabled).toBe(true);
    expect(container.textContent).toContain("HOSTED_MCP_PROXY_BASE_URL");
  });
  it("shows incomplete revocation and surfaces failed retries without reporting readiness", async () => {
    retry.mockRejectedValue(new Error("Remote cleanup could not be verified."));
    await render({ mode: "disabled", status: "revocation_pending", error: "Provider unavailable." });
    expect(container.textContent).toContain("revocation is incomplete");
    await act(async () => container.querySelectorAll("button")[1].click());
    expect(container.querySelector('[role="alert"]')?.textContent).toContain("Remote cleanup could not be verified");
    expect(container.querySelector('[role="status"]')?.textContent).toContain("revocation_pending");
  });
  it("does not mark unshared or failed local MCP tools ready in hosted mode", () => {
    const server = { name: "CRM", enabled: true, allowedTools: ["lookup"], discoveredTools: [{ name: "lookup" }] };
    expect(getSkillToolReadiness(["lookup"], [server], true)[0].status).toBe("unconfigured");
    expect(getSkillToolReadiness(["lookup"], [{ ...server, hostedMode: "proxy", hostedSyncStatus: "failed" }], true)[0].status).toBe("unconfigured");
    expect(getSkillToolReadiness(["lookup"], [{ ...server, hostedMode: "proxy", hostedSyncStatus: "synced" }], true)[0].status).toBe("ready");
    expect(getSkillToolReadiness(["lookup"], [server])[0].status).toBe("ready");
  });
});
