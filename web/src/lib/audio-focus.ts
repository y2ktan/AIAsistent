export interface AudioFocusGraph {
  stream: MediaStream;
  analyser: AnalyserNode | null;
  gate: GainNode | null;
  close: () => void;
}

export interface VADDecision {
  event: "start" | "end" | null;
  speaking: boolean;
  noiseFloor: number;
  startThreshold: number;
  holdThreshold: number;
}

export function buildMicrophoneConstraints(audioFocusEnabled: boolean): MediaTrackConstraints {
  const constraints: MediaTrackConstraints = {
    echoCancellation: true,
    noiseSuppression: true,
    autoGainControl: true,
  };
  if (audioFocusEnabled) {
    (constraints as MediaTrackConstraints & { voiceIsolation: boolean }).voiceIsolation = true;
  }
  return constraints;
}

export function createAudioFocusGraph(
  context: AudioContext,
  input: MediaStream,
  enabled: boolean
): AudioFocusGraph {
  if (!enabled || typeof context.createMediaStreamDestination !== "function") {
    return { stream: input, analyser: null, gate: null, close: () => {} };
  }

  try {
    const source = context.createMediaStreamSource(input);
    const highpass = context.createBiquadFilter();
    const presence = context.createBiquadFilter();
    const lowpass = context.createBiquadFilter();
    const analyser = context.createAnalyser();
    const compressor = context.createDynamicsCompressor();
    const gate = context.createGain();
    const destination = context.createMediaStreamDestination();

    highpass.type = "highpass";
    highpass.frequency.value = 95;
    highpass.Q.value = 0.707;

    presence.type = "peaking";
    presence.frequency.value = 1800;
    presence.gain.value = 2.5;
    presence.Q.value = 1;

    lowpass.type = "lowpass";
    lowpass.frequency.value = 3800;
    lowpass.Q.value = 0.707;

    analyser.fftSize = 256;

    compressor.threshold.value = -24;
    compressor.knee.value = 18;
    compressor.ratio.value = 3;
    compressor.attack.value = 0.005;
    compressor.release.value = 0.15;

    gate.gain.value = 0.08;
    source.connect(highpass);
    highpass.connect(presence);
    presence.connect(lowpass);
    lowpass.connect(analyser);
    analyser.connect(compressor);
    compressor.connect(gate);
    gate.connect(destination);

    return {
      stream: destination.stream,
      analyser,
      gate,
      close: () => {
        source.disconnect();
        highpass.disconnect();
        presence.disconnect();
        lowpass.disconnect();
        analyser.disconnect();
        compressor.disconnect();
        gate.disconnect();
      },
    };
  } catch {
    return { stream: input, analyser: null, gate: null, close: () => {} };
  }
}

export function setAudioFocusSpeechGate(
  graph: AudioFocusGraph,
  speaking: boolean,
  context: AudioContext
) {
  if (!graph.gate) return;
  const target = speaking ? 1 : 0.08;
  graph.gate.gain.cancelScheduledValues(context.currentTime);
  graph.gate.gain.setTargetAtTime(target, context.currentTime, speaking ? 0.008 : 0.04);
}

export const VAD_SPEECH_HOLD_MS = 2500;

export class AdaptiveVAD {
  private noiseFloor = 0.015;
  private speaking = false;
  private onsetTicks = 0;
  private lastSpeechAt = 0;
  private calibrationTicks = 0;

  constructor(
    private readonly enabled = true,
    private readonly speechHoldMs = VAD_SPEECH_HOLD_MS
  ) {}

  update(rms: number, nowMs: number): VADDecision {
    const startThreshold = this.enabled
      ? Math.max(0.03, this.noiseFloor * 2.5 + 0.015)
      : 0.03;
    const holdThreshold = this.enabled
      ? Math.max(0.022, this.noiseFloor * 1.8 + 0.008)
      : 0.03;

    let event: VADDecision["event"] = null;

    if (!this.speaking) {
      if (this.calibrationTicks < 10) {
        this.updateNoiseFloor(rms);
        this.calibrationTicks += 1;
      } else if (rms > startThreshold) {
        this.onsetTicks += 1;
        if (this.onsetTicks >= 3) {
          this.speaking = true;
          this.lastSpeechAt = nowMs;
          this.onsetTicks = 0;
          event = "start";
        }
      } else {
        this.onsetTicks = 0;
        this.updateNoiseFloor(rms);
      }
    } else if (rms > holdThreshold) {
      this.lastSpeechAt = nowMs;
    } else if (nowMs - this.lastSpeechAt >= this.speechHoldMs) {
      this.speaking = false;
      this.onsetTicks = 0;
      event = "end";
      this.updateNoiseFloor(rms);
    }

    return {
      event,
      speaking: this.speaking,
      noiseFloor: this.noiseFloor,
      startThreshold,
      holdThreshold,
    };
  }

  private updateNoiseFloor(rms: number) {
    if (!this.enabled) return;
    const clamped = Math.max(0.008, Math.min(0.12, rms));
    const alpha = clamped < this.noiseFloor ? 0.15 : 0.04;
    this.noiseFloor += (clamped - this.noiseFloor) * alpha;
  }
}
