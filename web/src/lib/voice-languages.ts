export interface VoiceLanguage {
  code: string;
  name: string;
  voice: string;
}

export interface LanguageCatalogItem {
  code: string;
  name: string;
  script: string;
}

export interface VoiceLanguageSettings {
  languages: VoiceLanguage[];
  defaultLanguage: string;
  saved: boolean;
  catalog: LanguageCatalogItem[];
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
  return [...languages, { code: entry.code, name: entry.name, voice: "" }];
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
