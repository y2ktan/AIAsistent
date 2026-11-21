package agent

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMatchWakePrefix(t *testing.T) {
	aliases := []string{"小智", "贾维斯", "Aegis Pro", "Alex"}

	tests := []struct {
		name        string
		text        string
		primary     string
		aliases     []string
		wantMatched bool
		wantWake    string
		wantRest    string
	}{
		{
			name:        "default English primary name",
			text:        "Aegis, what is the status?",
			primary:     "",
			aliases:     nil,
			wantMatched: true,
			wantWake:    "Aegis",
			wantRest:    "what is the status?",
		},
		{
			name:        "English greeting before primary name",
			text:        "Hey Aegis, why did latency spike?",
			primary:     "Aegis",
			aliases:     aliases,
			wantMatched: true,
			wantWake:    "Aegis",
			wantRest:    "why did latency spike?",
		},
		{
			name:        "longest wake phrase wins",
			text:        "Hi Aegis Pro! Can you check logs?",
			primary:     "Aegis",
			aliases:     aliases,
			wantMatched: true,
			wantWake:    "Aegis Pro",
			wantRest:    "Can you check logs?",
		},
		{
			name:        "Latin word boundary rejects lookalike prefix",
			text:        "Alexander, what time is it?",
			primary:     "Aegis",
			aliases:     aliases,
			wantMatched: false,
		},
		{
			name:        "greeting alone without wake name rejected",
			text:        "Hello everyone, how is your afternoon?",
			primary:     "Aegis",
			aliases:     aliases,
			wantMatched: false,
		},
		{
			name:        "English name followed immediately by CJK text",
			text:        "Aegis今天有什么安排？",
			primary:     "Aegis",
			aliases:     aliases,
			wantMatched: true,
			wantWake:    "Aegis",
			wantRest:    "今天有什么安排？",
		},
		{
			name:        "Chinese wake alias without space delimiter",
			text:        "小智今天有什么安排？",
			primary:     "Aegis",
			aliases:     aliases,
			wantMatched: true,
			wantWake:    "小智",
			wantRest:    "今天有什么安排？",
		},
		{
			name:        "Chinese greeting + Chinese wake alias",
			text:        "你好小智，服务器正常吗？",
			primary:     "Aegis",
			aliases:     aliases,
			wantMatched: true,
			wantWake:    "小智",
			wantRest:    "服务器正常吗？",
		},
		{
			name:        "Traditional CJK transcript matches Simplified wake alias",
			text:        "嗨賈維斯，今天天氣如何",
			primary:     "Aegis",
			aliases:     aliases,
			wantMatched: true,
			wantWake:    "贾维斯",
			wantRest:    "今天天氣如何",
		},
		{
			name:        "wake name only with no trailing text",
			text:        "Hey Aegis!",
			primary:     "Aegis",
			aliases:     nil,
			wantMatched: true,
			wantWake:    "Aegis",
			wantRest:    "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotMatched, gotWake, gotRest := MatchWakePrefix(tc.text, tc.primary, tc.aliases)
			if gotMatched != tc.wantMatched {
				t.Fatalf("MatchWakePrefix(%q) matched = %v, want %v", tc.text, gotMatched, tc.wantMatched)
			}
			if !tc.wantMatched {
				return
			}
			if gotWake != tc.wantWake {
				t.Fatalf("MatchWakePrefix(%q) wake = %q, want %q", tc.text, gotWake, tc.wantWake)
			}
			if gotRest != tc.wantRest {
				t.Fatalf("MatchWakePrefix(%q) remainder = %q, want %q", tc.text, gotRest, tc.wantRest)
			}
		})
	}
}

