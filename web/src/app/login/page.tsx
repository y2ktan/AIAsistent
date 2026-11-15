"use client";

import { useEffect, useState, FormEvent } from "react";
import { useRouter } from "next/navigation";
import { Lock, ShieldCheck, Sparkles, AlertCircle, Loader2 } from "lucide-react";
import { apiFetch, setCsrfToken } from "@/lib/api-client";

export default function LoginPage() {
  const router = useRouter();
  const [checkingBootstrap, setCheckingBootstrap] = useState(true);
  const [needsBootstrap, setNeedsBootstrap] = useState(false);

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [bootstrapToken, setBootstrapToken] = useState("");
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    let active = true;
    apiFetch<{ needsBootstrap: boolean }>("/api/v1/auth/bootstrap-status")
      .then((res) => {
        if (active) setNeedsBootstrap(Boolean(res.needsBootstrap));
      })
      .catch(() => {
        if (active) setNeedsBootstrap(false);
      })
      .finally(() => {
        if (active) setCheckingBootstrap(false);
      });
    return () => {
      active = false;
    };
  }, []);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError("");
    setSubmitting(true);

    try {
      if (needsBootstrap) {
        const res = await apiFetch<{ csrfToken: string }>("/api/v1/auth/bootstrap", {
          method: "POST",
          body: JSON.stringify({ bootstrapToken, email, password }),
        });
        if (res.csrfToken) setCsrfToken(res.csrfToken);
      } else {
        const res = await apiFetch<{ csrfToken: string }>("/api/v1/auth/login", {
          method: "POST",
          body: JSON.stringify({ email, password }),
        });
        if (res.csrfToken) setCsrfToken(res.csrfToken);
      }
      router.push("/");
      router.refresh();
    } catch (err) {
      setError(
        needsBootstrap
          ? err instanceof Error
            ? err.message
            : "Bootstrap failed"
          : "Invalid email or password"
      );
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <main className="relative flex min-h-screen items-center justify-center overflow-hidden bg-[#05070F] px-4 text-slate-100">
      {/* Ambient bioluminescent backdrop */}
      <div className="pointer-events-none absolute -top-40 left-1/2 h-[520px] w-[520px] -translate-x-1/2 rounded-full bg-cyan-500/15 blur-[140px]" />
      <div className="pointer-events-none absolute -bottom-40 right-1/4 h-[420px] w-[420px] rounded-full bg-violet-500/15 blur-[140px]" />

      <div className="relative w-full max-w-md rounded-3xl border border-slate-700/40 bg-slate-900/60 p-8 shadow-2xl backdrop-blur-2xl">
        <div className="mb-7 flex flex-col items-center text-center">
          <div className="mb-4 flex h-12 w-12 items-center justify-center rounded-2xl border border-cyan-400/30 bg-cyan-500/10 text-cyan-300 shadow-lg shadow-cyan-500/10">
            {needsBootstrap ? (
              <ShieldCheck className="h-6 w-6" />
            ) : (
              <Sparkles className="h-6 w-6" />
            )}
          </div>
          <h1 className="text-2xl font-semibold tracking-tight text-white">
            {needsBootstrap ? "Initialize Aegis Voice" : "Sign in to Aegis Voice"}
          </h1>
          <p className="mt-1.5 text-sm text-slate-400">
            {needsBootstrap
              ? "Create the first administrator account using your one-time bootstrap token."
              : "Zero-Trust authenticated voice & vision workspace."}
          </p>
        </div>

        {checkingBootstrap ? (
          <div className="flex items-center justify-center py-10 text-slate-400">
            <Loader2 className="h-5 w-5 animate-spin" />
          </div>
        ) : (
          <form onSubmit={handleSubmit} className="space-y-4">
            {error && (
              <div
                role="alert"
                className="flex items-center gap-2.5 rounded-xl border border-rose-500/30 bg-rose-500/10 px-3.5 py-2.5 text-sm text-rose-200"
              >
                <AlertCircle className="h-4 w-4 shrink-0 text-rose-400" />
                <span>{error}</span>
              </div>
            )}

            {needsBootstrap && (
              <div>
                <label
                  htmlFor="bootstrapToken"
                  className="mb-1.5 block text-xs font-medium uppercase tracking-wider text-slate-400"
                >
                  Bootstrap Token
                </label>
                <input
                  id="bootstrapToken"
                  type="password"
                  required
                  value={bootstrapToken}
                  onChange={(e) => setBootstrapToken(e.target.value)}
                  placeholder="Enter ADMIN_BOOTSTRAP_TOKEN"
                  className="w-full rounded-xl border border-slate-700/60 bg-slate-950/70 px-3.5 py-2.5 text-sm text-white placeholder-slate-500 outline-none transition focus:border-cyan-400/70 focus:ring-2 focus:ring-cyan-500/20"
                />
              </div>
            )}

            <div>
              <label
                htmlFor="email"
                className="mb-1.5 block text-xs font-medium uppercase tracking-wider text-slate-400"
              >
                Email Address
              </label>
              <input
                id="email"
                type="email"
                autoComplete="email"
                required
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                placeholder="you@organization.com"
                className="w-full rounded-xl border border-slate-700/60 bg-slate-950/70 px-3.5 py-2.5 text-sm text-white placeholder-slate-500 outline-none transition focus:border-cyan-400/70 focus:ring-2 focus:ring-cyan-500/20"
              />
            </div>

            <div>
              <label
                htmlFor="password"
                className="mb-1.5 block text-xs font-medium uppercase tracking-wider text-slate-400"
              >
                Password
              </label>
              <input
                id="password"
                type="password"
                autoComplete={needsBootstrap ? "new-password" : "current-password"}
                required
                minLength={needsBootstrap ? 12 : 1}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder={needsBootstrap ? "Minimum 12 characters" : "••••••••••••"}
                className="w-full rounded-xl border border-slate-700/60 bg-slate-950/70 px-3.5 py-2.5 text-sm text-white placeholder-slate-500 outline-none transition focus:border-cyan-400/70 focus:ring-2 focus:ring-cyan-500/20"
              />
            </div>

            <button
              type="submit"
              disabled={submitting}
              className="mt-2 flex w-full items-center justify-center gap-2 rounded-xl bg-gradient-to-r from-cyan-500 to-blue-600 px-4 py-2.5 text-sm font-medium text-white shadow-lg shadow-cyan-500/20 transition hover:from-cyan-400 hover:to-blue-500 disabled:opacity-50"
            >
              {submitting ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <Lock className="h-4 w-4" />
              )}
              <span>
                {needsBootstrap ? "Create Administrator & Sign In" : "Sign In"}
              </span>
            </button>
          </form>
        )}
      </div>
    </main>
  );
}
