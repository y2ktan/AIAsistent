import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import { beforeEach, describe, expect, it, vi } from "vitest";

const media = vi.hoisted(() => ({
  order: [] as string[], volume: vi.fn(), cleanup: vi.fn(),
  muted: false, stop: vi.fn(), publish: vi.fn(),
  mute: vi.fn(async () => {}), disconnect: vi.fn(),
  pendingTrack: undefined as Promise<{ mute: () => Promise<void>; stop: () => void }> | undefined,
}));
vi.mock("livekit-client", () => {
  const events = { Connected: "connected", SignalConnected: "signal", Disconnected: "disconnected" };
  class Room {
    listeners = new Map<string, Array<() => void>>();
    name = "conv_fixture";
    state = "connected";
    localParticipant = {
      audioTrackPublications: new Map<string, unknown>(),
      setMicrophoneEnabled: vi.fn(async () => { media.order.push("unmuted"); }),
      publishTrack: vi.fn(async (track: { stop: () => void }) => {
        media.order.push("publish");
        media.publish(media.muted);
        this.localParticipant.audioTrackPublications.set("microphone", { track });
      }),
      publishData: vi.fn(async () => {}),
      getTrackPublication: () => undefined,
    };
    on(event: string, callback: () => void) {
      this.listeners.set(event, [...(this.listeners.get(event) || []), callback]);
    }
    once(event: string, callback: () => void) { this.on(event, callback); }
    off() {}
    async connect() {
      for (const callback of this.listeners.get(events.SignalConnected) || []) callback();
      for (const callback of this.listeners.get(events.Connected) || []) callback();
    }
    disconnect() { media.disconnect(); }
  }
  return {
    Room, RoomEvent: events, Track: { Source: { Microphone: "microphone" }, Kind: {} },
    ConnectionState: { Connected: "connected" }, DisconnectReason: {},
    createLocalAudioTrack: async () => media.pendingTrack || ({
      mute: async () => {
        await media.mute();
        media.muted = true;
        media.order.push("mute");
      },
      stop: media.stop,
    }),
  };
});
vi.mock("../../node_modules/@elevenlabs/client/dist/WebRTCAudioAdapter.js", () => ({
  createAudioAdapter: () => ({ setVolume: media.volume, cleanup: media.cleanup }),
}));
vi.mock("../../node_modules/@elevenlabs/client/dist/utils/ConnectionFactory.js", () => ({
  createConnection: async () => ({}),
}));
vi.mock("../../node_modules/@elevenlabs/client/dist/platform/VoiceSessionSetup.js", () => ({
  setSetupStrategy: vi.fn(),
  setupWebRTCSession: () => ({ connection: {}, detach: async () => {} }),
}));
import { WebRTCConnection } from "../../node_modules/@elevenlabs/client/dist/utils/WebRTCConnection.js";
import { WebAudioAdapter } from "../../node_modules/@elevenlabs/client/dist/platform/web/webAudioAdapter.js";
import { webSessionSetup } from "../../node_modules/@elevenlabs/client/dist/platform/web/VoiceSessionSetup.js";

