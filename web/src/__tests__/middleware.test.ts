import { describe, it, expect, vi, beforeEach } from "vitest";
import { NextRequest } from "next/server";
import { middleware } from "@/middleware";
import { SECURITY_HEADERS, isProtectedPath } from "@/lib/security-headers";

describe("Next.js security headers and route protection", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("identifies protected vs public paths", () => {
    expect(isProtectedPath("/")).toBe(true);
    expect(isProtectedPath("/admin")).toBe(true);
    expect(isProtectedPath("/admin/users")).toBe(true);
    expect(isProtectedPath("/login")).toBe(false);
    expect(isProtectedPath("/api/health")).toBe(false);
  });

  it("redirects unauthenticated user from / to /login with security headers", async () => {
    const req = new NextRequest("http://localhost:3000/");
    const res = await middleware(req);
    expect(res.status).toBe(307);
    expect(res.headers.get("location")).toContain("/login");
    for (const [k, v] of Object.entries(SECURITY_HEADERS)) {
      expect(res.headers.get(k)).toBe(v);
    }
  });

  it("redirects non-admin user away from /admin to /", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValueOnce(
      new Response(JSON.stringify({ user: { id: "u1", role: "user" } }), {
        status: 200,
        headers: { "content-type": "application/json" },
      })
    );
    const req = new NextRequest("http://localhost:3000/admin", {
      headers: { cookie: "aegis_session=valid-token" },
    });
    const res = await middleware(req);
    expect(res.status).toBe(307);
    expect(res.headers.get("location")).toBe("http://localhost:3000/");
  });

  it("allows admin user onto /admin with hardened headers", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValueOnce(
      new Response(JSON.stringify({ user: { id: "a1", role: "admin" } }), {
        status: 200,
        headers: { "content-type": "application/json" },
      })
    );
    const req = new NextRequest("http://localhost:3000/admin", {
      headers: { cookie: "aegis_session=admin-token" },
    });
    const res = await middleware(req);
    expect(res.status).toBe(200);
    expect(res.headers.get("Permissions-Policy")).toBe("camera=(self), microphone=(self), geolocation=()");
  });
});
