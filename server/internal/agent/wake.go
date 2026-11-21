package agent

import (
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	DefaultWakeWindow       = 15 * time.Second
	WaitForQuestionSentinel = "[WAIT_FOR_QUESTION]"
)

type WakeGateAction int

const (
	WakeActionIgnore WakeGateAction = iota
	WakeActionBufferWait
	WakeActionRespond
)

type WakeGateResult struct {
	Action       WakeGateAction
	WakeMatched  bool
	MatchedWake  string
	SegmentText  string
	CombinedText string
	Segments     []string
}

var (
	englishGreetings = []string{"hello", "okay", "hey", "hi", "ok"}
	chineseGreetings = []string{"请问", "你好", "您好", "哈喽", "嗨", "喂", "嘿"}

	englishInterrogatives = map[string]struct{}{
		"what": {}, "what's": {}, "whats": {},
		"why": {}, "why's": {}, "whys": {},
		"how": {}, "how's": {}, "hows": {},
		"when": {}, "when's": {}, "whens": {},
		"where": {}, "where's": {}, "wheres": {},
		"who": {}, "who's": {}, "whos": {},
		"which": {}, "whose": {}, "whom": {},
	}

	englishAuxiliaries = map[string]struct{}{
		"is": {}, "isn't": {}, "isnt": {},
		"are": {}, "aren't": {}, "arent": {},
		"am": {},
		"was": {}, "wasn't": {}, "wasnt": {},
		"were": {}, "weren't": {}, "werent": {},
		"can": {}, "can't": {}, "cant": {},
		"could": {}, "couldn't": {}, "couldnt": {},
		"would": {}, "wouldn't": {}, "wouldnt": {},
		"should": {}, "shouldn't": {}, "shouldnt": {},
		"will": {}, "won't": {}, "wont": {},
		"shall": {},
		"do": {}, "don't": {}, "dont": {},
		"does": {}, "doesn't": {}, "doesnt": {},
		"did": {}, "didn't": {}, "didnt": {},
		"have": {}, "haven't": {}, "havent": {},
		"has": {}, "hasn't": {}, "hasnt": {},
		"had": {}, "hadn't": {}, "hadnt": {},
		"may": {}, "might": {},
	}

	englishInversionSubjects = map[string]struct{}{
		"you": {}, "i": {}, "we": {}, "they": {}, "he": {}, "she": {}, "it": {},
		"there": {}, "this": {}, "that": {}, "these": {}, "those": {},
		"the": {}, "a": {}, "an": {}, "any": {},
		"my": {}, "our": {}, "your": {}, "his": {}, "her": {}, "their": {},
		"anyone": {}, "anybody": {}, "anything": {}, "someone": {}, "somebody": {}, "something": {},
	}

	englishLeadDiscourse = map[string]struct{}{
		"and": {}, "so": {}, "but": {}, "then": {}, "now": {}, "well": {}, "actually": {}, "please": {},
	}

	malayQuestionWords = map[string]struct{}{
		"apa": {}, "apakah": {}, "bagaimana": {}, "bagaimanakah": {},
		"kenapa": {}, "mengapa": {}, "mengapakah": {},
		"bila": {}, "bilakah": {}, "mana": {}, "manakah": {},
		"siapa": {}, "siapakah": {}, "berapa": {}, "berapakah": {},
		"bolehkah": {}, "adakah": {}, "sudahkah": {}, "benarkah": {}, "dapatkah": {},
	}

	malayNonQuestionKah = map[string]struct{}{
		"langkah": {}, "markah": {}, "nikah": {}, "sedekah": {},
		"tingkah": {}, "berkah": {}, "nafkah": {}, "rekah": {}, "mekah": {},
	}

	// Folded (Simplified) Mandarin & Cantonese question phrases and A-not-A forms.
	cjkQuestionSubstrings = []string{
		"为什么", "什么", "怎么", "怎样", "如何",
		"哪里", "哪儿", "哪个", "哪些", "哪位", "哪天", "哪年",
		"多少", "几号", "几点", "几个", "几天", "几年", "几岁", "几月", "星期几", "周几", "第几",
		"多长时间", "多久", "多远", "多大", "多高", "多重",
		"能否", "可否", "是否",
		"是不是", "能不能", "可不可以", "有没有", "好不好", "行不行", "对不对", "会不会", "要不要", "知不知道",
		"可以吗", "好吗", "对吗", "是吗", "行吗", "你好吗", "请问",
		// Cantonese (folded via FoldWakeRune)
		"点解", "点样", "边度", "边个", "几多", "几时", "乜嘢", "做咩", "咩事", "系唔系", "系咪", "得唔得", "有无", "有冇", "好唔好", "可唔可以", "知唔知",
	}

	cjkTrailingParticles = []rune{'吗', '呢', '么'}
)

