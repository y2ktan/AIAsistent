package agent

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

type LanguageCatalogEntry struct {
	Code      string   `json:"code"`
	Name      string   `json:"name"`
	Script    string   `json:"script"` // "latin", "han", or "other"
	STTLabels []string `json:"-"`
}

type VoiceLanguage struct {
	Code  string `json:"code"`
	Name  string `json:"name"`
	Voice string `json:"voice"`
}

type VoiceLanguageSettings struct {
	Languages   []VoiceLanguage `json:"languages"`
	DefaultCode string          `json:"defaultLanguage"`
}

var languageCatalog = []LanguageCatalogEntry{
	{Code: "en", Name: "English", Script: "latin", STTLabels: []string{"en", "eng", "english"}},
	{Code: "zh", Name: "Mandarin Chinese", Script: "han", STTLabels: []string{"zh", "zho", "chi", "zh-cn", "zh-tw", "chinese", "mandarin", "mandarin chinese"}},
	{Code: "yue", Name: "Cantonese", Script: "han", STTLabels: []string{"yue", "zh-hk", "cantonese"}},
	{Code: "ms", Name: "Malay", Script: "latin", STTLabels: []string{"ms", "msa", "may", "zsm", "malay"}},
	{Code: "id", Name: "Indonesian", Script: "latin", STTLabels: []string{"id", "ind", "indonesian"}},
	{Code: "ta", Name: "Tamil", Script: "other", STTLabels: []string{"ta", "tam", "tamil"}},
	{Code: "ja", Name: "Japanese", Script: "other", STTLabels: []string{"ja", "jpn", "japanese"}},
	{Code: "ko", Name: "Korean", Script: "other", STTLabels: []string{"ko", "kor", "korean"}},
	{Code: "hi", Name: "Hindi", Script: "other", STTLabels: []string{"hi", "hin", "hindi"}},
	{Code: "th", Name: "Thai", Script: "other", STTLabels: []string{"th", "tha", "thai"}},
	{Code: "vi", Name: "Vietnamese", Script: "latin", STTLabels: []string{"vi", "vie", "vietnamese"}},
	{Code: "es", Name: "Spanish", Script: "latin", STTLabels: []string{"es", "spa", "spanish"}},
	{Code: "fr", Name: "French", Script: "latin", STTLabels: []string{"fr", "fra", "fre", "french"}},
	{Code: "de", Name: "German", Script: "latin", STTLabels: []string{"de", "deu", "ger", "german"}},
	{Code: "pt", Name: "Portuguese", Script: "latin", STTLabels: []string{"pt", "por", "portuguese"}},
	{Code: "it", Name: "Italian", Script: "latin", STTLabels: []string{"it", "ita", "italian"}},
	{Code: "ar", Name: "Arabic", Script: "other", STTLabels: []string{"ar", "ara", "arabic"}},
}

func LanguageCatalog() []LanguageCatalogEntry {
	out := make([]LanguageCatalogEntry, len(languageCatalog))
	copy(out, languageCatalog)
	return out
}

func LookupCatalogLanguage(code string) (LanguageCatalogEntry, bool) {
	norm := strings.ToLower(strings.TrimSpace(code))
	for _, entry := range languageCatalog {
		if entry.Code == norm {
			return entry, true
		}
	}
	return LanguageCatalogEntry{}, false
}

func InitialVoiceLanguageSettings(englishVoice string) VoiceLanguageSettings {
	v := strings.TrimSpace(englishVoice)
	if v == "" {
		v = "alloy"
	}
	return VoiceLanguageSettings{
		DefaultCode: "en",
		Languages: []VoiceLanguage{
			{Code: "en", Name: "English", Voice: v},
			{Code: "zh", Name: "Mandarin Chinese", Voice: ""},
			{Code: "yue", Name: "Cantonese", Voice: ""},
			{Code: "ms", Name: "Malay", Voice: ""},
		},
	}
}

func ValidateVoiceLanguageSettings(s VoiceLanguageSettings) error {
	if len(s.Languages) == 0 {
		return errors.New("at least one supported language is required")
	}
	seen := make(map[string]bool, len(s.Languages))
	defaultVoice := ""
	hasDefault := false

	for _, lang := range s.Languages {
		code := strings.ToLower(strings.TrimSpace(lang.Code))
		if _, ok := LookupCatalogLanguage(code); !ok {
			return fmt.Errorf("unsupported language code: %s", lang.Code)
		}
		if seen[code] {
			return fmt.Errorf("duplicate language code: %s", code)
		}
		seen[code] = true
		if code == strings.ToLower(strings.TrimSpace(s.DefaultCode)) {
			hasDefault = true
			defaultVoice = strings.TrimSpace(lang.Voice)
		}
	}

	if !hasDefault {
		return errors.New("choose another default language before removing this one")
	}
	if defaultVoice == "" {
		return errors.New("the default language must have an assigned voice")
	}
	return nil
}

