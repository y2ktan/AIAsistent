import { NextRequest, NextResponse } from "next/server";
import { applySecurityHeaders, isProtectedPath } from "@/lib/security-headers";

const BACKEND_URL = process.env.BACKEND_INTERNAL_URL || "http://server:8080";

export async function middleware(request: NextRequest) {
  const { pathname } = request.nextUrl;

  if (!isProtectedPath(pathname)) {
    const res = NextResponse.next();
    applySecurityHeaders(res.headers);
    return res;
  }

  const sessionCookie = request.cookies.get("aegis_session")?.value;
  if (!sessionCookie) {
    const loginUrl = new URL("/login", request.url);
    const res = NextResponse.redirect(loginUrl);
    applySecurityHeaders(res.headers);
    return res;
  }

  try {
    const authRes = await fetch(`${BACKEND_URL}/api/v1/auth/me`, {
      method: "GET",
      headers: {
        Cookie: `aegis_session=${sessionCookie}`,
      },
      cache: "no-store",
    });

    if (!authRes.ok) {
      const loginUrl = new URL("/login", request.url);
      const res = NextResponse.redirect(loginUrl);
      res.cookies.delete("aegis_session");
      applySecurityHeaders(res.headers);
      return res;
    }

    const data = (await authRes.json()) as { user?: { role?: string } };
    if (pathname.startsWith("/admin") && data.user?.role !== "admin") {
      const homeUrl = new URL("/", request.url);
      const res = NextResponse.redirect(homeUrl);
      applySecurityHeaders(res.headers);
      return res;
    }

    const res = NextResponse.next();
    applySecurityHeaders(res.headers);
    return res;
  } catch {
    const loginUrl = new URL("/login", request.url);
    const res = NextResponse.redirect(loginUrl);
    applySecurityHeaders(res.headers);
    return res;
  }
}

export const config = {
  matcher: ["/((?!_next/static|_next/image|favicon.ico).*)"],
};