// BuildWakeVocabularyPrompt formats the primary assistant name and wake aliases for STT prompt biasing.
func BuildWakeVocabularyPrompt(assistantName string, wakeAliases []string) string {
	candidates := orderedWakeCandidates(assistantName, wakeAliases)
	return strings.Join(candidates, ", ")
}

func orderedWakeCandidates(assistantName string, wakeAliases []string) []string {
	primary := strings.TrimSpace(assistantName)
	if primary == "" {
		primary = DefaultAssistantName
	}
	seen := make(map[string]struct{}, 1+len(wakeAliases))
	out := make([]string, 0, 1+len(wakeAliases))
	add := func(raw string) {
		s := strings.TrimSpace(raw)
		if s == "" {
			return
		}
		key := CanonicalWakeKey(s)
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, s)
	}
	add(primary)
	for _, a := range wakeAliases {
		add(a)
	}
	return out
}

func isCJKScript(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r)
}

func isWakeSeparator(r rune) bool {
	if unicode.IsSpace(r) || unicode.IsPunct(r) {
		return true
	}
	switch r {
	case '～', '~':
		return true
	default:
		return false
	}
}

func trimLeadingWakeSeparators(s string) string {
	for len(s) > 0 {
		r, size := utf8.DecodeRuneInString(s)
		if !isWakeSeparator(r) {
			return s
		}
		s = s[size:]
	}
	return ""
}

func matchWakeCandidateAtStart(s, candidate string) (bool, string) {
	s = trimLeadingWakeSeparators(s)
	candidate = strings.TrimSpace(candidate)
	if s == "" || candidate == "" {
		return false, ""
	}

	si := 0
	ci := 0
	var lastCandidateRune rune

	for ci < len(candidate) {
		cr, cSize := utf8.DecodeRuneInString(candidate[ci:])
		if unicode.IsSpace(cr) {
			for ci < len(candidate) {
				nr, nSize := utf8.DecodeRuneInString(candidate[ci:])
				if !unicode.IsSpace(nr) {
					break
				}
				ci += nSize
			}
			sepConsumed := false
			for si < len(s) {
				sr, sSize := utf8.DecodeRuneInString(s[si:])
				if !isWakeSeparator(sr) {
					break
				}
				sepConsumed = true
				si += sSize
			}
			if !sepConsumed {
				return false, ""
			}
			continue
		}

		if si >= len(s) {
			return false, ""
		}
		sr, sSize := utf8.DecodeRuneInString(s[si:])
		if FoldWakeRune(sr) != FoldWakeRune(cr) {
			return false, ""
		}
		lastCandidateRune = cr
		si += sSize
		ci += cSize
	}

	if !isCJKScript(lastCandidateRune) && si < len(s) {
		nextR, _ := utf8.DecodeRuneInString(s[si:])
		if (unicode.IsLetter(nextR) && !isCJKScript(nextR)) || unicode.IsDigit(nextR) {
			return false, ""
		}
	}

	return true, trimLeadingWakeSeparators(s[si:])
}

func stripOptionalLeadingGreeting(s string) (string, bool) {
	s = trimLeadingWakeSeparators(s)
	if s == "" {
		return "", false
	}
	for _, g := range englishGreetings {
		if ok, rest := matchWakeCandidateAtStart(s, g); ok && rest != "" {
			return rest, true
		}
	}
	for _, g := range chineseGreetings {
		if ok, rest := matchWakeCandidateAtStart(s, g); ok && rest != "" {
			return rest, true
		}
	}
	return "", false
}

// MatchWakePrefix checks whether text starts with assistantName or one of wakeAliases
// (optionally preceded by a single short English or Chinese greeting).
func MatchWakePrefix(text, assistantName string, wakeAliases []string) (matched bool, matchedWake string, remainder string) {
	candidates := orderedWakeCandidates(assistantName, wakeAliases)
	sort.SliceStable(candidates, func(i, j int) bool {
		return utf8.RuneCountInString(candidates[i]) > utf8.RuneCountInString(candidates[j])
	})

	trimmed := trimLeadingWakeSeparators(text)
	if trimmed == "" {
		return false, "", ""
	}

	for _, cand := range candidates {
		if ok, rest := matchWakeCandidateAtStart(trimmed, cand); ok {
			return true, cand, rest
		}
	}

	if afterGreeting, stripped := stripOptionalLeadingGreeting(trimmed); stripped {
		for _, cand := range candidates {
			if ok, rest := matchWakeCandidateAtStart(afterGreeting, cand); ok {
				return true, cand, rest
			}
		}
	}

	return false, "", ""
}

