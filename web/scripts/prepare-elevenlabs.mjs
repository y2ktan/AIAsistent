import { copyFile, mkdir } from "node:fs/promises";

const directory = new URL("../public/elevenlabs/", import.meta.url);
await mkdir(directory, { recursive: true });
await copyFile(
  new URL(import.meta.resolve("@elevenlabs/client/worklets/rawAudioProcessor.js")),
  new URL("rawAudioProcessor.js", directory),
);
