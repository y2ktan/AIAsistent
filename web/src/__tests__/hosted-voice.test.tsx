import { act, type ReactNode } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { useConversation } from "@elevenlabs/react";

type Options = NonNullable<Parameters<ReturnType<typeof useConversation>["startSession"]>[0]>;
const mocks = vi.hoisted(() => ({
  api: vi.fn(),
  options: undefined as Options | undefined,
  start: vi.fn(),
  end: vi.fn(),
  mute: vi.fn(),
  volume: vi.fn(),
  backendStart: vi.fn(async () => {}),
  backendStop: vi.fn(),
  backendHook: vi.fn(),
  provider: vi.fn(),
}));

vi.mock("@/lib/api-client", () => ({ apiFetch: mocks.api }));
vi.mock("@elevenlabs/react", () => ({
  ConversationProvider: ({ children }: { children: ReactNode }) => {
    mocks.provider();
    return children;
  },
  useConversation: () => ({
    startSession: mocks.start, endSession: mocks.end, setMuted: mocks.mute,
    setVolume: mocks.volume, getInputVolume: () => 0.4, getOutputVolume: () => 0.7,
    isSpeaking: false,
  }),
}));
vi.mock("@/hooks/useVoiceAgentRTC", async (original) => ({
  ...await original<typeof import("@/hooks/useVoiceAgentRTC")>(),
  useVoiceAgentRTC: mocks.backendHook,
}));

import { useHostedVoiceAgent } from "@/hooks/useHostedVoiceAgent";
import { VoiceSessionRouter } from "@/components/VoiceSessionRouter";

