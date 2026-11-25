"use client";

import { useEffect, useState } from "react";

export type HostedMCPMode = "disabled" | "direct" | "proxy";

interface HostedMCPControlsProps {
  mode?: HostedMCPMode;
  status?: string;
  error?: string;
  allowedTools: string[];
  proxyAvailable: boolean;
  onSave: (mode: HostedMCPMode, confirmed: boolean) => Promise<void>;
  onRetry: () => Promise<void>;
}

export function HostedMCPControls({
  mode = "disabled", status = "disabled", error, allowedTools, proxyAvailable, onSave, onRetry,
}: HostedMCPControlsProps) {
  const [selected, setSelected] = useState<HostedMCPMode>(mode);
  const [confirmed, setConfirmed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [operationError, setOperationError] = useState("");
  useEffect(() => {
    setSelected(mode);
    setConfirmed(false);
  }, [mode]);

  async function perform(action: () => Promise<void>): Promise<void> {
    setBusy(true);
    setOperationError("");
    try {
      await action();
      setConfirmed(false);
    } catch (err) {
      setOperationError(err instanceof Error ? err.message : "Hosted MCP update failed.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="mt-4 space-y-3 rounded-xl border border-slate-700 p-4 text-xs text-slate-300">
      <label className="flex items-center gap-3">
        Hosted integration
        <select
          aria-label="Hosted integration mode"
          value={selected}
          disabled={busy}
          onChange={(event) => {
            const value = event.target.value;
            if (value === "disabled" || value === "direct" || value === "proxy") {
              setSelected(value);
              setConfirmed(false);
            }
          }}
          className="rounded-lg border border-slate-600 bg-slate-950 px-3 py-2"
        >
          <option value="disabled">Disabled (not shared)</option>
          <option value="direct">Direct registration</option>
          <option value="proxy" disabled={!proxyAvailable}>Application proxy</option>
        </select>
      </label>
      {selected === "disabled" && <p>Not shared with hosted agents. Local MCP behavior is unchanged. Remote cleanup must be verified before revocation is complete.</p>}
      {selected === "direct" && <p>Shares this public HTTPS endpoint and its authentication with ElevenLabs. Every currently discovered tool must be allowlisted; use proxy mode for a partial allowlist.</p>}
      {selected === "proxy" && <p>ElevenLabs calls the application over public HTTPS with a dedicated credential. The original MCP credential stays in the application. Only enabled, discovered, allowlisted tools can be called.</p>}
      {!proxyAvailable && <p>Proxy mode requires a public HTTPS HOSTED_MCP_PROXY_BASE_URL routed to the Go backend.</p>}
      {selected !== "disabled" && (
        <>
          <p>Provider-executed tools may send conversation-derived data to external services, incur charges, and cause external side effects. The non-secret ownership marker may reach the upstream service; no local conversation tool loop is started.</p>
          <p>Allowlisted tools: {allowedTools.length ? allowedTools.join(", ") : "none"}</p>
          <label className="flex items-start gap-2">
            <input type="checkbox" checked={confirmed} onChange={(event) => setConfirmed(event.target.checked)} />
            <span>{selected === "direct" ? "I confirm provider-side credential sharing and external actions." : "I acknowledge external processing and tool side effects."}</span>
          </label>
        </>
      )}
      <p role="status">Hosted status: {status}{error ? ` - ${error}` : ""}</p>
      {(status === "recovery_pending" || status === "revocation_pending") && <p className="text-amber-200">Recovery or revocation is incomplete. Retry verifies ownership without creating duplicates; unresolved recovery may require administrator assistance.</p>}
      {operationError && <p role="alert" className="text-rose-300">{operationError}</p>}
      <div className="flex gap-3">
        <button
          type="button"
          disabled={busy || (selected !== "disabled" && !confirmed) || (selected === "proxy" && !proxyAvailable)}
          onClick={() => void perform(() => onSave(selected, confirmed))}
          className="rounded-lg border border-cyan-500/40 px-3 py-2 disabled:opacity-40"
        >{busy ? "Synchronizing..." : "Save hosted integration"}</button>
        <button type="button" disabled={busy || status === "disabled"} onClick={() => void perform(onRetry)} className="rounded-lg border border-slate-600 px-3 py-2 disabled:opacity-40">Retry hosted sync / cleanup</button>
      </div>
    </div>
  );
}
