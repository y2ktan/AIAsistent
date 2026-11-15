package agent

import "testing"

func TestLanguageCatalogAndValidation(t *testing.T) {
	for _, entry := range LanguageCatalog() {
		if entry.Code == "hokkien" || entry.Name == "Hokkien" {
			t.Fatal("Hokkien should not be in the catalog")
		}
	}

	init := InitialVoiceLanguageSettings("af_heart")
	if err := ValidateVoiceLanguageSettings(init); err != nil {
		t.Fatalf("initial settings should be valid: %v", err)
	}
	if init.DefaultCode != "en" || len(init.Languages) != 4 {
		t.Fatalf("unexpected initial settings: %+v", init)
	}

	cases := []struct {
		name string
		cfg  VoiceLanguageSettings
	}{
		{"empty list", VoiceLanguageSettings{DefaultCode: "en", Languages: nil}},
		{"unknown code", VoiceLanguageSettings{DefaultCode: "en", Languages: []VoiceLanguage{{Code: "en", Voice: "v"}, {Code: "hokkien", Voice: ""}}}},
		{"duplicate code", VoiceLanguageSettings{DefaultCode: "en", Languages: []VoiceLanguage{{Code: "en", Voice: "v"}, {Code: "en", Voice: "v2"}}}},
		{"default not in list", VoiceLanguageSettings{DefaultCode: "zh", Languages: []VoiceLanguage{{Code: "en", Voice: "v"}}}},
		{"default has no voice", VoiceLanguageSettings{DefaultCode: "en", Languages: []VoiceLanguage{{Code: "en", Voice: ""}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateVoiceLanguageSettings(tc.cfg); err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
		})
	}
}

func TestSTTNormalizationScriptAndWordUnits(t *testing.T) {
	settings := InitialVoiceLanguageSettings("af_heart")

	code, _, ok := NormalizeSTTLabel("zh", settings)
	if !ok || code != "zh" {
		t.Fatalf("expected zh, got %q ok=%v", code, ok)
	}
	code, _, ok = NormalizeSTTLabel("Chinese", settings)
	if !ok || code != "zh" {
		t.Fatalf("expected zh for Chinese, got %q ok=%v", code, ok)
	}
	code, _, ok = NormalizeSTTLabel("english", settings)
	if !ok || code != "en" {
		t.Fatalf("expected en for english, got %q ok=%v", code, ok)
	}
	code, _, ok = NormalizeSTTLabel("indonesian", settings)
	if !ok || code != "ms" {
		t.Fatalf("expected indonesian->ms alias when Malay is supported, got %q ok=%v", code, ok)
	}
	if _, _, ok := NormalizeSTTLabel("klingon", settings); ok {
		t.Fatal("expected unknown label to return ok=false")
	}

	// Indonesian should NOT alias to Malay if Indonesian is explicitly in settings
	withID := VoiceLanguageSettings{
		DefaultCode: "en",
		Languages: []VoiceLanguage{
			{Code: "en", Name: "English", Voice: "af_heart"},
			{Code: "ms", Name: "Malay", Voice: ""},
			{Code: "id", Name: "Indonesian", Voice: ""},
		},
	}
	if code, _, _ := NormalizeSTTLabel("indonesian", withID); code != "id" {
		t.Fatalf("expected id when Indonesian is configured, got %q", code)
	}

	// Dominant script and mixing
	c, _ := DetectUtteranceLanguage("en", "我今天想去买一个 laptop 回来用", settings)
	if c != "zh" {
		t.Fatalf("mostly-Han mixed utterance should resolve to zh, got %q", c)
	}
	c, _ = DetectUtteranceLanguage("zh", "I want to check the weather in 北京 today please", settings)
	if c != "en" {
		t.Fatalf("mostly-English mixed utterance should resolve to en, got %q", c)
	}

	// One-word inputs are undetermined
	if c, n := DetectUtteranceLanguage("en", "OK", settings); c != "" || n != "" {
		t.Fatalf("one-word English input should be undetermined, got (%q, %q)", c, n)
	}
	if c, n := DetectUtteranceLanguage("zh", "好", settings); c != "" || n != "" {
		t.Fatalf("one-char Han input should be undetermined, got (%q, %q)", c, n)
	}
}

func TestDetectUtteranceLanguageMixedScripts(t *testing.T) {
	settings := InitialVoiceLanguageSettings("af_heart")
	tests := []struct {
		name       string
		label      string
		transcript string
		want       string
	}{
		{"Chinese greeting with English label", "en", "Hello 你好", "zh"},
		{"Chinese greeting without label", "", "Hello 你好", "zh"},
		{"English sentence with Chinese greeting", "zh", "Hello how are you 你好", "en"},
		{"Chinese sentence with English word", "en", "我想买 laptop", "zh"},
		{"ambiguous acknowledgement", "zh", "好", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := DetectUtteranceLanguage(tt.label, tt.transcript, settings)
			if got != tt.want {
				t.Errorf("DetectUtteranceLanguage(%q, %q) = %q, want %q", tt.label, tt.transcript, got, tt.want)
			}
		})
	}
}

