package rtc

import "time"

type Event struct {
	Type      string      `json:"type"`
	State     string      `json:"state,omitempty"`
	Role      string      `json:"role,omitempty"`
	Text      string      `json:"text,omitempty"`
	Tool      string      `json:"tool,omitempty"`
	Status    string      `json:"status,omitempty"`
	Message   string      `json:"message,omitempty"`
	Citations interface{} `json:"citations,omitempty"`
	Media     *MediaState `json:"media,omitempty"`
	Timestamp int64       `json:"ts"`
}

type MediaState struct {
	MicEnabled     bool `json:"micEnabled"`
	SpeakerEnabled bool `json:"speakerEnabled"`
	CameraEnabled  bool `json:"cameraEnabled"`
}

func NewStateEvent(state string) Event {
	return Event{Type: "state", State: state, Timestamp: time.Now().UnixMilli()}
}

func NewTranscriptEvent(eventType, role, text string) Event {
	return Event{Type: eventType, Role: role, Text: text, Timestamp: time.Now().UnixMilli()}
}

func NewBargeInEvent() Event {
	return Event{Type: "barge_in", Timestamp: time.Now().UnixMilli()}
}

func NewErrorEvent(msg string) Event {
	return Event{Type: "error", Message: msg, Timestamp: time.Now().UnixMilli()}
}
