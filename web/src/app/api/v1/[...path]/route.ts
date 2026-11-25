import { NextRequest, NextResponse } from "next/server";
import { applySecurityHeaders } from "@/lib/security-headers";

const BACKEND_URL = process.env.BACKEND_INTERNAL_URL || "http://server:8080";

async function proxyRequest(
  request: NextRequest,
  context: { params: Promise<{ path: string[] }> }
) {
  const { path } = await context.params;
  const targetUrl = `${BACKEND_URL}/api/v1/${path.join("/")}${request.nextUrl.search}`;

  const headers = new Headers();
  const forwardHeaders = ["cookie", "content-type", "x-csrf-token", "origin", "accept"];
  for (const h of forwardHeaders) {
    const val = request.headers.get(h);
    if (val) headers.set(h, val);
  }

  const init: RequestInit = {
    method: request.method,
    headers,
    cache: "no-store",
  };

  if (request.method !== "GET" && request.method !== "HEAD") {
    init.body = await request.arrayBuffer();
  }

  try {
    const upstream = await fetch(targetUrl, init);
    const respHeaders = new Headers();
    respHeaders.set("Cache-Control", "no-store");
    const ct = upstream.headers.get("content-type");
    if (ct) respHeaders.set("content-type", ct);

    const setCookie = upstream.headers.get("set-cookie");
    if (setCookie) respHeaders.set("set-cookie", setCookie);

    const retryAfter = upstream.headers.get("retry-after");
    if (retryAfter) respHeaders.set("retry-after", retryAfter);

    applySecurityHeaders(respHeaders);
    const body = await upstream.arrayBuffer();
    return new NextResponse(body, {
      status: upstream.status,
      headers: respHeaders,
    });
  } catch {
    const respHeaders = new Headers({ "content-type": "application/json" });
    respHeaders.set("Cache-Control", "no-store");
    applySecurityHeaders(respHeaders);
    return new NextResponse(JSON.stringify({ error: "backend unavailable" }), {
      status: 502,
      headers: respHeaders,
    });
  }
}

export const GET = proxyRequest;
export const POST = proxyRequest;
export const PUT = proxyRequest;
export const PATCH = proxyRequest;
export const DELETE = proxyRequest;