func (s VoiceLanguageSettings) Find(code string) (VoiceLanguage, bool) {
	norm := strings.ToLower(strings.TrimSpace(code))
	for _, l := range s.Languages {
		if strings.ToLower(strings.TrimSpace(l.Code)) == norm {
			return l, true
		}
	}
	return VoiceLanguage{}, false
}

func (s VoiceLanguageSettings) VoicedLanguages() []VoiceLanguage {
	var out []VoiceLanguage
	for _, l := range s.Languages {
		if strings.TrimSpace(l.Voice) != "" {
			out = append(out, l)
		}
	}
	return out
}

func NormalizeSTTLabel(label string, settings VoiceLanguageSettings) (code string, displayName string, known bool) {
	norm := strings.ToLower(strings.TrimSpace(label))
	if norm == "" {
		return "", "", false
	}
	for _, entry := range languageCatalog {
		if entry.Code == norm || strings.ToLower(entry.Name) == norm {
			code = entry.Code
			displayName = entry.Name
			known = true
			break
		}
		for _, alias := range entry.STTLabels {
			if alias == norm {
				code = entry.Code
				displayName = entry.Name
				known = true
				break
			}
		}
		if known {
			break
		}
	}
	if !known {
		return "", strings.TrimSpace(label), false
	}

	// Whisper frequently labels Malay audio as Indonesian; map to Malay when Malay is configured and Indonesian is not.
	if code == "id" {
		_, hasMS := settings.Find("ms")
		_, hasID := settings.Find("id")
		if hasMS && !hasID {
			return "ms", "Malay", true
		}
	}
	return code, displayName, true
}

func DetectDominantScript(text string) (script string, wordUnits int) {
	var hanCount, latinWords, otherWords int
	inLatinWord := false
	inOtherWord := false

	for _, r := range text {
		switch {
		case unicode.Is(unicode.Han, r):
			hanCount++
			wordUnits++
			inLatinWord = false
			inOtherWord = false
		case unicode.IsLetter(r) && unicode.Is(unicode.Latin, r):
			if !inLatinWord {
				latinWords++
				wordUnits++
				inLatinWord = true
			}
			inOtherWord = false
		case unicode.IsLetter(r):
			if !inOtherWord {
				otherWords++
				wordUnits++
				inOtherWord = true
			}
			inLatinWord = false
		default:
			inLatinWord = false
			inOtherWord = false
		}
	}

	switch {
	case hanCount > latinWords && hanCount > otherWords:
		return "han", wordUnits
	case latinWords > hanCount && latinWords > otherWords:
		return "latin", wordUnits
	case otherWords > hanCount && otherWords > latinWords:
		return "other", wordUnits
	default:
		return "", wordUnits
	}
}

func DetectUtteranceLanguage(sttLabel, transcript string, settings VoiceLanguageSettings) (code string, rawName string) {
	labelCode, labelName, known := NormalizeSTTLabel(sttLabel, settings)
	domScript, wordUnits := DetectDominantScript(transcript)

	if wordUnits < 2 {
		return "", ""
	}

	if domScript != "" {
		if known {
			entry, _ := LookupCatalogLanguage(labelCode)
			if entry.Script == domScript {
				return labelCode, labelName
			}
		}
		// Script contradicts STT label (or no known label): prefer configured default if it matches domScript.
		if defLang, ok := settings.Find(settings.DefaultCode); ok {
			if e, ok := LookupCatalogLanguage(defLang.Code); ok && e.Script == domScript {
				return defLang.Code, defLang.Name
			}
		}
		if domScript == "han" {
			if _, ok := settings.Find("zh"); ok {
				return "zh", "Mandarin Chinese"
			}
			if _, ok := settings.Find("yue"); ok {
				return "yue", "Cantonese"
			}
			return "zh", "Mandarin Chinese"
		}
		if domScript == "latin" {
			if _, ok := settings.Find("en"); ok {
				return "en", "English"
			}
			for _, l := range settings.Languages {
				if e, ok := LookupCatalogLanguage(l.Code); ok && e.Script == "latin" {
					return l.Code, l.Name
				}
			}
			return "en", "English"
		}
	}

	if known {
		return labelCode, labelName
	}
	if labelName != "" {
		return "", labelName
	}
	return "", ""
}

type LanguageSessionState struct {
	LastReplyCode        string
	AnnouncedUnsupported map[string]bool
}

func NewLanguageSessionState() *LanguageSessionState {
	return &LanguageSessionState{
		AnnouncedUnsupported: make(map[string]bool),
	}
}

