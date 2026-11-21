import { describe, expect, it } from "vitest";
import { AdaptiveVAD, buildMicrophoneConstraints } from "@/lib/audio-focus";

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

  it("detects near-field speech onset and ends the turn after its hangover", () => {
    const vad = new AdaptiveVAD(true);
    for (let tick = 0; tick < 30; tick++) vad.update(0.05, tick * 50);

    const onset = [1550, 1600, 1650]
      .map((now) => vad.update(0.22, now).event)
      .filter(Boolean);
    expect(onset).toEqual(["start"]);

    for (let tick = 0; tick < 17; tick++) vad.update(0.05, 1700 + tick * 50);
    expect(vad.update(0.05, 2600).event).toBe("end");
  });
});