func TestResolveReplyLanguageScenarios(t *testing.T) {
	settings := VoiceLanguageSettings{
		DefaultCode: "en",
		Languages: []VoiceLanguage{
			{Code: "en", Name: "English", Voice: "af_heart"},
			{Code: "zh", Name: "Mandarin Chinese", Voice: "zf_xiaoxiao"},
			{Code: "yue", Name: "Cantonese", Voice: ""},
			{Code: "ms", Name: "Malay", Voice: ""},
		},
	}

	state := NewLanguageSessionState()

	// 1. Supported language with a voice (Mandarin)
	res := ResolveReplyLanguage("zh", "Mandarin Chinese", settings, state)
	if res.ReplyCode != "zh" || res.ReplyVoice != "zf_xiaoxiao" || res.Fallback {
		t.Fatalf("expected Mandarin reply, got %+v", res)
	}
	state.LastReplyCode = res.ReplyCode

	// 2. Undetermined utterance keeps previous reply language (Mandarin)
	res = ResolveReplyLanguage("", "", settings, state)
	if res.ReplyCode != "zh" || res.Fallback {
		t.Fatalf("expected previous reply language zh, got %+v", res)
	}

	// 3. Undetermined utterance with no previous reply uses default
	fresh := NewLanguageSessionState()
	res = ResolveReplyLanguage("", "", settings, fresh)
	if res.ReplyCode != "en" || res.Fallback {
		t.Fatalf("expected default en on fresh state, got %+v", res)
	}

	// 4. Language switch back to English
	res = ResolveReplyLanguage("en", "English", settings, state)
	if res.ReplyCode != "en" || res.ReplyVoice != "af_heart" || res.Fallback {
		t.Fatalf("expected switch to en, got %+v", res)
	}

	// 5. Supported language without a voice (Cantonese) -> fallback + notice once
	res = ResolveReplyLanguage("yue", "Cantonese", settings, state)
	if res.ReplyCode != "en" || !res.Fallback || !res.NoticeDue || res.UnsupportedName != "Cantonese" {
		t.Fatalf("expected Cantonese fallback with notice, got %+v", res)
	}
	// Repeat in same session -> notice NOT repeated
	res = ResolveReplyLanguage("yue", "Cantonese", settings, state)
	if res.ReplyCode != "en" || !res.Fallback || res.NoticeDue {
		t.Fatalf("expected Cantonese fallback without repeating notice, got %+v", res)
	}

	// 6. Unsupported language (Japanese) -> fallback + notice once
	res = ResolveReplyLanguage("ja", "Japanese", settings, state)
	if res.ReplyCode != "en" || !res.Fallback || !res.NoticeDue || res.UnsupportedName != "Japanese" {
		t.Fatalf("expected Japanese fallback with notice, got %+v", res)
	}

	// 7. New session -> notice given again
	newSess := NewLanguageSessionState()
	res = ResolveReplyLanguage("yue", "Cantonese", settings, newSess)
	if !res.NoticeDue {
		t.Fatalf("expected notice again in fresh session, got %+v", res)
	}
}

func TestDefaultLanguagePreference(t *testing.T) {
	mandarinDefault := VoiceLanguageSettings{
		DefaultCode: "zh",
		Languages: []VoiceLanguage{
			{Code: "en", Name: "English", Voice: "af_heart"},
			{Code: "zh", Name: "Mandarin Chinese", Voice: "zf_xiaoxiao"},
			{Code: "yue", Name: "Cantonese", Voice: ""},
			{Code: "ms", Name: "Malay", Voice: ""},
		},
	}

	t.Run("undetermined initial utterance falls back to Mandarin default", func(t *testing.T) {
		fresh := NewLanguageSessionState()
		code, name := DetectUtteranceLanguage("en", "OK", mandarinDefault)
		res := ResolveReplyLanguage(code, name, mandarinDefault, fresh)
		if res.ReplyCode != "zh" || res.ReplyVoice != "zf_xiaoxiao" || res.Fallback {
			t.Fatalf("expected zh fallback on fresh state, got %+v", res)
		}
	})

	t.Run("undetermined mid-session utterance keeps previous English reply", func(t *testing.T) {
		state := NewLanguageSessionState()
		state.LastReplyCode = "en"
		code, name := DetectUtteranceLanguage("en", "OK", mandarinDefault)
		res := ResolveReplyLanguage(code, name, mandarinDefault, state)
		if res.ReplyCode != "en" || res.ReplyVoice != "af_heart" || res.Fallback {
			t.Fatalf("expected previous reply language en, got %+v", res)
		}
	})

	t.Run("script-contradicting Han utterance resolves to Mandarin default", func(t *testing.T) {
		got, _ := DetectUtteranceLanguage("en", "你好吗请问", mandarinDefault)
		if got != "zh" {
			t.Fatalf("expected zh, got %q", got)
		}
	})

	cantoneseDefault := VoiceLanguageSettings{
		DefaultCode: "yue",
		Languages: []VoiceLanguage{
			{Code: "en", Name: "English", Voice: "af_heart"},
			{Code: "zh", Name: "Mandarin Chinese", Voice: "zf_xiaoxiao"},
			{Code: "yue", Name: "Cantonese", Voice: "yue_voice"},
		},
	}

	t.Run("Cantonese default preferred for Han script with missing or contradicting STT label", func(t *testing.T) {
		for _, label := range []string{"", "en"} {
			got, _ := DetectUtteranceLanguage(label, "你好嗎請問", cantoneseDefault)
			if got != "yue" {
				t.Fatalf("label %q: expected yue, got %q", label, got)
			}
		}
	})

	malayDefault := VoiceLanguageSettings{
		DefaultCode: "ms",
		Languages: []VoiceLanguage{
			{Code: "en", Name: "English", Voice: "af_heart"},
			{Code: "zh", Name: "Mandarin Chinese", Voice: "zf_xiaoxiao"},
			{Code: "ms", Name: "Malay", Voice: "ms_voice"},
		},
	}

	t.Run("Malay default preferred for Latin script with missing or contradicting STT label", func(t *testing.T) {
		for _, label := range []string{"", "zh"} {
			got, _ := DetectUtteranceLanguage(label, "Apa khabar hari ini", malayDefault)
			if got != "ms" {
				t.Fatalf("label %q: expected ms, got %q", label, got)
			}
		}
	})
}
