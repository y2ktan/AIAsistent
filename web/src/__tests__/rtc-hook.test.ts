import { act, createElement, type ReactNode } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const rtcState = vi.hoisted(() => ({
  connected: true,
  connecting: false,
  agentState: "thinking" as const,
  assistantName: "Aegis",
  micEnabled: true,
  speakerEnabled: true,
  cameraEnabled: false,
  transcripts: [] as Array<{
    id: string;
    role: "user" | "assistant";
    text: string;
    isPartial?: boolean;
  }>,
  citations: [] as Array<{
    documentId: string;
    documentTitle: string;
    chunkIndex: number;
    content: string;
    similarity: number;
  }>,
  activeTool: "load_skill" as string | null,
  errorMsg: null as string | null,
  permissionPrompt: null as string | null,
  videoPreviewRef: { current: null },
  remoteAudioRef: { current: null },
  startCall: vi.fn(),
  stopCall: vi.fn(),
  toggleMic: vi.fn(),
  toggleSpeaker: vi.fn(),
  toggleCamera: vi.fn(),
  getAudioEnergy: vi.fn(() => ({ micLevel: 0, speakerLevel: 0 })),
}));
const router = vi.hoisted(() => ({ push: vi.fn() }));
const session = vi.hoisted(() => ({
  role: "user" as "user" | "admin",
}));

vi.mock("next/navigation", () => ({
  useRouter: () => router,
}));

vi.mock("next/link", () => ({
  default: ({ children }: { children: ReactNode }) => createElement("a", null, children),
}));

vi.mock("@/lib/api-client", () => ({
  apiFetch: vi.fn(),
  fetchSession: vi.fn(async () => ({
    user: { id: "user-1", email: "user@example.com", role: session.role },
  })),
}));

vi.mock("@/components/LiquidPlasmaOrb", () => ({
  LiquidPlasmaOrb: () => createElement("div", { "data-testid": "orb" }),
}));

vi.mock("@/hooks/useVoiceAgentRTC", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/hooks/useVoiceAgentRTC")>();
  return {
    ...actual,
    useVoiceAgentRTC: () => rtcState,
  };
});

import HomePage from "@/app/page";
import { apiFetch } from "@/lib/api-client";
import {
  filterVisibleCitations,
  isAgentSkillCitation,
  normalizeAssistantName,
  parseRTCEvent,
} from "@/hooks/useVoiceAgentRTC";
import { buildMicrophoneConstraints } from "@/lib/audio-focus";

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

describe("Agent Skill citation filtering", () => {
  const skillCitation = {
    documentId: "skill-1",
    documentTitle: "SKILL.md",
    chunkIndex: 0,
    content: "---\nname: web-search\ndescription: Search the web.\n---\n\nUse web search.",
    similarity: 0.44,
  };

  it("hides canonical and frontmatter-based Agent Skill citations only", () => {
    const citations = [
      skillCitation,
      {
        ...skillCitation,
        documentId: "skill-2",
        documentTitle: "incident-review.md",
      },
      {
        documentId: "guide-1",
        documentTitle: "Guide.pdf",
        chunkIndex: 0,
        content: "Ordinary knowledge base content.",
        similarity: 0.9,
      },
      {
        documentId: "guide-2",
        documentTitle: "Onboarding.md",
        chunkIndex: 0,
        content: "# Onboarding\n\nWelcome to the system.",
        similarity: 0.82,
      },
      {
        documentId: "not-a-skill",
        documentTitle: "Skill.pdf",
        chunkIndex: 0,
        content: skillCitation.content,
        similarity: 0.75,
      },
    ];

    expect(isAgentSkillCitation(skillCitation)).toBe(true);
    expect(filterVisibleCitations(citations).map((citation) => citation.documentTitle)).toEqual([
      "Guide.pdf",
      "Onboarding.md",
      "Skill.pdf",
    ]);
  });

  describe("RTC Audio Focus capture", () => {
    it("enables voice isolation for the default-on Audio Focus capture path", () => {
      expect(buildMicrophoneConstraints(true)).toMatchObject({
        echoCancellation: true,
        noiseSuppression: true,
        autoGainControl: true,
        voiceIsolation: true,
      });
    });
  });

  it("returns an empty list when an event contains only Agent Skill citations", () => {
    expect(filterVisibleCitations([skillCitation])).toEqual([]);
  });
});

