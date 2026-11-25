"use client";

import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { ConversationProvider } from "@elevenlabs/react";
import { apiFetch } from "@/lib/api-client";
import { useVoiceAgentRTC } from "@/hooks/useVoiceAgentRTC";
import { useHostedVoiceAgent } from "@/hooks/useHostedVoiceAgent";

type VoiceSession = ReturnType<typeof useVoiceAgentRTC>;
interface Config {
  mode: "cascaded" | "realtime" | "elevenlabs_agent";
  assistantName: string;
}
type Renderer = (voice: VoiceSession, hosted: boolean) => ReactNode;
interface AdapterProps {
  config: Config;
  sequence: number;
  render: Renderer;
}

function SessionAdapter({ voice, sequence, render, hosted }: Omit<AdapterProps, "config"> & { voice: VoiceSession; hosted: boolean }) {
  const started = useRef(0);
  const { startCall } = voice;
  useEffect(() => {
    if (sequence > 0 && started.current !== sequence) {
      started.current = sequence;
      void startCall();
    }
  }, [sequence, startCall]);
  return render(voice, hosted);
}

function BackendAdapter(props: AdapterProps) {
  const voice = useVoiceAgentRTC();
  return <SessionAdapter {...props} voice={voice} hosted={false} />;
}

function HostedAdapter(props: AdapterProps) {
  const voice = useHostedVoiceAgent(props.config.assistantName);
  return <SessionAdapter {...props} voice={voice} hosted />;
}

export function VoiceSessionRouter({ children }: { children: Renderer }) {
  const [config, setConfig] = useState<Config | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const [sequence, setSequence] = useState(0);
  const request = useRef<AbortController | null>(null);

  const load = useCallback(async (start: boolean) => {
    if (request.current) return;
    const controller = new AbortController();
    request.current = controller;
    setRefreshing(true);
    setError(null);
    try {
      const next = await apiFetch<Config>("/api/v1/rtc/config", { signal: controller.signal, cache: "no-store" });
      if (controller.signal.aborted) return;
      if (!next || !["cascaded", "realtime", "elevenlabs_agent"].includes(next.mode)) {
        throw new Error("Conversation configuration is unavailable. Refresh and try again.");
      }
      setConfig(next);
      if (start) setSequence((value) => value + 1);
    } catch {
      if (!controller.signal.aborted) setError("Could not load conversation configuration. Try again.");
    } finally {
      if (request.current === controller) {
        request.current = null;
        setRefreshing(false);
      }
    }
  }, []);
  useEffect(() => {
    void load(false);
    return () => {
      request.current?.abort();
      request.current = null;
    };
  }, [load]);

  if (!config) {
    return <main className="min-h-screen bg-[#05070F] p-8 text-slate-100">
      <p role={error ? "alert" : "status"}>{error || "Loading voice configuration..."}</p>
      {error && <button onClick={() => void load(false)}>Retry</button>}
    </main>;
  }
  const render: Renderer = (voice, hosted) => children({
    ...voice,
    connecting: voice.connecting || refreshing,
    errorMsg: error || voice.errorMsg,
    startCall: () => load(true),
    stopCall: () => {
      request.current?.abort();
      voice.stopCall();
    },
  }, hosted);
  const props = { config, sequence, render };
  return config.mode === "elevenlabs_agent"
    ? <ConversationProvider><HostedAdapter {...props} /></ConversationProvider>
    : <BackendAdapter {...props} />;
}
