"use client";

import { useEffect, useRef, useState } from "react";
import * as THREE from "three";
import type { AgentState } from "@/hooks/useVoiceAgentRTC";

interface LiquidPlasmaOrbProps {
  state: AgentState;
  getAudioEnergy: () => { micLevel: number; speakerLevel: number };
}

const STATE_PALETTES: Record<AgentState, { primary: string; secondary: string }> = {
  idle: { primary: "#334155", secondary: "#1E293B" },
  listening: { primary: "#00F5D4", secondary: "#0EA5E9" },
  retrieving: { primary: "#10B981", secondary: "#00F5D4" },
  thinking: { primary: "#7B61FF", secondary: "#EC4899" },
  speaking: { primary: "#FFB703", secondary: "#F97316" },
};

const VERTEX_SHADER = `
  uniform float uTime;
  uniform float uAmplitude;
  varying vec3 vNormal;
  varying float vDisplacement;

  float hash(vec3 p) {
    p = fract(p * 0.3183099 + 0.1);
    p *= 17.0;
    return fract(p.x * p.y * p.z * (p.x + p.y + p.z));
  }

  float noise(vec3 x) {
    vec3 i = floor(x);
    vec3 f = fract(x);
    f = f * f * (3.0 - 2.0 * f);
    return mix(
      mix(mix(hash(i + vec3(0,0,0)), hash(i + vec3(1,0,0)), f.x),
          mix(hash(i + vec3(0,1,0)), hash(i + vec3(1,1,0)), f.x), f.y),
      mix(mix(hash(i + vec3(0,0,1)), hash(i + vec3(1,0,1)), f.x),
          mix(hash(i + vec3(0,1,1)), hash(i + vec3(1,1,1)), f.x), f.y),
      f.z
    );
  }

  void main() {
    vNormal = normalize(normalMatrix * normal);
    float n = noise(position * 2.2 + vec3(uTime * 0.85));
    float n2 = noise(position * 4.5 - vec3(uTime * 1.2));
    float disp = (n * 0.65 + n2 * 0.35 - 0.5) * (0.14 + uAmplitude * 0.55);
    vDisplacement = disp;
    vec3 newPos = position + normal * disp;
    gl_Position = projectionMatrix * modelViewMatrix * vec4(newPos, 1.0);
  }
`;

const FRAGMENT_SHADER = `
  uniform vec3 uColorPrimary;
  uniform vec3 uColorSecondary;
  uniform float uAmplitude;
  varying vec3 vNormal;
  varying float vDisplacement;

  void main() {
    float fresnel = pow(1.0 - abs(dot(vNormal, vec3(0.0, 0.0, 1.0))), 2.2);
    vec3 base = mix(uColorSecondary, uColorPrimary, clamp(vDisplacement * 3.0 + 0.5, 0.0, 1.0));
    vec3 glow = uColorPrimary * fresnel * (1.2 + uAmplitude * 1.4);
    gl_FragColor = vec4(base * 0.72 + glow, 0.95);
  }
`;

export function LiquidPlasmaOrb({ state, getAudioEnergy }: LiquidPlasmaOrbProps) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const stateRef = useRef<AgentState>(state);
  const [reducedMotion, setReducedMotion] = useState(false);

  useEffect(() => {
    stateRef.current = state;
  }, [state]);

  useEffect(() => {
    const mq = window.matchMedia("(prefers-reduced-motion: reduce)");
    setReducedMotion(mq.matches);
    const onChange = (e: MediaQueryListEvent) => setReducedMotion(e.matches);
    mq.addEventListener("change", onChange);
    return () => mq.removeEventListener("change", onChange);
  }, []);

  useEffect(() => {
    if (reducedMotion || !containerRef.current) return;
    const container = containerRef.current;
    const size = 320;

    let renderer: THREE.WebGLRenderer;
    try {
      renderer = new THREE.WebGLRenderer({ alpha: true, antialias: true });
    } catch {
      setReducedMotion(true);
      return;
    }

    renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));
    renderer.setSize(size, size);
    container.innerHTML = "";
    container.appendChild(renderer.domElement);

    const scene = new THREE.Scene();
    const camera = new THREE.PerspectiveCamera(45, 1, 0.1, 100);
    camera.position.z = 3.2;

    const geometry = new THREE.IcosahedronGeometry(1.0, 48);
    const uniforms = {
      uTime: { value: 0 },
      uAmplitude: { value: 0 },
      uColorPrimary: { value: new THREE.Color(STATE_PALETTES.idle.primary) },
      uColorSecondary: { value: new THREE.Color(STATE_PALETTES.idle.secondary) },
    };

    const material = new THREE.ShaderMaterial({
      vertexShader: VERTEX_SHADER,
      fragmentShader: FRAGMENT_SHADER,
      uniforms,
      transparent: true,
    });

    const mesh = new THREE.Mesh(geometry, material);
    scene.add(mesh);

    let rafId = 0;
    const clock = new THREE.Clock();
    const targetPrimary = new THREE.Color();
    const targetSecondary = new THREE.Color();

    const animate = () => {
      rafId = requestAnimationFrame(animate);
      const dt = clock.getDelta();
      uniforms.uTime.value += dt;

      const palette = STATE_PALETTES[stateRef.current] || STATE_PALETTES.idle;
      targetPrimary.set(palette.primary);
      targetSecondary.set(palette.secondary);
      uniforms.uColorPrimary.value.lerp(targetPrimary, 0.08);
      uniforms.uColorSecondary.value.lerp(targetSecondary, 0.08);

      const { micLevel, speakerLevel } = getAudioEnergy();
      const targetAmp = Math.max(micLevel, speakerLevel);
      uniforms.uAmplitude.value += (targetAmp - uniforms.uAmplitude.value) * 0.2;

      mesh.rotation.y += dt * 0.18;
      mesh.rotation.x += dt * 0.07;
      renderer.render(scene, camera);
    };

    animate();

    return () => {
      cancelAnimationFrame(rafId);
      geometry.dispose();
      material.dispose();
      renderer.dispose();
    };
  }, [reducedMotion, getAudioEnergy]);

  const palette = STATE_PALETTES[state] || STATE_PALETTES.idle;

  if (reducedMotion) {
    return (
      <div
        className="flex h-64 w-64 items-center justify-center rounded-full border transition-all duration-500"
        style={{
          borderColor: palette.primary,
          background: `radial-gradient(circle, ${palette.primary}33 0%, ${palette.secondary}11 70%, transparent 100%)`,
          boxShadow: `0 0 60px ${palette.primary}33`,
        }}
        aria-label={`Assistant state: ${state}`}
      >
        <span className="text-xs font-medium uppercase tracking-widest text-slate-200">
          {state}
        </span>
      </div>
    );
  }

  return (
    <div className="relative flex items-center justify-center">
      <div
        className="pointer-events-none absolute h-72 w-72 rounded-full blur-3xl transition-colors duration-700"
        style={{ backgroundColor: `${palette.primary}24` }}
      />
      <div
        ref={containerRef}
        className="relative h-80 w-80"
        aria-label={`Assistant state: ${state}`}
      />
    </div>
  );
}
