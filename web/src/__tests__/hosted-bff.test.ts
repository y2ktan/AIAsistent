import { NextRequest } from "next/server";
import { afterEach, describe, expect, it, vi } from "vitest";
import { POST } from "@/app/api/v1/[...path]/route";
import { SECURITY_HEADERS } from "@/lib/security-headers";

describe("hosted token BFF and CSP", () => {
  afterEach(() => vi.restoreAllMocks());
  it.each([200, 400, 401, 429, 502])("keeps token and failure status %s non-cacheable", async (status) => {
    const payload = status === 200
      ? { conversationToken: "fixture", conversationId: "conv_fixture", mode: "elevenlabs_agent", assistantName: "Aegis" }
      : { error: "Safe hosted failure" };
    const fetch = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify(payload), {
      status, headers: { "content-type": "application/json", "cache-control": "public" },
    }));
    const request = new NextRequest("https://aegis.local/api/v1/elevenlabs/conversation-token", {
      method: "POST", body: "{}",
      headers: { cookie: "fixture-cookie", origin: "https://aegis.local", "x-csrf-token": "fixture-csrf" },
    });
    const response = await POST(request, { params: Promise.resolve({ path: ["elevenlabs", "conversation-token"] }) });
    expect(response.status).toBe(status);
    expect(response.headers.get("Cache-Control")).toContain("no-store");
    expect(await response.json()).toEqual(payload);
    const init = fetch.mock.calls[0][1];
    expect(init?.cache).toBe("no-store");
    const headers = new Headers(init?.headers);
    expect(headers.get("origin")).toBe("https://aegis.local");
    expect(headers.get("x-csrf-token")).toBe("fixture-csrf");
    expect(headers.has("xi-api-key")).toBe(false);
  });
  it("permits only the SDK default HTTPS signaling origin without broader script/frame policy", () => {
    const csp = SECURITY_HEADERS["Content-Security-Policy"];
    expect(csp).toContain("connect-src 'self' ws: wss: https://livekit.rtc.elevenlabs.io");
    expect(csp.match(/https:\/\/[^;\s]+/g)).toEqual(["https://livekit.rtc.elevenlabs.io"]);
    expect(csp).not.toMatch(/(?:^|\s)https:(?:\s|;|$)/);
    expect(csp).toContain("script-src 'self' 'unsafe-inline';");
    expect(csp).toContain("frame-ancestors 'none'");
    expect(SECURITY_HEADERS["Permissions-Policy"]).toBe("camera=(self), microphone=(self), geolocation=()");
  });
});
