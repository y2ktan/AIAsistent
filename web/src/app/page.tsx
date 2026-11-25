"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import {
  Mic,
  MicOff,
  Volume2,
  VolumeX,
  Video,
  VideoOff,
  Phone,
  PhoneOff,
  Shield,
  LogOut,
  BookOpen,
  ChevronDown,
  ChevronUp,
  Cpu,
  AlertTriangle,
} from "lucide-react";
import { apiFetch, fetchSession, AuthUser } from "@/lib/api-client";
import {
  filterVisibleCitations,
  normalizeAssistantName,
  type useVoiceAgentRTC,
} from "@/hooks/useVoiceAgentRTC";
import { LiquidPlasmaOrb } from "@/components/LiquidPlasmaOrb";
import { VoiceSessionRouter } from "@/components/VoiceSessionRouter";

export default function HomePage() {
  return <VoiceSessionRouter>{(voice, hosted) => <VoiceHome voice={voice} hosted={hosted} />}</VoiceSessionRouter>;
}

function VoiceHome({ voice, hosted }: { voice: ReturnType<typeof useVoiceAgentRTC>; hosted: boolean }) {
  const router = useRouter();
  const [user, setUser] = useState<AuthUser | null>(null);
  const [expandedCitation, setExpandedCitation] = useState<string | null>(null);

  const {
    connected,
    connecting,
    agentState,
    assistantName,
    micEnabled,
    speakerEnabled,
    cameraEnabled,
    transcripts,
    citations,
    activeTool,
    errorMsg,
    permissionPrompt,
    videoPreviewRef,
    remoteAudioRef,
    startCall,
    stopCall,
    toggleMic,
    toggleSpeaker,
    toggleCamera,
    getAudioEnergy,
  } = voice;

  useEffect(() => {
    fetchSession().then((res) => {
      if (!res) {
        router.push("/login");
        return;
      }
      setUser(res.user);
    });
  }, [router]);

  useEffect(() => {
    if (hosted) setExpandedCitation(null);
  }, [hosted]);

  async function handleLogout() {
    stopCall();
    try {
      await apiFetch("/api/v1/auth/logout", { method: "POST" });
    } finally {
      router.push("/login");
    }
  }

  const displayAssistantName = normalizeAssistantName(assistantName);
  const latestTranscripts = transcripts.slice(-3);
  const visibleCitations = filterVisibleCitations(citations);

  return (
    <main className="relative flex min-h-screen flex-col justify-between overflow-hidden bg-[#05070F] text-slate-100">
      <audio ref={remoteAudioRef} autoPlay playsInline className="hidden" />

      {/* Top Glassmorphic Header */}
      <header className="relative z-10 flex items-center justify-between px-6 py-4">
        <div className="flex items-center gap-3">
          <div className="h-2.5 w-2.5 rounded-full bg-cyan-400 shadow-sm shadow-cyan-400" />
          <span
            data-testid="assistant-header-name"
            className="text-sm font-semibold tracking-wide text-white"
          >
            {displayAssistantName}
          </span>
          <span className="rounded-full border border-slate-700/60 bg-slate-900/60 px-2.5 py-0.5 text-[11px] uppercase tracking-wider text-slate-300 backdrop-blur-md">
            {agentState}
          </span>
          {activeTool && (
            <span className="flex items-center gap-1 rounded-full border border-violet-500/40 bg-violet-500/15 px-2.5 py-0.5 text-[11px] text-violet-200">
              <Cpu className="h-3 w-3" />
              {activeTool}
            </span>
          )}
        </div>

        <div className="flex items-center gap-2.5">
          {user?.role === "admin" && (
            <Link
              href="/admin"
              className="flex items-center gap-1.5 rounded-xl border border-cyan-500/30 bg-cyan-500/10 px-3 py-1.5 text-xs font-medium text-cyan-200 transition hover:bg-cyan-500/20"
            >
              <Shield className="h-3.5 w-3.5" />
              Admin Console
            </Link>
          )}
          <button
            onClick={handleLogout}
            aria-label="Sign Out"
            className="flex items-center gap-1.5 rounded-xl border border-slate-700/60 bg-slate-900/60 px-3 py-1.5 text-xs text-slate-300 backdrop-blur-md transition hover:bg-slate-800"
          >
            <LogOut className="h-3.5 w-3.5" />
            Sign Out
          </button>
        </div>
      </header>

      {/* Center Stage: Liquid Plasma Orb + Kinetic Captions + Citations */}
      <section className="relative z-10 flex flex-1 flex-col items-center justify-center px-4 pb-28">
        <LiquidPlasmaOrb state={agentState} getAudioEnergy={getAudioEnergy} />
        {hosted && (
          <p className="mt-4 max-w-lg text-center text-xs text-slate-400">
            Assistant Name, System Instructions, enabled Agent Skills, and ready local documents are
            synchronized server-side. ElevenLabs processes audio and knowledge in the cloud and owns
            the model, voice, languages, retention, turn-taking, interruption, and noise handling.
            Usage may incur charges. Local wake/question/pause rules and Audio Focus do not apply.
            Camera, local citations, and runtime MCP tools are unavailable.
          </p>
        )}

        {(permissionPrompt || errorMsg) && (
          <div
            role="alert"
            className="mt-4 flex max-w-lg items-center gap-2.5 rounded-2xl border border-amber-500/40 bg-amber-500/10 px-4 py-3 text-xs text-amber-200 backdrop-blur-xl"
          >
            <AlertTriangle className="h-4 w-4 shrink-0 text-amber-400" />
            <span>{permissionPrompt || errorMsg}</span>
          </div>
        )}

        {/* Kinetic Streaming Captions */}
        {latestTranscripts.length > 0 && (
          <div className="mt-6 w-full max-w-2xl space-y-2.5">
            {latestTranscripts.map((t) => (
              <div
                key={t.id}
                className={`rounded-2xl border px-4 py-2.5 text-sm backdrop-blur-xl transition ${
                  t.role === "user"
                    ? "ml-auto max-w-md border-slate-700/50 bg-slate-900/50 text-slate-300"
                    : "mr-auto max-w-xl border-cyan-500/25 bg-slate-900/75 text-white"
                }`}
              >
                <span
                  data-testid="transcript-speaker-label"
                  className="mr-2 text-[10px] font-semibold uppercase tracking-wider text-slate-400"
                >
                  {t.role === "user" ? "You" : displayAssistantName}
                </span>
                <span>{t.text}</span>
              </div>
            ))}
          </div>
        )}

        {/* Grounded RAG Citation Cards */}
        {user?.role === "admin" && visibleCitations.length > 0 && (
          <div className="mt-4 flex w-full max-w-2xl flex-wrap justify-center gap-2">
            {visibleCitations.map((c) => {
              const citationKey = `${c.documentId}-${c.chunkIndex}`;
              const isOpen = expandedCitation === citationKey;
              return (
                <div
                  key={citationKey}
                  className="rounded-xl border border-emerald-500/30 bg-emerald-950/30 px-3 py-2 text-xs text-emerald-100 backdrop-blur-xl"
                >
                  <button
                    onClick={() => setExpandedCitation(isOpen ? null : citationKey)}
                    className="flex items-center gap-1.5 font-medium"
                  >
                    <BookOpen className="h-3.5 w-3.5 text-emerald-400" />
                    <span>{c.documentTitle}</span>
                    <span className="text-emerald-400/80">
                      ({Math.round(c.similarity * 100)}%)
                    </span>
                    {isOpen ? (
                      <ChevronUp className="h-3.5 w-3.5" />
                    ) : (
                      <ChevronDown className="h-3.5 w-3.5" />
                    )}
                  </button>
                  {isOpen && (
                    <p className="mt-2 max-w-md border-t border-emerald-500/20 pt-2 text-[11px] leading-relaxed text-slate-300">
                      {c.content}
                    </p>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </section>

      {/* Live PiP Camera Self-View */}
      {cameraEnabled && (
        <div className="fixed bottom-24 right-6 z-20 overflow-hidden rounded-2xl border border-cyan-400/50 bg-slate-950/90 shadow-2xl shadow-cyan-500/15">
          <video
            ref={videoPreviewRef}
            autoPlay
            muted
            playsInline
            className="h-36 w-48 object-cover"
          />
          <div className="flex items-center gap-1.5 bg-slate-950/80 px-2.5 py-1 text-[10px] uppercase tracking-wider text-cyan-300">
            <span className="h-2 w-2 animate-pulse rounded-full bg-cyan-400" />
            Vision Active
          </div>
        </div>
      )}

      {/* Floating Glassmorphic Media Dock */}
      <div className="fixed bottom-6 left-1/2 z-20 -translate-x-1/2">
        <div className="flex items-center gap-3 rounded-full border border-slate-700/50 bg-slate-900/70 px-5 py-3 shadow-2xl backdrop-blur-2xl">
          <button
            onClick={connected || connecting ? stopCall : startCall}
            disabled={!hosted && connecting}
            aria-label={connected || connecting ? "End Voice Session" : "Start Voice Session"}
            className={`flex h-12 w-12 items-center justify-center rounded-full transition ${
              connected
                ? "bg-rose-600 text-white shadow-lg shadow-rose-600/30 hover:bg-rose-500"
                : "bg-gradient-to-r from-cyan-500 to-blue-600 text-white shadow-lg shadow-cyan-500/30 hover:from-cyan-400 hover:to-blue-500"
            }`}
          >
            {connected ? <PhoneOff className="h-5 w-5" /> : <Phone className="h-5 w-5" />}
          </button>

          <button
            onClick={toggleMic}
            disabled={hosted && connecting}
            aria-label={micEnabled ? "Mute Microphone" : "Unmute Microphone"}
            className={`flex h-11 w-11 items-center justify-center rounded-full border transition ${
              micEnabled
                ? "border-slate-600/60 bg-slate-800/80 text-white hover:bg-slate-700"
                : "border-rose-500/50 bg-rose-500/20 text-rose-300"
            }`}
          >
            {micEnabled ? <Mic className="h-4 w-4" /> : <MicOff className="h-4 w-4" />}
          </button>

          <button
            onClick={toggleSpeaker}
            disabled={hosted && connecting}
            aria-label={speakerEnabled ? "Mute Speaker" : "Unmute Speaker"}
            className={`flex h-11 w-11 items-center justify-center rounded-full border transition ${
              speakerEnabled
                ? "border-slate-600/60 bg-slate-800/80 text-white hover:bg-slate-700"
                : "border-amber-500/50 bg-amber-500/20 text-amber-300"
            }`}
          >
            {speakerEnabled ? (
              <Volume2 className="h-4 w-4" />
            ) : (
              <VolumeX className="h-4 w-4" />
            )}
          </button>

          <button
            onClick={toggleCamera}
            disabled={!connected || hosted}
            aria-label={cameraEnabled ? "Disable Camera" : "Enable Camera"}
            className={`flex h-11 w-11 items-center justify-center rounded-full border transition disabled:opacity-40 ${
              cameraEnabled
                ? "border-cyan-400/60 bg-cyan-500/20 text-cyan-200"
                : "border-slate-600/60 bg-slate-800/80 text-white hover:bg-slate-700"
            }`}
          >
            {cameraEnabled ? (
              <Video className="h-4 w-4" />
            ) : (
              <VideoOff className="h-4 w-4" />
            )}
          </button>
        </div>
      </div>
    </main>
  );
}
