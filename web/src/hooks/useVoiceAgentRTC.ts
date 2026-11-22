"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { apiFetch } from "@/lib/api-client";
import {
  AdaptiveVAD,
  buildMicrophoneConstraints,
  createAudioFocusGraph,
  setAudioFocusSpeechGate,
  type AudioFocusGraph,
} from "@/lib/audio-focus";

export type AgentState = "idle" | "listening" | "retrieving" | "thinking" | "speaking";

export interface CitationItem {
  documentId: string;
  documentTitle: string;
  chunkIndex: number;
  content: string;
  similarity: number;
}

export interface TranscriptItem {
  id: string;
  role: "user" | "assistant";
  text: string;
  isPartial?: boolean;
}

export interface RTCEventPayload {
  type: string;
  state?: AgentState;
  role?: "user" | "assistant";
  text?: string;
  tool?: string;
  status?: string;
  message?: string;
  citations?: CitationItem[];
  ts?: number;
}

export function isAgentSkillCitation(citation: CitationItem): boolean {
  const normalizedTitle = citation.documentTitle.trim().replace(/\\/g, "/").toLowerCase();
  const basename = normalizedTitle.split("/").pop() || "";
  if (basename === "skill.md" || basename === "skill.markdown") {
    return true;
  }

  const markdownTitle = basename.endsWith(".md") || basename.endsWith(".markdown");
  const untitledDocument = basename !== "" && !basename.includes(".");
  if (!markdownTitle && !untitledDocument) {
    return false;
  }

  const content = citation.content.trimStart();
  if (!content.startsWith("---")) {
    return false;
  }
  const frontmatterEnd = content.indexOf("\n---", 3);
  if (frontmatterEnd < 0) {
    return false;
  }
  const frontmatter = content.slice(3, frontmatterEnd);
  return /^\s*name\s*:/im.test(frontmatter) && /^\s*description\s*:/im.test(frontmatter);
}

export function filterVisibleCitations(citations: CitationItem[]): CitationItem[] {
  return citations.filter((citation) => !isAgentSkillCitation(citation));
}

export const DEFAULT_ASSISTANT_NAME = "Aegis";

export function normalizeAssistantName(name?: string | null): string {
  const trimmed = typeof name === "string" ? name.trim() : "";
  return trimmed || DEFAULT_ASSISTANT_NAME;
}

export function parseRTCEvent(raw: string): RTCEventPayload | null {
  try {
    const parsed = JSON.parse(raw) as RTCEventPayload;
    if (!parsed || typeof parsed.type !== "string") return null;
    return parsed;
  } catch {
    return null;
  }
}

