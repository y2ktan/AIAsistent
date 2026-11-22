export interface VoiceLanguage {
  code: string;
  name: string;
  voice: string;
  elevenLabsVoice: string;
}

export interface LanguageCatalogItem {
  code: string;
  name: string;
  script: string;
}

export interface VoiceOption {
  id: string;
  name: string;
  language?: string;
}

export const ELEVENLABS_CATALOG_VOICES: VoiceOption[] = [
  { id: "BrbEfHMQu0fyclQR7lfh", name: "Kevin Tu", language: "zh" },
  {
    id: "hpp4J3VqNfWAUOO0d1Us",
    name: "Bella - Professional, Bright, Warm",
    language: "en",
  },
];

export const ELEVENLABS_MODEL_OPTIONS = [
  { id: "eleven_v4", name: "Eleven v4" },
  { id: "eleven_v4_turbo", name: "Eleven v4 Turbo" },
  { id: "eleven_v3", name: "Eleven v3" },
  { id: "eleven_v3_conversational", name: "Eleven v3 Conversational" },
  { id: "eleven_multilingual_v2", name: "Eleven Multilingual v2" },
  { id: "eleven_flash_v2_5", name: "Eleven Flash v2.5" },
  { id: "eleven_turbo_v2_5", name: "Eleven Turbo v2.5" },
  { id: "eleven_turbo_v2", name: "Eleven Turbo v2" },
  { id: "eleven_flash_v2", name: "Eleven Flash v2" },
];

export interface VoiceLanguageSettings {
  languages: VoiceLanguage[];
  defaultLanguage: string;
  saved: boolean;
  catalog: LanguageCatalogItem[];
}

export function missingDefaultProviderVoice(
  languages: VoiceLanguage[],
  defaultLanguage: string,
  speechProvider: "local" | "elevenlabs"
): VoiceLanguage | undefined {
  const defaultEntry = languages.find((language) => language.code === defaultLanguage);
  if (!defaultEntry) {
    return undefined;
  }

  const providerVoice =
    speechProvider === "elevenlabs"
      ? defaultEntry.elevenLabsVoice
      : defaultEntry.voice;
  return providerVoice?.trim() ? undefined : defaultEntry;
}

export function applyRecommendedElevenLabsVoices(
  languages: VoiceLanguage[],
  voiceOptions: VoiceOption[]
): VoiceLanguage[] {
  const recommended = new Map<string, VoiceOption>();
  for (const option of voiceOptions) {
    const language = option.language?.trim().toLowerCase();
    if (language && !recommended.has(language)) {
      recommended.set(language, option);
    }
  }

  return languages.map((language) => {
    if (language.elevenLabsVoice.trim() !== "") {
      return language;
    }
    const voice = recommended.get(language.code.toLowerCase());
    return voice ? { ...language, elevenLabsVoice: voice.id } : language;
  });
}

const KOKORO_PREFIXES: Record<string, string[]> = {
  en: ["a", "b"],
  es: ["e"],
  fr: ["f"],
  hi: ["h"],
  it: ["i"],
  ja: ["j"],
  pt: ["p"],
  zh: ["z"],
};

export function partitionVoicesByLanguage(
  voices: string[],
  languageCode: string
): { recommended: string[]; others: string[] } {
  const language = languageCode.toLowerCase().split("-")[0];
  const prefixes = KOKORO_PREFIXES[language] || [];
  const recommended: string[] = [];
  const others: string[] = [];

  for (const voice of voices) {
    if (prefixes.some((prefix) => voice.toLowerCase().startsWith(prefix))) {
      recommended.push(voice);
    } else {
      others.push(voice);
    }
  }

  return { recommended, others };
}

export function includeSavedOption(options: string[], savedValue: string): string[] {
  if (!savedValue || options.includes(savedValue)) {
    return options;
  }
  return [...options, savedValue];
}

export function canRemoveLanguage(code: string, defaultLanguage: string): boolean {
  return code !== defaultLanguage;
}

export function availableCatalogLanguages(
  languages: VoiceLanguage[],
  catalog: LanguageCatalogItem[]
): LanguageCatalogItem[] {
  const existing = new Set(languages.map((l) => l.code));
  return catalog.filter((item) => !existing.has(item.code));
}

export function addLanguage(
  languages: VoiceLanguage[],
  catalog: LanguageCatalogItem[],
  code: string
): VoiceLanguage[] {
  if (languages.some((l) => l.code === code)) {
    return languages;
  }
  const entry = catalog.find((c) => c.code === code);
  if (!entry) {
    return languages;
  }
  return [...languages, { code: entry.code, name: entry.name, voice: "", elevenLabsVoice: "" }];
}

export function removeLanguage(
  languages: VoiceLanguage[],
  defaultLanguage: string,
  code: string
): VoiceLanguage[] {
  if (!canRemoveLanguage(code, defaultLanguage)) {
    return languages;
  }
  return languages.filter((l) => l.code !== code);
}
