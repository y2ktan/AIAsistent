import { describe, it, expect } from "vitest";
import { parseRTCEvent } from "@/hooks/useVoiceAgentRTC";

describe("parseRTCEvent", () => {
  it("parses valid state and citation events", () => {
    const stateEv = parseRTCEvent(JSON.stringify({ type: "state", state: "speaking" }));
    expect(stateEv?.type).toBe("state");
    expect(stateEv?.state).toBe("speaking");

    const citEv = parseRTCEvent(
      JSON.stringify({
        type: "citation",
        citations: [{ documentId: "d1", documentTitle: "Guide.pdf", chunkIndex: 0, content: "Hello", similarity: 0.9 }],
      })
    );
    expect(citEv?.citations).toHaveLength(1);
    expect(citEv?.citations?.[0].documentTitle).toBe("Guide.pdf");
  });

  it("returns null for malformed JSON or missing type", () => {
    expect(parseRTCEvent("not-json")).toBeNull();
    expect(parseRTCEvent(JSON.stringify({ state: "listening" }))).toBeNull();
  });
});