export function useVoiceAgentRTC() {
  const [connected, setConnected] = useState(false);
  const [connecting, setConnecting] = useState(false);
  const [agentState, setAgentState] = useState<AgentState>("idle");
  const [assistantName, setAssistantName] = useState(DEFAULT_ASSISTANT_NAME);
  const [micEnabled, setMicEnabled] = useState(true);
  const [speakerEnabled, setSpeakerEnabled] = useState(true);
  const [cameraEnabled, setCameraEnabled] = useState(false);
  const [transcripts, setTranscripts] = useState<TranscriptItem[]>([]);
  const [citations, setCitations] = useState<CitationItem[]>([]);
  const [activeTool, setActiveTool] = useState<string | null>(null);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);
  const [playbackErrorMsg, setPlaybackErrorMsg] = useState<string | null>(null);
  const [permissionPrompt, setPermissionPrompt] = useState<string | null>(null);

  const pcRef = useRef<RTCPeerConnection | null>(null);
  const dcRef = useRef<RTCDataChannel | null>(null);
  const localAudioStreamRef = useRef<MediaStream | null>(null);
  const processedAudioStreamRef = useRef<MediaStream | null>(null);
  const audioFocusGraphRef = useRef<AudioFocusGraph | null>(null);
  const localVideoStreamRef = useRef<MediaStream | null>(null);
  const videoPreviewRef = useRef<HTMLVideoElement | null>(null);
  const remoteAudioRef = useRef<HTMLAudioElement | null>(null);

  const audioCtxRef = useRef<AudioContext | null>(null);
  const micAnalyserRef = useRef<AnalyserNode | null>(null);
  const spkAnalyserRef = useRef<AnalyserNode | null>(null);
  const visionIntervalRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const vadIntervalRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const micStateRef = useRef(true);
  const spkStateRef = useRef(true);
  const camStateRef = useRef(false);

  const playRemoteAudio = useCallback(() => {
    const audio = remoteAudioRef.current;
    const stream = audio?.srcObject;
    if (!audio || !stream) return;
    void audio.play().then(
      () => {
        if (audio.srcObject === stream) setPlaybackErrorMsg(null);
      },
      () => {
        if (audio.srcObject === stream) {
          setPlaybackErrorMsg(
            "The browser could not play the reply audio. Turn the speaker off and on to retry playback."
          );
        }
      }
    );
  }, []);

  const sendMediaToggle = useCallback(() => {
    const dc = dcRef.current;
    if (dc && dc.readyState === "open") {
      dc.send(
        JSON.stringify({
          type: "media.toggle",
          media: {
            micEnabled: micStateRef.current,
            speakerEnabled: spkStateRef.current,
            cameraEnabled: camStateRef.current,
          },
        })
      );
    }
  }, []);

  const handleDataChannelMessage = useCallback((raw: string) => {
    const ev = parseRTCEvent(raw);
    if (!ev) return;

    switch (ev.type) {
      case "state":
        if (ev.state) {
          setAgentState(ev.state);
          if (ev.state !== "thinking") setActiveTool(null);
          if (ev.state === "thinking" || ev.state === "speaking") setErrorMsg(null);
        }
        break;
      case "transcript.delta":
        if (ev.text && ev.role) {
          setTranscripts((prev) => {
            const last = prev[prev.length - 1];
            if (last && last.role === ev.role && last.isPartial) {
              return [
                ...prev.slice(0, -1),
                { ...last, text: last.text + ev.text },
              ];
            }
            return [
              ...prev.slice(-19),
              {
                id: `${Date.now()}-${Math.random()}`,
                role: ev.role!,
                text: ev.text!,
                isPartial: true,
              },
            ];
          });
        }
        break;
      case "transcript.final":
        if (ev.text && ev.role) {
          setTranscripts((prev) => {
            const last = prev[prev.length - 1];
            if (last && last.role === ev.role && last.isPartial) {
              return [
                ...prev.slice(0, -1),
                { ...last, text: ev.text!, isPartial: false },
              ];
            }
            return [
              ...prev.slice(-19),
              {
                id: `${Date.now()}-${Math.random()}`,
                role: ev.role!,
                text: ev.text!,
                isPartial: false,
              },
            ];
          });
        }
        break;
      case "citation":
        if (Array.isArray(ev.citations)) {
          setCitations(filterVisibleCitations(ev.citations));
        }
        break;
      case "tool.call":
        setActiveTool(ev.tool || "tool");
        break;
      case "barge_in":
        break;
      case "error":
        setErrorMsg(ev.message || "Assistant error");
        break;
    }
  }, []);

  const stopCall = useCallback(() => {
    if (visionIntervalRef.current) {
      clearInterval(visionIntervalRef.current);
      visionIntervalRef.current = null;
    }
    if (vadIntervalRef.current) {
      clearInterval(vadIntervalRef.current);
      vadIntervalRef.current = null;
    }
    dcRef.current?.close();
    dcRef.current = null;
    pcRef.current?.close();
    pcRef.current = null;

    localAudioStreamRef.current?.getTracks().forEach((t) => t.stop());
    localAudioStreamRef.current = null;
    processedAudioStreamRef.current?.getTracks().forEach((t) => t.stop());
    processedAudioStreamRef.current = null;
    audioFocusGraphRef.current?.close();
    audioFocusGraphRef.current = null;
    localVideoStreamRef.current?.getTracks().forEach((t) => t.stop());
    localVideoStreamRef.current = null;

    if (videoPreviewRef.current) {
      videoPreviewRef.current.srcObject = null;
    }
    if (remoteAudioRef.current) {
      remoteAudioRef.current.srcObject = null;
    }
    setPlaybackErrorMsg(null);
    if (audioCtxRef.current) {
      void audioCtxRef.current.close();
      audioCtxRef.current = null;
    }
    micAnalyserRef.current = null;
    spkAnalyserRef.current = null;

    camStateRef.current = false;
    setCameraEnabled(false);
    setConnected(false);
    setConnecting(false);
    setAgentState("idle");
    setActiveTool(null);
  }, []);

  const startCall = useCallback(async () => {
    setErrorMsg(null);
    setPlaybackErrorMsg(null);
    setPermissionPrompt(null);
    setConnecting(true);

    try {
      let audioFocusEnabled = true;
      try {
        const sessionConfig = await apiFetch<{
          assistantName?: string;
          audioFocusEnabled?: boolean;
        }>("/api/v1/rtc/config");
        if (typeof sessionConfig.audioFocusEnabled === "boolean") {
          audioFocusEnabled = sessionConfig.audioFocusEnabled;
        }
        if (typeof sessionConfig.assistantName === "string") {
          setAssistantName(normalizeAssistantName(sessionConfig.assistantName));
        }
      } catch {
        audioFocusEnabled = true;
      }

      let audioStream: MediaStream;
      try {
        audioStream = await navigator.mediaDevices.getUserMedia({
          audio: buildMicrophoneConstraints(audioFocusEnabled),
        });
      } catch {
        setPermissionPrompt(
          "Microphone permission was denied. Please allow microphone access in your browser site settings and try again."
        );
        setConnecting(false);
        return;
      }

      localAudioStreamRef.current = audioStream;
      audioStream.getAudioTracks().forEach((t) => {
        t.enabled = micStateRef.current;
      });

      // Setup Web Audio AnalyserNode for reactive FFT energy
      const AudioCtx = window.AudioContext || (window as unknown as { webkitAudioContext: typeof AudioContext }).webkitAudioContext;
      if (AudioCtx) {
        const actx = new AudioCtx();
        audioCtxRef.current = actx;
        const graph = createAudioFocusGraph(actx, audioStream, audioFocusEnabled);
        audioFocusGraphRef.current = graph;
        processedAudioStreamRef.current = graph.stream;

        let micAnalyser = graph.analyser;
        if (!micAnalyser) {
          const micSrc = actx.createMediaStreamSource(audioStream);
          micAnalyser = actx.createAnalyser();
          micAnalyser.fftSize = 128;
          micSrc.connect(micAnalyser);
        }
        micAnalyserRef.current = micAnalyser;

        const tdBuf = new Uint8Array(micAnalyser.fftSize);
        const vad = new AdaptiveVAD(audioFocusEnabled);
        vadIntervalRef.current = setInterval(() => {
          const dc = dcRef.current;
          if (!dc || dc.readyState !== "open" || !micStateRef.current) {
            setAudioFocusSpeechGate(graph, false, actx);
            return;
          }
          micAnalyser.getByteTimeDomainData(tdBuf);
          let sumSq = 0;
          for (let i = 0; i < tdBuf.length; i++) {
            const v = (tdBuf[i] - 128) / 128;
            sumSq += v * v;
          }
          const rms = Math.sqrt(sumSq / tdBuf.length);
          const now = performance.now();
          const decision = vad.update(rms, now);
          setAudioFocusSpeechGate(graph, decision.speaking, actx);
          if (decision.event === "start") {
            dc.send(JSON.stringify({ type: "vad.speech_start" }));
          } else if (decision.event === "end") {
            dc.send(JSON.stringify({ type: "vad.speech_end" }));
          }
        }, 50);
      } else {
        processedAudioStreamRef.current = audioStream;
      }

      const tokenRes = await apiFetch<{ token: string }>("/api/v1/rtc/token", {
        method: "POST",
      });

      const pc = new RTCPeerConnection({
        iceServers: [{ urls: "stun:stun.l.google.com:19302" }],
      });
      pcRef.current = pc;

      const outboundAudioStream = processedAudioStreamRef.current || audioStream;
      outboundAudioStream.getTracks().forEach((track) => pc.addTrack(track, outboundAudioStream));

      pc.ontrack = (event) => {
        const [remoteStream] = event.streams;
        if (remoteStream && remoteAudioRef.current) {
          remoteAudioRef.current.srcObject = remoteStream;
          remoteAudioRef.current.muted = !spkStateRef.current;
          playRemoteAudio();

          if (audioCtxRef.current) {
            const spkSrc = audioCtxRef.current.createMediaStreamSource(remoteStream);
            const spkAnalyser = audioCtxRef.current.createAnalyser();
            spkAnalyser.fftSize = 128;
            spkSrc.connect(spkAnalyser);
            spkAnalyserRef.current = spkAnalyser;
          }
        }
      };

      pc.onconnectionstatechange = () => {
        if (pc.connectionState === "failed" || pc.connectionState === "closed") {
          stopCall();
        }
      };

      const dc = pc.createDataChannel("events", { ordered: true });
      dcRef.current = dc;
      dc.onopen = () => {
        sendMediaToggle();
      };
      dc.onmessage = (msg) => {
        if (typeof msg.data === "string") {
          handleDataChannelMessage(msg.data);
        }
      };

      const offer = await pc.createOffer();
      await pc.setLocalDescription(offer);

      await new Promise<void>((resolve) => {
        if (pc.iceGatheringState === "complete") {
          resolve();
          return;
        }
        const timer = setTimeout(resolve, 2500);
        pc.onicegatheringstatechange = () => {
          if (pc.iceGatheringState === "complete") {
            clearTimeout(timer);
            resolve();
          }
        };
      });

      const answerRes = await apiFetch<{ peerId: string; sdp: string }>(
        "/api/v1/rtc/offer",
        {
          method: "POST",
          body: JSON.stringify({
            signalingToken: tokenRes.token,
            sdp: pc.localDescription?.sdp || offer.sdp,
          }),
        }
      );

      await pc.setRemoteDescription({ type: "answer", sdp: answerRes.sdp });
      setConnected(true);
      setAgentState("listening");
    } catch (err) {
      setErrorMsg(err instanceof Error ? err.message : "Failed to connect call");
      stopCall();
    } finally {
      setConnecting(false);
    }
  }, [handleDataChannelMessage, playRemoteAudio, sendMediaToggle, stopCall]);

  const toggleMic = useCallback(() => {
    const next = !micStateRef.current;
    micStateRef.current = next;
    setMicEnabled(next);
    localAudioStreamRef.current?.getAudioTracks().forEach((t) => {
      t.enabled = next;
    });
    processedAudioStreamRef.current?.getAudioTracks().forEach((t) => {
      t.enabled = next;
    });
    sendMediaToggle();
  }, [sendMediaToggle]);

  const toggleSpeaker = useCallback(() => {
    const next = !spkStateRef.current;
    spkStateRef.current = next;
    setSpeakerEnabled(next);
    if (remoteAudioRef.current) {
      remoteAudioRef.current.muted = !next;
      if (next) playRemoteAudio();
    }
    sendMediaToggle();
  }, [playRemoteAudio, sendMediaToggle]);

  const toggleCamera = useCallback(async () => {
    setPermissionPrompt(null);
    const next = !camStateRef.current;

    if (next) {
      try {
        const videoStream = await navigator.mediaDevices.getUserMedia({
          video: { width: { ideal: 640 }, height: { ideal: 480 }, frameRate: { max: 15 } },
        });
        localVideoStreamRef.current = videoStream;
        if (videoPreviewRef.current) {
          videoPreviewRef.current.srcObject = videoStream;
        }
        const videoTrack = videoStream.getVideoTracks()[0];
        if (pcRef.current && videoTrack) {
          const sender = pcRef.current
            .getSenders()
            .find((s) => s.track?.kind === "video");
          if (sender) {
            await sender.replaceTrack(videoTrack);
          } else {
            pcRef.current.addTrack(videoTrack, videoStream);
          }
        }

        // Sample low-bandwidth JPEG frame for multimodal vision context
        const canvas = document.createElement("canvas");
        canvas.width = 480;
        canvas.height = 360;
        const ctx2d = canvas.getContext("2d");
        visionIntervalRef.current = setInterval(() => {
          if (
            videoPreviewRef.current &&
            ctx2d &&
            dcRef.current?.readyState === "open"
          ) {
            ctx2d.drawImage(videoPreviewRef.current, 0, 0, canvas.width, canvas.height);
            const dataUrl = canvas.toDataURL("image/jpeg", 0.65);
            const b64 = dataUrl.split(",")[1];
            if (b64) {
              dcRef.current.send(JSON.stringify({ type: "vision.frame", text: b64 }));
            }
          }
        }, 1200);

        camStateRef.current = true;
        setCameraEnabled(true);
        sendMediaToggle();
      } catch {
        setPermissionPrompt(
          "Camera permission was denied. Please enable camera access in your browser site settings."
        );
      }
    } else {
      if (visionIntervalRef.current) {
        clearInterval(visionIntervalRef.current);
        visionIntervalRef.current = null;
      }
      localVideoStreamRef.current?.getTracks().forEach((t) => t.stop());
      localVideoStreamRef.current = null;
      if (videoPreviewRef.current) {
        videoPreviewRef.current.srcObject = null;
      }
      camStateRef.current = false;
      setCameraEnabled(false);
      sendMediaToggle();
    }
  }, [sendMediaToggle]);

  const getAudioEnergy = useCallback(() => {
    const readLevel = (analyser: AnalyserNode | null) => {
      if (!analyser) return 0;
      const buf = new Uint8Array(analyser.frequencyBinCount);
      analyser.getByteFrequencyData(buf);
      let sum = 0;
      for (let i = 0; i < buf.length; i++) sum += buf[i];
      return buf.length > 0 ? sum / (buf.length * 255) : 0;
    };
    return {
      micLevel: micStateRef.current ? readLevel(micAnalyserRef.current) : 0,
      speakerLevel: spkStateRef.current ? readLevel(spkAnalyserRef.current) : 0,
    };
  }, []);

  useEffect(() => {
    let active = true;
    apiFetch<{ assistantName?: string; audioFocusEnabled?: boolean }>("/api/v1/rtc/config")
      .then((cfg) => {
        if (active && cfg && typeof cfg.assistantName === "string") {
          setAssistantName(normalizeAssistantName(cfg.assistantName));
        }
      })
      .catch(() => {});
    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    return () => {
      stopCall();
    };
  }, [stopCall]);

  return {
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
    errorMsg: errorMsg || playbackErrorMsg,
    permissionPrompt,
    videoPreviewRef,
    remoteAudioRef,
    startCall,
    stopCall,
    toggleMic,
    toggleSpeaker,
    toggleCamera,
    getAudioEnergy,
  };
}
