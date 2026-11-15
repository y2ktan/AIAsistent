package rtc

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

func TestPeerConnectionDataChannelAndRevocation(t *testing.T) {
	mgr, err := NewManager(Config{}, nil)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	defer mgr.Close()

	// Create client-side Pion PeerConnection
	clientPC, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("client NewPeerConnection failed: %v", err)
	}
	defer func() { _ = clientPC.Close() }()

	audioTrack, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2},
		"audio",
		"client-mic",
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := clientPC.AddTrack(audioTrack); err != nil {
		t.Fatal(err)
	}

	dc, err := clientPC.CreateDataChannel("events", nil)
	if err != nil {
		t.Fatal(err)
	}

	receivedState := make(chan Event, 4)
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		var ev Event
		if err := json.Unmarshal(msg.Data, &ev); err == nil {
			receivedState <- ev
		}
	})

	offer, err := clientPC.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gather := webrtc.GatheringCompletePromise(clientPC)
	if err := clientPC.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	<-gather

	readyCh := make(chan *PeerSession, 1)
	peerID, answerSDP, err := mgr.HandleOffer(context.Background(), "user-123", "sess-123", clientPC.LocalDescription().SDP, func(ps *PeerSession) {
		readyCh <- ps
	})
	if err != nil {
		t.Fatalf("HandleOffer failed: %v", err)
	}
	if peerID == "" || answerSDP == "" {
		t.Fatal("expected non-empty peerID and answerSDP")
	}

	if err := clientPC.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answerSDP,
	}); err != nil {
		t.Fatalf("client SetRemoteDescription failed: %v", err)
	}

	var serverPeer *PeerSession
	select {
	case serverPeer = <-readyCh:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for serverPeer ready")
	}

	// Verify initial "listening" event received over SCTP data channel
	select {
	case ev := <-receivedState:
		if ev.Type != "state" || ev.State != "listening" {
			t.Fatalf("unexpected initial event: %+v", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for data channel event")
	}

	// Verify outbound audio sample write succeeds
	if err := serverPeer.WriteAudioSample([]byte{0xf8, 0xff, 0xfe}, 20*time.Millisecond); err != nil {
		t.Fatalf("WriteAudioSample failed: %v", err)
	}

	// Verify session revocation closes the peer
	mgr.RevokeByUser("user-123")
	select {
	case <-serverPeer.Context().Done():
	case <-time.After(3 * time.Second):
		t.Fatal("expected serverPeer context to cancel on RevokeByUser")
	}
}
