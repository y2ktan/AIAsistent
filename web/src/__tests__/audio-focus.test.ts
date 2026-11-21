import { describe, expect, it } from "vitest";
import { AdaptiveVAD, VAD_SPEECH_HOLD_MS, buildMicrophoneConstraints } from "@/lib/audio-focus";

describe("audio focus", () => {
  it("adds voice isolation constraints only when enabled", () => {
    expect(buildMicrophoneConstraints(true)).toMatchObject({
      echoCancellation: true,
      noiseSuppression: true,
      autoGainControl: true,
      voiceIsolation: true,
    });
    expect(buildMicrophoneConstraints(false)).not.toHaveProperty("voiceIsolation");
  });

  it("does not treat steady noisy-room ambience as speech", () => {
    const vad = new AdaptiveVAD(true);
    for (let tick = 0; tick < 200; tick++) {
      const decision = vad.update(0.05, tick * 50);
      expect(decision.event).toBeNull();
      expect(decision.speaking).toBe(false);
    }
  });

  it("detects near-field speech onset and ends the turn after its 2500ms hangover", () => {
    expect(VAD_SPEECH_HOLD_MS).toBe(2500);
    const vad = new AdaptiveVAD(true);
    for (let tick = 0; tick < 30; tick++) vad.update(0.05, tick * 50);

    const onset = [1550, 1600, 1650]
      .map((now) => vad.update(0.22, now).event)
      .filter(Boolean);
    expect(onset).toEqual(["start"]);

    for (let tick = 0; tick < 49; tick++) {
      const decision = vad.update(0.05, 1700 + tick * 50);
      expect(decision.event).toBeNull();
      expect(decision.speaking).toBe(true);
    }
    expect(vad.update(0.05, 4150).event).toBe("end");
  });

  it("keeps a speech segment open across thinking pauses shorter than 2500ms", () => {
    const vad = new AdaptiveVAD(true);
    for (let tick = 0; tick < 20; tick++) vad.update(0.03, tick * 50);

    expect(vad.update(0.22, 1050).event).toBeNull();
    expect(vad.update(0.22, 1100).event).toBeNull();
    expect(vad.update(0.22, 1150).event).toBe("start");

    // 1800ms pause (< 2500ms) -> segment stays open
    for (let now = 1200; now <= 2950; now += 50) {
      const decision = vad.update(0.02, now);
      expect(decision.event).toBeNull();
      expect(decision.speaking).toBe(true);
    }

    // Speaker resumes thinking out loud -> refreshes hold timer without re-emitting start
    const resume = vad.update(0.22, 3000);
    expect(resume.event).toBeNull();
    expect(resume.speaking).toBe(true);

    // Silence >= 2500ms from 3000ms -> emits end at 5500ms
    expect(vad.update(0.02, 5450).event).toBeNull();
    expect(vad.update(0.02, 5500).event).toBe("end");
  });
});
