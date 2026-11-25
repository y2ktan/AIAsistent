"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useConversation } from "@elevenlabs/react";
import { apiFetch } from "@/lib/api-client";
import { normalizeAssistantName, type TranscriptItem, type useVoiceAgentRTC } from "./useVoiceAgentRTC";

export function useHostedVoiceAgent(name: string): ReturnType<typeof useVoiceAgentRTC> {
  const [connected, setConnected] = useState(false);
  const [connecting, setConnecting] = useState(false);
  const [micEnabled, setMicEnabled] = useState(true);
  const [speakerEnabled, setSpeakerEnabled] = useState(true);
  const [transcripts, setTranscripts] = useState<TranscriptItem[]>([]);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);
  const phase = useRef<"idle" | "token" | "sdk" | "connected" | "ending">("idle");
  const generation = useRef(0);
  const mounted = useRef(true);
  const tokenRequest = useRef<AbortController | null>(null);
  const tokenDeadline = useRef<number | null>(null);
  const authRequest = useRef<AbortController | null>(null);
  const micIntent = useRef(true);
  const speakerIntent = useRef(true);
  const remoteAudioRef = useRef<HTMLAudioElement | null>(null);
  const videoPreviewRef = useRef<HTMLVideoElement | null>(null);

  const upsert = useCallback((item: TranscriptItem, append = false) => {
    setTranscripts((previous) => {
      const index = previous.findIndex((entry) => entry.id === item.id);
      if (index < 0) return [...previous, item];
      return previous.map((entry, i) => i === index
        ? { ...item, text: append ? entry.text + item.text : item.text }
        : entry);
    });
  }, []);
  const responseID = (event: { event_id: number; response_id?: string }) =>
    `assistant:${event.response_id ?? event.event_id}`;

  const conversation = useConversation({ micMuted: !micEnabled, volume: speakerEnabled ? 1 : 0 });
  const { startSession, endSession, setMuted, setVolume, getInputVolume, getOutputVolume } = conversation;

  const reset = useCallback(() => {
    phase.current = "idle";
    tokenRequest.current?.abort();
    tokenRequest.current = null;
    if (tokenDeadline.current !== null) window.clearTimeout(tokenDeadline.current);
    tokenDeadline.current = null;
    authRequest.current?.abort();
    authRequest.current = null;
    if (mounted.current) {
      setConnected(false);
      setConnecting(false);
    }
  }, []);

  const stopCall = useCallback(() => {
    generation.current++;
    authRequest.current?.abort();
    tokenRequest.current?.abort();
    if (tokenDeadline.current !== null) window.clearTimeout(tokenDeadline.current);
    tokenDeadline.current = null;
    if (phase.current === "idle") return;
    const pendingToken = phase.current === "token";
    phase.current = "ending";
    if (mounted.current) {
      setConnected(false);
      setConnecting(true);
    }
    endSession();
    if (pendingToken) reset();
  }, [endSession, reset]);

  const startCall = useCallback(async () => {
    if (phase.current !== "idle") return;
    const current = ++generation.current;
    let receivedAgentResponse = false;
    const controller = new AbortController();
    tokenRequest.current = controller;
    phase.current = "token";
    setConnecting(true);
    setErrorMsg(null);
    setTranscripts([]);
    const timeout = window.setTimeout(() => {
      if (mounted.current && current === generation.current && phase.current === "token") {
        setErrorMsg("Hosted access timed out. Check connectivity and start again.");
        reset();
      }
    }, 20_000);
    tokenDeadline.current = timeout;
    try {
      const access = await apiFetch<{ conversationToken: string; conversationId: string; mode: string }>(
        "/api/v1/elevenlabs/conversation-token",
        { method: "POST", body: "{}", signal: controller.signal, cache: "no-store" }
      );
      if (!mounted.current || current !== generation.current || controller.signal.aborted) return;
      if (!access.conversationToken || !access.conversationId || access.mode !== "elevenlabs_agent") {
        throw new Error("Invalid hosted access. Refresh and start again.");
      }
      phase.current = "sdk";
      startSession({
        conversationToken: access.conversationToken,
        connectionType: "webrtc",
        initialMicMuted: !micIntent.current,
        initialVolume: speakerIntent.current ? 1 : 0,
        sessionSignal: controller.signal,
        workletPaths: { rawAudioProcessor: "/elevenlabs/rawAudioProcessor.js" },
        onPlaybackError: () => {
          if (mounted.current && current === generation.current) {
            setErrorMsg("The browser could not play reply audio. End the call and start again to retry playback.");
            stopCall();
          }
        },
        onConnect: () => {
          if (!mounted.current || current !== generation.current) {
            endSession();
            return;
          }
          phase.current = "connected";
          tokenRequest.current = null;
          setMuted(!micIntent.current);
          setVolume({ volume: speakerIntent.current ? 1 : 0 });
          setConnected(true);
          setConnecting(false);
        },
        onDisconnect: (details) => {
          if (current !== generation.current && phase.current !== "ending") return;
          if (mounted.current && details.reason === "error") {
            setErrorMsg("Hosted voice disconnected. Check microphone permission and connectivity, then start again.");
          } else if (mounted.current && current === generation.current && details.reason === "agent" && !receivedAgentResponse) {
            setErrorMsg("ElevenLabs ended the call before replying. Check agent availability and account credits in the ElevenLabs dashboard.");
          }
          reset();
        },
        onStatusChange: ({ status }) => {
          if (status === "disconnecting" && current === generation.current && mounted.current) {
            phase.current = "ending";
            setConnected(false);
            setConnecting(true);
          }
          if (status === "disconnected" && phase.current === "ending") reset();
        },
        onError: () => {
          if (!mounted.current || current !== generation.current) return;
          setErrorMsg("Hosted voice failed. Check microphone permission, audio playback, and connectivity, then start again.");
          endSession();
          reset();
        },
        onMessage: (message) => {
          if (!mounted.current || current !== generation.current) return;
          if (message.role === "agent" && message.message) receivedAgentResponse = true;
          upsert({
            id: message.role === "user" ? `user:${message.event_id}` : responseID(message),
            role: message.role === "user" ? "user" : "assistant",
            text: message.message,
            isPartial: false,
          });
        },
        onAgentChatResponsePart: (part) => {
          if (!mounted.current || current !== generation.current) return;
          if (part.text) receivedAgentResponse = true;
          upsert({ id: responseID(part), role: "assistant", text: part.text, isPartial: part.type !== "stop" }, part.type !== "start");
        },
        onAgentResponseCorrection: (correction) => {
          if (!mounted.current || current !== generation.current) return;
          if (correction.corrected_agent_response) receivedAgentResponse = true;
          upsert({ id: responseID(correction), role: "assistant", text: correction.corrected_agent_response, isPartial: false });
        },
      });
    } catch (error) {
      if (mounted.current && current === generation.current && !controller.signal.aborted) {
        setErrorMsg(error instanceof Error ? error.message : "Hosted access failed. Start again.");
        reset();
      }
    } finally {
      window.clearTimeout(timeout);
      if (tokenDeadline.current === timeout) tokenDeadline.current = null;
    }
  }, [startSession, endSession, setMuted, setVolume, stopCall, reset, upsert]);

  useEffect(() => {
    if (!connected) return;
    const current = generation.current;
    let active = true;
    async function revalidate() {
      if (authRequest.current || !active) return;
      const controller = new AbortController();
      authRequest.current = controller;
      const timeout = window.setTimeout(() => controller.abort(), 10_000);
      try {
        const session = await apiFetch<{ user: { isActive: boolean } }>("/api/v1/auth/me", {
          signal: controller.signal, cache: "no-store",
        });
        if (!session.user?.isActive) throw new Error("Inactive session");
      } catch {
        if (active && mounted.current && current === generation.current) {
          setErrorMsg("Application access could not be verified. The hosted call has ended.");
          stopCall();
        }
      } finally {
        window.clearTimeout(timeout);
        if (authRequest.current === controller) authRequest.current = null;
      }
    }
    const timer = window.setInterval(() => void revalidate(), 30_000);
    const foreground = () => {
      if (document.visibilityState === "visible") void revalidate();
    };
    document.addEventListener("visibilitychange", foreground);
    window.addEventListener("focus", foreground);
    return () => {
      active = false;
      window.clearInterval(timer);
      authRequest.current?.abort();
      document.removeEventListener("visibilitychange", foreground);
      window.removeEventListener("focus", foreground);
    };
  }, [connected, stopCall]);

  useEffect(() => {
    mounted.current = true;
    const invalidate = () => generation.current++;
    return () => {
      mounted.current = false;
      invalidate();
      tokenRequest.current?.abort();
      if (tokenDeadline.current !== null) window.clearTimeout(tokenDeadline.current);
      authRequest.current?.abort();
      endSession();
    };
  }, [endSession]);

  const toggleMic = useCallback(() => {
    micIntent.current = !micIntent.current;
    setMicEnabled(micIntent.current);
    if (phase.current === "connected") setMuted(!micIntent.current);
  }, [setMuted]);
  const toggleSpeaker = useCallback(() => {
    speakerIntent.current = !speakerIntent.current;
    setSpeakerEnabled(speakerIntent.current);
    if (phase.current === "connected") setVolume({ volume: speakerIntent.current ? 1 : 0 });
  }, [setVolume]);
  const getAudioEnergy = useCallback(() => ({
    micLevel: micIntent.current && phase.current === "connected" ? getInputVolume() : 0,
    speakerLevel: speakerIntent.current && phase.current === "connected" ? getOutputVolume() : 0,
  }), [getInputVolume, getOutputVolume]);

  return {
    connected, connecting, assistantName: normalizeAssistantName(name),
    agentState: connected ? (conversation.isSpeaking ? "speaking" : "listening") : "idle",
    micEnabled, speakerEnabled, cameraEnabled: false, transcripts, citations: [],
    activeTool: null, errorMsg, permissionPrompt: null, remoteAudioRef, videoPreviewRef,
    startCall, stopCall, toggleMic, toggleSpeaker, toggleCamera: async () => {}, getAudioEnergy,
  };
}