func foldWakeString(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		b.WriteRune(FoldWakeRune(r))
	}
	return b.String()
}

func splitQuestionClauses(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		switch r {
		case '.', '!', ';', ',', '—', '\n', '。', '！', '；', '，', '、':
			return true
		default:
			return false
		}
	})
}

func tokenizeLatinWords(s string) []string {
	var tokens []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			tokens = append(tokens, cur.String())
			cur.Reset()
		}
	}
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '\'' || r == '-' {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return tokens
}

func isEnglishClauseQuestion(words []string) bool {
	for len(words) > 0 {
		if _, ok := englishLeadDiscourse[words[0]]; ok {
			words = words[1:]
			continue
		}
		break
	}
	// Strip polite indirect question openers ("tell me what...", "can you tell me how...")
	if len(words) >= 3 && words[0] == "tell" && words[1] == "me" {
		words = words[2:]
	}
	if len(words) == 0 {
		return false
	}

	first := words[0]
	if _, ok := englishInterrogatives[first]; ok {
		return true
	}

	if len(words) >= 2 {
		second := words[1]
		if _, isAux := englishAuxiliaries[first]; isAux {
			// Avoid treating imperative "do it" as a question without '?'
			if first == "do" && second == "it" {
				return false
			}
			if _, isSubj := englishInversionSubjects[second]; isSubj {
				return true
			}
		}
	}

	// Who/what/how at the end ("for what", "or what", "how about...")
	last := words[len(words)-1]
	if len(words) >= 2 && (last == "right" || (words[len(words)-2] == "or" && last == "not")) {
		return true
	}
	return false
}

func isMalayClauseQuestion(words []string) bool {
	for _, w := range words {
		w = strings.Trim(w, "'-")
		if w == "" {
			continue
		}
		if _, ok := malayQuestionWords[w]; ok {
			return true
		}
		if len(w) >= 6 && strings.HasSuffix(w, "kah") {
			if _, excluded := malayNonQuestionKah[w]; !excluded {
				return true
			}
		}
	}
	return false
}

func isCJKClauseQuestion(foldedClause string) bool {
	foldedClause = strings.TrimSpace(foldedClause)
	if foldedClause == "" {
		return false
	}
	for _, sub := range cjkQuestionSubstrings {
		if strings.Contains(foldedClause, sub) {
			return true
		}
	}
	// Standalone '谁' (who)
	if strings.ContainsRune(foldedClause, '谁') {
		return true
	}
	// Trailing Mandarin question particle
	cleaned := strings.TrimRightFunc(foldedClause, isWakeSeparator)
	if cleaned != "" {
		lastR, _ := utf8.DecodeLastRuneInString(cleaned)
		for _, p := range cjkTrailingParticles {
			if lastR == p {
				return true
			}
		}
	}
	return false
}

// IsDirectQuestion reports whether the utterance contains a direct question in
// English, Mandarin Chinese, Cantonese, or Malay.
func IsDirectQuestion(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	if strings.ContainsRune(trimmed, '?') || strings.ContainsRune(trimmed, '？') {
		return true
	}

	folded := foldWakeString(trimmed)
	clauses := splitQuestionClauses(folded)
	for _, rawClause := range clauses {
		clause := strings.TrimSpace(rawClause)
		if clause == "" {
			continue
		}
		if isCJKClauseQuestion(clause) {
			return true
		}
		words := tokenizeLatinWords(clause)
		if len(words) > 0 && (isEnglishClauseQuestion(words) || isMalayClauseQuestion(words)) {
			return true
		}
	}
	return false
}

// IsWaitForQuestionReply reports whether an LLM reply matches the [WAIT_FOR_QUESTION] sentinel.
func IsWaitForQuestionReply(reply string) bool {
	cleaned := strings.TrimSpace(reply)
	if cleaned == "" {
		return false
	}
	return strings.EqualFold(cleaned, WaitForQuestionSentinel) ||
		strings.Contains(strings.ToUpper(cleaned), WaitForQuestionSentinel)
}

// WakeTurnGate manages per-session wake-name gating and the 15-second silent pause buffer.
type WakeTurnGate struct {
	mu         sync.Mutex
	window     time.Duration
	nowFn      func() time.Time
	armedUntil time.Time
	segments   []string
	timer      *time.Timer
	onExpire   func()
}