describe("pinned ElevenLabs SDK media-safety patch", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    media.order = [];
    media.muted = false;
    media.pendingTrack = undefined;
  });
  it("mutes before publishing and releases tracks even after disconnection", async () => {
    const connection = await WebRTCConnection.create({
      conversationToken: "fixture", initialMicMuted: true, initialVolume: 0,
    });
    expect(media.order).toEqual(["mute", "publish"]);
    expect(media.publish).toHaveBeenCalledWith(true);
    expect(media.volume).toHaveBeenCalledWith(0);
    connection.close();
    expect(media.stop).toHaveBeenCalled();
    expect(media.cleanup).toHaveBeenCalled();
    expect(media.disconnect).toHaveBeenCalled();
  });
  it("does not publish when initial track muting fails", async () => {
    media.mute.mockRejectedValueOnce(new Error("mute failed"));
    await expect(WebRTCConnection.create({
      conversationToken: "fixture", initialMicMuted: true,
    })).rejects.toThrow("mute failed");
    expect(media.publish).not.toHaveBeenCalled();
    expect(media.stop).toHaveBeenCalled();
    expect(media.disconnect).toHaveBeenCalled();
  });
  it("stops late microphone capture instead of publishing after cancellation", async () => {
    let resolve!: (value: { mute: () => Promise<void>; stop: () => void }) => void;
    media.pendingTrack = new Promise((done) => { resolve = done; });
    const controller = new AbortController();
    const pending = WebRTCConnection.create({ conversationToken: "fixture", sessionSignal: controller.signal });
    const result = expect(pending).rejects.toThrow();
    controller.abort();
    resolve({ mute: media.mute, stop: media.stop });
    await result;
    expect(media.publish).not.toHaveBeenCalled();
    expect(media.stop).toHaveBeenCalled();
  });
  it("does not open a second preliminary microphone stream for private WebRTC", async () => {
    const getUserMedia = vi.fn();
    const original = Object.getOwnPropertyDescriptor(navigator, "mediaDevices");
    Object.defineProperty(navigator, "mediaDevices", { configurable: true, value: { getUserMedia } });
    try {
      await webSessionSetup({ conversationToken: "fixture", clientTools: {}, useWakeLock: false });
      expect(getUserMedia).not.toHaveBeenCalled();
    } finally {
      if (original) Object.defineProperty(navigator, "mediaDevices", original);
      else Reflect.deleteProperty(navigator, "mediaDevices");
    }
  });
  it("sets speaker volume before attachment and detects rejected playback", async () => {
    const adapter = new WebAudioAdapter();
    adapter.setVolume(0);
    const attach = vi.fn((element: HTMLAudioElement) => {
      expect(element.volume).toBe(0);
      return element;
    });
    const play = vi.spyOn(HTMLMediaElement.prototype, "play").mockRejectedValue(new DOMException("blocked", "NotAllowedError"));
    await expect(Reflect.apply(adapter.attachRemoteTrack, adapter, [{ attach }, null])).rejects.toThrow("blocked");
    expect(attach).toHaveBeenCalledTimes(1);
    expect(document.querySelectorAll("audio")).toHaveLength(1);
    adapter.cleanup();
    expect(document.querySelectorAll("audio")).toHaveLength(0);
    play.mockRestore();
  });
  it("captures output at the provider sample rate without resampler control messages", async () => {
    const audio = vi.fn();
    const processorErrors: unknown[] = [];
    let processAudio!: (inputs: Float32Array[][]) => boolean;
    const contextOptions = vi.fn();
    const port: {
      onmessage: ((event: { data: unknown }) => void) | null;
      postMessage: (data: unknown) => void;
    } = { onmessage: null, postMessage: () => {} };
    class Context {
      sampleRate: number;
      audioWorklet = { addModule: vi.fn(async () => {}) };
      constructor(options?: AudioContextOptions) {
        contextOptions(options);
        this.sampleRate = options?.sampleRate ?? 48_000;
      }
      createAnalyser() {
        return { connect: vi.fn(), frequencyBinCount: 1024, getFloatTimeDomainData: vi.fn() };
      }
      createMediaStreamSource() { return { connect: vi.fn() }; }
      async close() {}
    }
    vi.stubGlobal("AudioContext", Context);
    vi.stubGlobal("MediaStream", class {});
    vi.stubGlobal("AudioWorkletNode", class {
      port = port;
      constructor(context: Context) {
        runInNewContext(readFileSync("public/elevenlabs/rawAudioProcessor.js", "utf8"), {
          sampleRate: context.sampleRate,
          AudioWorkletProcessor: class {
            port = {
              onmessage: null,
              postMessage: (data: unknown) => {
                try { port.onmessage?.({ data }); } catch (error) { processorErrors.push(error); }
              },
            };
          },
          registerProcessor: (_name: string, Processor: new () => {
            port: { onmessage: (event: { data: unknown }) => void };
            process: typeof processAudio;
          }) => {
            const processor = new Processor();
            port.postMessage = (data) => processor.port.onmessage({ data });
            processAudio = processor.process.bind(processor);
          },
        });
      }
    });
    const adapter = new WebAudioAdapter();
    try {
      await adapter.setupOutputAnalysis(
        { mediaStreamTrack: {} } as Parameters<WebAudioAdapter["setupOutputAnalysis"]>[0],
        { format: "pcm", sampleRate: 16_000 },
        audio,
        { rawAudioProcessor: "/elevenlabs/rawAudioProcessor.js" },
      );
      await Promise.resolve();
      processAudio([[new Float32Array(400).fill(0.25)]]);
      expect(processorErrors).toEqual([]);
      expect(audio).toHaveBeenCalledTimes(1);
      expect(new Int16Array(audio.mock.calls[0][0])).toHaveLength(400);
      expect(audio.mock.calls[0][1]).toBeCloseTo(0.25);
      expect(contextOptions).toHaveBeenCalledWith({ sampleRate: 16_000 });
    } finally {
      adapter.cleanup();
      vi.unstubAllGlobals();
    }
  });
});