type LanguageResolution struct {
	ReplyCode       string
	ReplyName       string
	ReplyVoice      string
	Fallback        bool
	NoticeDue       bool
	UnsupportedName string
}

func ResolveReplyLanguage(detectedCode, detectedName string, settings VoiceLanguageSettings, state *LanguageSessionState) LanguageResolution {
	defaultLang, hasDefault := settings.Find(settings.DefaultCode)
	if !hasDefault && len(settings.Languages) > 0 {
		defaultLang = settings.Languages[0]
	}

	// Undetermined utterance
	if detectedCode == "" && detectedName == "" {
		if state != nil && state.LastReplyCode != "" {
			if prev, ok := settings.Find(state.LastReplyCode); ok && strings.TrimSpace(prev.Voice) != "" {
				return LanguageResolution{
					ReplyCode:  prev.Code,
					ReplyName:  prev.Name,
					ReplyVoice: prev.Voice,
				}
			}
		}
		return LanguageResolution{
			ReplyCode:  defaultLang.Code,
			ReplyName:  defaultLang.Name,
			ReplyVoice: defaultLang.Voice,
		}
	}

	if detectedCode != "" {
		if lang, ok := settings.Find(detectedCode); ok && strings.TrimSpace(lang.Voice) != "" {
			return LanguageResolution{
				ReplyCode:  lang.Code,
				ReplyName:  lang.Name,
				ReplyVoice: lang.Voice,
			}
		}
	}

	// Fallback to default language
	uName := detectedName
	if uName == "" {
		if entry, ok := LookupCatalogLanguage(detectedCode); ok {
			uName = entry.Name
		} else {
			uName = "that language"
		}
	}

	noticeKey := strings.ToLower(strings.TrimSpace(detectedCode))
	if noticeKey == "" {
		noticeKey = strings.ToLower(strings.TrimSpace(uName))
	}

	noticeDue := false
	if state != nil {
		if state.AnnouncedUnsupported == nil {
			state.AnnouncedUnsupported = make(map[string]bool)
		}
		if !state.AnnouncedUnsupported[noticeKey] {
			noticeDue = true
			state.AnnouncedUnsupported[noticeKey] = true
		}
	}

	return LanguageResolution{
		ReplyCode:       defaultLang.Code,
		ReplyName:       defaultLang.Name,
		ReplyVoice:      defaultLang.Voice,
		Fallback:        true,
		NoticeDue:       noticeDue,
		UnsupportedName: uName,
	}
}

// GuardVoiceForReply checks whether the reply's dominant script matches the resolved
// reply language. If it contradicts (e.g. model answered in English when Mandarin was
// requested), it selects a supported, voiced language whose script matches, preferring
// the default language.
func GuardVoiceForReply(replyText string, res LanguageResolution, settings VoiceLanguageSettings) (voice string, actualCode string) {
	domScript, _ := DetectDominantScript(replyText)
	if domScript == "" || domScript == "other" {
		return res.ReplyVoice, res.ReplyCode
	}
	if entry, ok := LookupCatalogLanguage(res.ReplyCode); ok && entry.Script == domScript {
		return res.ReplyVoice, res.ReplyCode
	}
	if defLang, ok := settings.Find(settings.DefaultCode); ok && strings.TrimSpace(defLang.Voice) != "" {
		if entry, ok := LookupCatalogLanguage(defLang.Code); ok && entry.Script == domScript {
			return defLang.Voice, defLang.Code
		}
	}
	for _, l := range settings.Languages {
		if strings.TrimSpace(l.Voice) == "" {
			continue
		}
		if entry, ok := LookupCatalogLanguage(l.Code); ok && entry.Script == domScript {
			return l.Voice, l.Code
		}
	}
	return res.ReplyVoice, res.ReplyCode
}

func BuildLanguageSystemInstruction(res LanguageResolution) string {
	if !res.Fallback {
		return fmt.Sprintf("LANGUAGE RULE: Reply ONLY in %s.", res.ReplyName)
	}
	if res.NoticeDue {
		return fmt.Sprintf(
			"LANGUAGE RULE: The user spoke %s, which you cannot reply in yet. Understand their message as well as you can, answer in %s, and tell them in one short sentence in %s that you cannot reply in %s yet.",
			res.UnsupportedName, res.ReplyName, res.ReplyName, res.UnsupportedName,
		)
	}
	return fmt.Sprintf(
		"LANGUAGE RULE: The user spoke %s. Understand their message as well as you can and answer ONLY in %s without repeating that you cannot speak %s.",
		res.UnsupportedName, res.ReplyName, res.UnsupportedName,
	)
}