describe("home-page citation rendering", () => {
  let container: HTMLDivElement;
  let root: ReturnType<typeof createRoot>;

  beforeEach(() => {
    vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
    session.role = "user";
    rtcState.assistantName = "Aegis";
    rtcState.transcripts = [];
    rtcState.citations = [
      {
        documentId: "skill-1",
        documentTitle: "SKILL.md",
        chunkIndex: 0,
        content: "---\nname: web-search\ndescription: Search the web.\n---\n\nUse web search.",
        similarity: 0.44,
      },
      {
        documentId: "guide-1",
        documentTitle: "User Guide.pdf",
        chunkIndex: 0,
        content: "The supported workflow is documented here.",
        similarity: 0.91,
      },
    ];
  });

  afterEach(async () => {
    if (root) {
      await act(async () => root.unmount());
    }
    container?.remove();
    vi.unstubAllGlobals();
  });

  it("omits citation badges and excerpts for non-admin users while preserving tool activity", async () => {
    container = document.createElement("div");
    document.body.append(container);
    root = createRoot(container);

    await act(async () => {
      root.render(createElement(HomePage));
    });

    expect(container.textContent).not.toContain("User Guide.pdf");
    expect(container.textContent).not.toContain("The supported workflow is documented here.");
    expect(container.textContent).not.toContain("SKILL.md");
    expect(container.textContent).toContain("load_skill");
  });

  it("renders only non-skill citation badges and excerpts for administrators", async () => {
    session.role = "admin";
    container = document.createElement("div");
    document.body.append(container);
    root = createRoot(container);

    await act(async () => {
      root.render(createElement(HomePage));
    });

    expect(container.textContent).toContain("User Guide.pdf");
    expect(container.textContent).not.toContain("SKILL.md");
    expect(container.textContent).toContain("load_skill");

    const citationButton = Array.from(container.querySelectorAll("button")).find((button) =>
      button.textContent?.includes("User Guide.pdf")
    );
    expect(citationButton).toBeDefined();

    await act(async () => {
      citationButton?.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    });

    expect(container.textContent).toContain("The supported workflow is documented here.");
  });

  it("renders configured English or Chinese assistantName in header and assistant transcript labels", async () => {
    expect(normalizeAssistantName(undefined)).toBe("Aegis");
    expect(normalizeAssistantName("   ")).toBe("Aegis");
    expect(normalizeAssistantName("  小智  ")).toBe("小智");

    rtcState.assistantName = "小智";
    rtcState.transcripts = [
      { id: "t1", role: "user", text: "小智，现在几点？" },
      { id: "t2", role: "assistant", text: "现在是下午三点。" },
    ];

    container = document.createElement("div");
    document.body.append(container);
    root = createRoot(container);

    await act(async () => {
      root.render(createElement(HomePage));
    });

    const headerName = container.querySelector('[data-testid="assistant-header-name"]');
    expect(headerName?.textContent).toBe("小智");

    const speakerLabels = Array.from(
      container.querySelectorAll('[data-testid="transcript-speaker-label"]')
    ).map((el) => el.textContent);
    expect(speakerLabels).toEqual(["You", "小智"]);
    expect(container.textContent).not.toContain("Aegis");
  });

  it("loads assistantName from /api/v1/rtc/config inside useVoiceAgentRTC", async () => {
    const actualHookModule = await vi.importActual<typeof import("@/hooks/useVoiceAgentRTC")>(
      "@/hooks/useVoiceAgentRTC"
    );
    vi.mocked(apiFetch).mockImplementationOnce(async (path: string) => {
      expect(path).toBe("/api/v1/rtc/config");
      return { assistantName: "Jarvis", audioFocusEnabled: true };
    });

    function Probe() {
      const { assistantName } = actualHookModule.useVoiceAgentRTC();
      return createElement("div", { "data-testid": "probe-name" }, assistantName);
    }

    container = document.createElement("div");
    document.body.append(container);
    root = createRoot(container);

    await act(async () => {
      root.render(createElement(Probe));
    });

    expect(container.querySelector('[data-testid="probe-name"]')?.textContent).toBe("Jarvis");
  });

  it("shows rejected audio playback and allows a speaker-toggle retry", async () => {
    const actualHookModule = await vi.importActual<typeof import("@/hooks/useVoiceAgentRTC")>(
      "@/hooks/useVoiceAgentRTC"
    );
    vi.mocked(apiFetch).mockResolvedValue({ assistantName: "Aegis" });
    const play = vi.spyOn(HTMLMediaElement.prototype, "play")
      .mockRejectedValueOnce(new DOMException("Playback blocked", "NotAllowedError"))
      .mockResolvedValue(undefined);

    function Probe() {
      const { remoteAudioRef, toggleSpeaker, errorMsg } = actualHookModule.useVoiceAgentRTC();
      return createElement("div", null,
        createElement("audio", { ref: remoteAudioRef }),
        createElement("button", { onClick: toggleSpeaker }, "Toggle speaker"),
        createElement("div", { "data-testid": "playback-error" }, errorMsg)
      );
    }

    container = document.createElement("div");
    document.body.append(container);
    root = createRoot(container);
    try {
      await act(async () => root.render(createElement(Probe)));
      Object.defineProperty(container.querySelector("audio"), "srcObject", {
        value: { id: "remote-stream" },
        writable: true,
      });
      const button = container.querySelector("button")!;
      await act(async () => {
        button.dispatchEvent(new MouseEvent("click", { bubbles: true }));
        button.dispatchEvent(new MouseEvent("click", { bubbles: true }));
      });
      expect(play).toHaveBeenCalledTimes(1);
      expect(container.querySelector('[data-testid="playback-error"]')?.textContent)
        .toContain("browser could not play");
      await act(async () => {
        button.dispatchEvent(new MouseEvent("click", { bubbles: true }));
        button.dispatchEvent(new MouseEvent("click", { bubbles: true }));
      });
      expect(play).toHaveBeenCalledTimes(2);
      expect(container.querySelector('[data-testid="playback-error"]')?.textContent).toBe("");
    } finally {
      play.mockRestore();
    }
  });
});