func NewWakeTurnGate(window time.Duration) *WakeTurnGate {
	if window <= 0 {
		window = DefaultWakeWindow
	}
	return &WakeTurnGate{
		window: window,
		nowFn:  time.Now,
	}
}

func (g *WakeTurnGate) SetClock(nowFn func() time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if nowFn != nil {
		g.nowFn = nowFn
	}
}

func (g *WakeTurnGate) SetOnExpire(fn func()) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.onExpire = fn
}

func (g *WakeTurnGate) IsArmed() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.nowFn()
	if g.armedUntil.IsZero() || !now.Before(g.armedUntil) {
		return false
	}
	return true
}

func (g *WakeTurnGate) Close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.timer != nil {
		g.timer.Stop()
		g.timer = nil
	}
	g.segments = nil
	g.armedUntil = time.Time{}
}

func (g *WakeTurnGate) resetTimerLocked() {
	if g.timer != nil {
		g.timer.Stop()
		g.timer = nil
	}
	if g.armedUntil.IsZero() {
		return
	}
	g.timer = time.AfterFunc(g.window, func() {
		var cb func()
		g.mu.Lock()
		now := g.nowFn()
		if !g.armedUntil.IsZero() && !now.Before(g.armedUntil) {
			g.segments = nil
			g.armedUntil = time.Time{}
			cb = g.onExpire
		}
		g.mu.Unlock()
		if cb != nil {
			cb()
		}
	})
}

func joinBufferedSegments(segments []string) string {
	var b strings.Builder
	for _, seg := range segments {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		if b.Len() > 0 {
			lastR, _ := utf8.DecodeLastRuneInString(b.String())
			firstR, _ := utf8.DecodeRuneInString(seg)
			if isCJKScript(lastR) && isCJKScript(firstR) {
				b.WriteString("，")
			} else {
				b.WriteByte(' ')
			}
		}
		b.WriteString(seg)
	}
	return b.String()
}

// Evaluate applies wake-name prefix gating, 15s armed thought accumulation, and direct-question detection.
func (g *WakeTurnGate) Evaluate(text, assistantName string, wakeAliases []string) WakeGateResult {
	g.mu.Lock()
	defer g.mu.Unlock()

	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return WakeGateResult{Action: WakeActionIgnore}
	}

	now := g.nowFn()
	if !g.armedUntil.IsZero() && !now.Before(g.armedUntil) {
		g.segments = nil
		g.armedUntil = time.Time{}
		if g.timer != nil {
			g.timer.Stop()
			g.timer = nil
		}
	}

	matched, matchedWake, remainder := MatchWakePrefix(trimmed, assistantName, wakeAliases)
	armed := !g.armedUntil.IsZero() && now.Before(g.armedUntil)

	if !matched && !armed {
		return WakeGateResult{Action: WakeActionIgnore}
	}

	var segment string
	if matched {
		segment = strings.TrimSpace(remainder)
	} else {
		segment = trimmed
	}

	if segment != "" {
		g.segments = append(g.segments, segment)
	}
	g.armedUntil = now.Add(g.window)
	g.resetTimerLocked()

	combined := joinBufferedSegments(g.segments)
	snapshot := append([]string(nil), g.segments...)

	if combined != "" && IsDirectQuestion(combined) {
		g.segments = nil
		g.armedUntil = time.Time{}
		if g.timer != nil {
			g.timer.Stop()
			g.timer = nil
		}
		return WakeGateResult{
			Action:       WakeActionRespond,
			WakeMatched:  matched,
			MatchedWake:  matchedWake,
			SegmentText:  segment,
			CombinedText: combined,
			Segments:     snapshot,
		}
	}

	return WakeGateResult{
		Action:       WakeActionBufferWait,
		WakeMatched:  matched,
		MatchedWake:  matchedWake,
		SegmentText:  segment,
		CombinedText: combined,
		Segments:     snapshot,
	}
}

// RestoreBuffered restores flushed segments back into the armed window when the LLM returns [WAIT_FOR_QUESTION].
func (g *WakeTurnGate) RestoreBuffered(segments []string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(segments) == 0 {
		return
	}
	if len(g.segments) == 0 {
		g.segments = append([]string(nil), segments...)
	} else {
		merged := make([]string, 0, len(segments)+len(g.segments))
		merged = append(merged, segments...)
		merged = append(merged, g.segments...)
		g.segments = merged
	}
	g.armedUntil = g.nowFn().Add(g.window)
	g.resetTimerLocked()
}