func TestIsDirectQuestion(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		// Questions -> true
		{"What is on my desk?", true},
		{"what is on my desk", true},
		{"let me look at the third-quarter numbers, why did server latency spike", true},
		{"can you check the deployment logs", true},
		{"is the database healthy", true},
		{"do we have any active incidents", true},
		{"今天有什么安排", true},
		{"服务器是不是挂了", true},
		{"今天几号", true},
		{"你好嗎請問", true},
		{"點解服務器咁慢", true},
		{"今日有冇會議", true},
		{"係唔係已經部署咗", true},
		{"Apakah status pelayan sekarang", true},
		{"Bagaimana prestasi suku ketiga", true},
		{"Bolehkah awak semak log", true},
		{"Perlukah kita mulakan semula pelayan", true},

		// Declarative statements & thinking pauses -> false
		{"", false},
		{"I am opening the dashboard now.", false},
		{"Let me look at the third-quarter numbers...", false},
		{"Hmm, revenue was up ten percent in July, and August looked flat.", false},
		{"The server is running on port 8080.", false},
		{"Do it right away.", false},
		{"我正在打开控制台。", false},
		{"我先看一下第三季度的数据", false},
		{"我而家睇緊個表", false},
		{"Saya sedang membuka papan pemuka sekarang.", false},
		{"Langkah seterusnya sudah siap.", false},
	}

	for _, tc := range tests {
		if got := IsDirectQuestion(tc.text); got != tc.want {
			t.Errorf("IsDirectQuestion(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

func TestWakeTurnGateAccumulationAndExpiry(t *testing.T) {
	now := time.Date(2026, 11, 21, 15, 0, 0, 0, time.UTC)
	gate := NewWakeTurnGate(15 * time.Second)
	defer gate.Close()
	gate.SetClock(func() time.Time { return now })

	// 1. Background speech while unarmed -> Ignore
	res := gate.Evaluate("Did you watch the game last night?", "Aegis", []string{"小智"})
	if res.Action != WakeActionIgnore || gate.IsArmed() {
		t.Fatalf("expected background question without wake name to be ignored, got %+v", res)
	}

	// 2. Wake-addressed thinking out loud (no question yet) -> BufferWait
	res = gate.Evaluate("Hey Aegis, let me look at the third-quarter numbers...", "Aegis", []string{"小智"})
	if res.Action != WakeActionBufferWait || !res.WakeMatched || !gate.IsArmed() {
		t.Fatalf("expected BufferWait + armed window, got %+v", res)
	}

	// 3. Additional paused thought after 6s -> BufferWait (refreshes 15s window)
	now = now.Add(6 * time.Second)
	res = gate.Evaluate("August traffic was flat, September jumped.", "Aegis", []string{"小智"})
	if res.Action != WakeActionBufferWait || res.WakeMatched || !gate.IsArmed() {
		t.Fatalf("expected BufferWait on paused continuation, got %+v", res)
	}

	// 4. Direct question 10s after refresh (16s after initial wake, still within refreshed 15s) -> Respond with full context
	now = now.Add(10 * time.Second)
	res = gate.Evaluate("why did server latency spike?", "Aegis", []string{"小智"})
	if res.Action != WakeActionRespond || gate.IsArmed() {
		t.Fatalf("expected Respond + disarmed gate, got %+v (armed=%v)", res, gate.IsArmed())
	}
	if !strings.Contains(res.CombinedText, "third-quarter numbers") ||
		!strings.Contains(res.CombinedText, "September jumped") ||
		!strings.Contains(res.CombinedText, "why did server latency spike?") {
		t.Fatalf("expected combined context across pauses, got %q", res.CombinedText)
	}

	// 5. Armed window expires silently after 15s when user abandons thought
	res = gate.Evaluate("小智，我先看下日志", "Aegis", []string{"小智"})
	if res.Action != WakeActionBufferWait || !gate.IsArmed() {
		t.Fatalf("expected Chinese wake phrase to arm window, got %+v", res)
	}
	now = now.Add(16 * time.Second)
	if gate.IsArmed() {
		t.Fatal("expected wake window to be expired after 16s")
	}
	res = gate.Evaluate("随便聊聊别的", "Aegis", []string{"小智"})
	if res.Action != WakeActionIgnore {
		t.Fatalf("expected post-expiry unaddressed speech to be ignored, got %+v", res)
	}

	// 6. RestoreBuffered re-arms segments if LLM returns [WAIT_FOR_QUESTION]
	if !IsWaitForQuestionReply("  [WAIT_FOR_QUESTION] ") {
		t.Fatal("expected IsWaitForQuestionReply to match sentinel")
	}
	gate.RestoreBuffered([]string{"Checking node memory metrics."})
	if !gate.IsArmed() {
		t.Fatal("expected RestoreBuffered to re-arm gate")
	}
	res = gate.Evaluate("what is the current heap usage?", "Aegis", nil)
	if res.Action != WakeActionRespond || !strings.Contains(res.CombinedText, "Checking node memory metrics.") {
		t.Fatalf("expected restored buffer to merge with follow-up question, got %+v", res)
	}
}

func TestWakeTurnGateTimerCallbackOnRealExpiry(t *testing.T) {
	var expired atomic.Bool
	gate := NewWakeTurnGate(40 * time.Millisecond)
	defer gate.Close()
	gate.SetOnExpire(func() { expired.Store(true) })

	res := gate.Evaluate("Aegis, thinking out loud", "Aegis", nil)
	if res.Action != WakeActionBufferWait {
		t.Fatalf("expected BufferWait, got %+v", res)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if expired.Load() && !gate.IsArmed() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expected real timer to expire armed wake window silently")
}
