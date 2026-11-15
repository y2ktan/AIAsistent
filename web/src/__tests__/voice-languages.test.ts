import { describe, expect, it } from "vitest";
import {
  addLanguage,
  availableCatalogLanguages,
  canRemoveLanguage,
  includeSavedOption,
  partitionVoicesByLanguage,
  removeLanguage,
  type LanguageCatalogItem,
  type VoiceLanguage,
} from "@/lib/voice-languages";

const catalog: LanguageCatalogItem[] = [
  { code: "en", name: "English", script: "latin" },
  { code: "zh", name: "Mandarin Chinese", script: "han" },
  { code: "ja", name: "Japanese", script: "other" },
];

describe("voice-languages helpers", () => {
  it("adds a catalog language once and filters it from available options", () => {
    const initial: VoiceLanguage[] = [{ code: "en", name: "English", voice: "af_heart" }];
    expect(availableCatalogLanguages(initial, catalog).map((c) => c.code)).toEqual(["zh", "ja"]);

    const added = addLanguage(initial, catalog, "ja");
    expect(added.map((l) => l.code)).toEqual(["en", "ja"]);
    expect(availableCatalogLanguages(added, catalog).map((c) => c.code)).toEqual(["zh"]);

    // Duplicate add is a no-op
    expect(addLanguage(added, catalog, "ja")).toEqual(added);
  });

  it("prevents removing the default language while allowing non-default removal", () => {
    const list: VoiceLanguage[] = [
      { code: "en", name: "English", voice: "af_heart" },
      { code: "zh", name: "Mandarin Chinese", voice: "zf_xiaoxiao" },
    ];
    expect(canRemoveLanguage("en", "en")).toBe(false);
    expect(canRemoveLanguage("zh", "en")).toBe(true);

    expect(removeLanguage(list, "en", "en")).toEqual(list);
    expect(removeLanguage(list, "en", "zh").map((l) => l.code)).toEqual(["en"]);
  });

  it("partitions Kokoro voices by the language-specific recommendation prefixes", () => {
    const voices = ["af_heart", "bf_emma", "zf_xiaoxiao", "jf_alpha", "ef_dora"];

    expect(partitionVoicesByLanguage(voices, "zh")).toEqual({
      recommended: ["zf_xiaoxiao"],
      others: ["af_heart", "bf_emma", "jf_alpha", "ef_dora"],
    });
    expect(partitionVoicesByLanguage(voices, "en")).toEqual({
      recommended: ["af_heart", "bf_emma"],
      others: ["zf_xiaoxiao", "jf_alpha", "ef_dora"],
    });
    expect(partitionVoicesByLanguage(voices, "yue").recommended).toEqual([]);
    expect(partitionVoicesByLanguage(voices, "ms").recommended).toEqual([]);
  });

  it("preserves previously saved custom values in selector options", () => {
    expect(includeSavedOption(["af_heart"], "custom_voice")).toEqual([
      "af_heart",
      "custom_voice",
    ]);
    expect(includeSavedOption(["Systran/faster-whisper-small"], "custom-model")).toEqual([
      "Systran/faster-whisper-small",
      "custom-model",
    ]);
    expect(includeSavedOption(["af_heart"], "af_heart")).toEqual(["af_heart"]);
  });
});