describe("hosted voice lifecycle", () => {
  let container: HTMLDivElement;
  let root: ReturnType<typeof createRoot>;
  let voice: ReturnType<typeof useHostedVoiceAgent>;
  const access = { conversationToken: "transient-token", conversationId: "conv_fixture", mode: "elevenlabs_agent" };
  function Probe() {
    voice = useHostedVoiceAgent("Policy Assistant");
    return <div>{JSON.stringify({ connected: voice.connected, connecting: voice.connecting, transcripts: voice.transcripts, error: voice.errorMsg })}</div>;
  }
  async function render() {
    await act(async () => root.render(<Probe />));
  }
  async function start() {
    await act(async () => voice.startCall());
  }
  async function connect() {
    await act(async () => mocks.options?.onConnect?.({ conversationId: "conv_fixture" }));
  }
  beforeEach(() => {
    vi.clearAllMocks();
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
    mocks.options = undefined;
    mocks.start.mockImplementation((options: Options) => { mocks.options = options; });
    mocks.api.mockImplementation(async (path: string) =>
      path.endsWith("/auth/me") ? { user: { isActive: true } } : access);
    container = document.createElement("div");
    document.body.append(container);
    root = createRoot(container);
  });
  afterEach(async () => {
    await act(async () => root.unmount());
    container.remove();
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("keeps pre-call mute independent and merges streamed/final/corrected responses", async () => {
    await render();
    await act(async () => { voice.toggleMic(); voice.toggleSpeaker(); });
    expect(mocks.mute).not.toHaveBeenCalled();
    expect(mocks.volume).not.toHaveBeenCalled();
    await start();
    expect(mocks.options).toMatchObject({
      conversationToken: "transient-token", connectionType: "webrtc",
      initialMicMuted: true, initialVolume: 0,
      workletPaths: { rawAudioProcessor: "/elevenlabs/rawAudioProcessor.js" },
    });
    expect(mocks.options).not.toHaveProperty("overrides");
    expect(mocks.options).not.toHaveProperty("clientTools");
    await connect();
    expect(voice.connected).toBe(true);
    expect(voice.getAudioEnergy()).toEqual({ micLevel: 0, speakerLevel: 0 });
    await act(async () => voice.toggleSpeaker());
    expect(voice.micEnabled).toBe(false);
    expect(voice.speakerEnabled).toBe(true);
    expect(voice.getAudioEnergy()).toEqual({ micLevel: 0, speakerLevel: 0.7 });
    await act(async () => voice.toggleMic());
    expect(voice.micEnabled).toBe(true);
    expect(voice.speakerEnabled).toBe(true);
    expect(mocks.mute).toHaveBeenLastCalledWith(false);
    expect(voice.getAudioEnergy()).toEqual({ micLevel: 0.4, speakerLevel: 0.7 });
    await act(async () => {
      mocks.options?.onMessage?.({ role: "user", source: "user", event_id: 1, message: "What is the policy?" });
      mocks.options?.onAgentChatResponsePart?.({ event_id: 2, response_id: "answer", type: "start", text: "Thirty" });
      mocks.options?.onAgentChatResponsePart?.({ event_id: 2, response_id: "answer", type: "delta", text: " days" });
      mocks.options?.onMessage?.({ role: "agent", source: "ai", event_id: 2, response_id: "answer", message: "Thirty days." });
      mocks.options?.onAgentResponseCorrection?.({ event_id: 2, response_id: "answer", original_agent_response: "Thirty days.", corrected_agent_response: "Thirty" });
    });
    expect(voice.transcripts).toHaveLength(2);
    expect(voice.transcripts[1]).toMatchObject({ text: "Thirty", isPartial: false });
    expect(voice.agentState).toBe("listening");
    expect(voice.citations).toEqual([]);
    expect(voice.cameraEnabled).toBe(false);
    expect(voice.activeTool).toBeNull();
    expect(mocks.api.mock.calls.map(([path]) => path)).toEqual(["/api/v1/elevenlabs/conversation-token"]);
    expect(localStorage.length).toBe(0);
    expect(sessionStorage.length).toBe(0);
  });

  it("cancels token acquisition and ignores its late result", async () => {
    let resolve!: (value: typeof access) => void;
    mocks.api.mockImplementationOnce(() => new Promise<typeof access>((done) => { resolve = done; }));
    await render();
    let pending!: Promise<void>;
    await act(async () => { pending = voice.startCall(); });
    const signal = mocks.api.mock.calls[0][1].signal as AbortSignal;
    await act(async () => voice.stopCall());
    expect(signal.aborted).toBe(true);
    await act(async () => { resolve(access); await pending; });
    expect(mocks.start).not.toHaveBeenCalled();
    expect(voice.connected).toBe(false);
    expect(voice.connecting).toBe(false);
  });

  it("bounds unavailable token acquisition and cleans up its deadline", async () => {
    vi.useFakeTimers();
    mocks.api.mockImplementationOnce(() => new Promise(() => {}));
    await render();
    await act(async () => { void voice.startCall(); });
    await act(async () => vi.advanceTimersByTimeAsync(20_000));
    expect(voice.errorMsg).toContain("access timed out");
    expect(voice.connecting).toBe(false);
    expect(mocks.start).not.toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(0);
  });

  it("serializes repeated starts and tears down late SDK connection success", async () => {
    await render();
    await act(async () => Promise.all([voice.startCall(), voice.startCall()]));
    expect(mocks.start).toHaveBeenCalledTimes(1);
    await act(async () => voice.stopCall());
    await connect();
    expect(mocks.end).toHaveBeenCalledTimes(2);
    expect(voice.connected).toBe(false);
    await act(async () => mocks.options?.onStatusChange?.({ status: "disconnected" }));
    expect(voice.connecting).toBe(false);
    await start();
    expect(mocks.start).toHaveBeenCalledTimes(2);
  });

  it.each(["rejected", "inactive"])("ends a call on %s auth validation and removes polling", async (failure) => {
    vi.useFakeTimers();
    await render();
    await start();
    await connect();
    mocks.api.mockImplementation(async () => {
      if (failure === "rejected") throw new Error("unavailable");
      return { user: { isActive: false } };
    });
    await act(async () => vi.advanceTimersByTimeAsync(30_000));
    expect(mocks.end).toHaveBeenCalled();
    expect(voice.errorMsg).toContain("access could not be verified");
    expect(voice.connected).toBe(false);
    expect(vi.getTimerCount()).toBe(0);
  });

  it("revalidates on foreground return and cleans up on unmount", async () => {
    vi.useFakeTimers();
    await render();
    await start();
    await connect();
    await act(async () => window.dispatchEvent(new Event("focus")));
    expect(mocks.api).toHaveBeenCalledWith("/api/v1/auth/me", expect.objectContaining({ cache: "no-store" }));
    await act(async () => root.unmount());
    expect(mocks.end).toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(0);
  });

  it("surfaces safe playback failures and preserves transcripts after disconnect", async () => {
    await render();
    await start();
    await connect();
    await act(async () => mocks.options?.onMessage?.({ role: "agent", source: "ai", event_id: 1, message: "Policy answer" }));
    await act(async () => mocks.options?.onPlaybackError?.());
    expect(voice.errorMsg).toContain("could not play");
    expect(mocks.end).toHaveBeenCalled();
    await act(async () => mocks.options?.onDisconnect?.({ reason: "user" }));
    expect(voice.transcripts[0].text).toBe("Policy answer");
    expect(voice.connecting).toBe(false);
    expect(container.textContent).not.toContain("transient-token");
  });

  it("explains provider termination before any reply instead of silently resetting", async () => {
    await render();
    await start();
    await connect();
    await act(async () => {
      mocks.options?.onStatusChange?.({ status: "disconnecting" });
      mocks.options?.onStatusChange?.({ status: "disconnected" });
      mocks.options?.onDisconnect?.({ reason: "agent" });
    });
    expect(voice.connected).toBe(false);
    expect(voice.connecting).toBe(false);
    expect(voice.errorMsg).toContain("ElevenLabs ended the call before replying");
    expect(voice.errorMsg).toContain("account credits");
  });

  it("does not label a normal provider hangup after a reply as a failure", async () => {
    await render();
    await start();
    await connect();
    await act(async () => {
      mocks.options?.onMessage?.({ role: "agent", source: "ai", event_id: 1, message: "Goodbye." });
      mocks.options?.onDisconnect?.({ reason: "agent" });
    });
    expect(voice.errorMsg).toBeNull();
    expect(voice.transcripts[0].text).toBe("Goodbye.");
  });

  it("mounts only the selected transport and changes routing on the next start", async () => {
    let mode = "elevenlabs_agent";
    mocks.api.mockImplementation(async (path: string) =>
      path.endsWith("/rtc/config") ? { mode, assistantName: "Policy Assistant" } : access);
    mocks.backendHook.mockReturnValue({
      connected: false, connecting: false, startCall: mocks.backendStart, stopCall: mocks.backendStop,
      errorMsg: null, assistantName: "Backend",
    });
    await act(async () => root.render(<VoiceSessionRouter>{(session, hosted) => {
      voice = session;
      return <div>{hosted ? "Hosted" : "Backend"}</div>;
    }}</VoiceSessionRouter>));
    expect(container.textContent).toBe("Hosted");
    expect(mocks.provider).toHaveBeenCalled();
    expect(mocks.backendHook).not.toHaveBeenCalled();
    await start();
    expect(mocks.start).toHaveBeenCalledTimes(1);
    await connect();
    mode = "cascaded";
    expect(container.textContent).toBe("Hosted");
    await act(async () => {
      voice.stopCall();
      mocks.options?.onDisconnect?.({ reason: "user" });
    });
    await start();
    expect(container.textContent).toBe("Backend");
    expect(mocks.backendStart).toHaveBeenCalledTimes(1);
    expect(mocks.start).toHaveBeenCalledTimes(1);
  });
});
