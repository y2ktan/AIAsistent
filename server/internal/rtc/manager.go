package rtc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

type Config struct {
	UDPMuxPort int
	PublicIP   string
	STUNURLs   []string
}

type AudioFrame struct {
	Payload   []byte
	Timestamp uint32
}

type PeerSession struct {
	ID            string
	UserID        string
	SessionID     string
	PC            *webrtc.PeerConnection
	AudioOut      *webrtc.TrackLocalStaticSample
	AudioIn       chan AudioFrame
	OnBargeIn     func()
	OnSpeechStart func()
	OnSpeechEnd   func()
	OnClose       func()

	mu            sync.RWMutex
	eventsDC      *webrtc.DataChannel
	mediaState    MediaState
	latestFrame   []byte
	lastFrameTime time.Time
	closed        bool
	cancel        context.CancelFunc
	ctx           context.Context
}

func (ps *PeerSession) Context() context.Context {
	return ps.ctx
}

func (ps *PeerSession) SendEvent(ev Event) {
	ps.mu.RLock()
	dc := ps.eventsDC
	closed := ps.closed
	ps.mu.RUnlock()

	if closed || dc == nil || dc.ReadyState() != webrtc.DataChannelStateOpen {
		return
	}
	b, err := json.Marshal(ev)
	if err != nil {
		return
	}
	_ = dc.SendText(string(b))
}

func (ps *PeerSession) WriteAudioSample(data []byte, duration time.Duration) error {
	ps.mu.RLock()
	closed := ps.closed
	speakerEnabled := ps.mediaState.SpeakerEnabled
	ps.mu.RUnlock()

	if closed || !speakerEnabled || ps.AudioOut == nil {
		return nil
	}
	return ps.AudioOut.WriteSample(media.Sample{
		Data:     data,
		Duration: duration,
	})
}

func (ps *PeerSession) SetMediaState(ms MediaState) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.mediaState = ms
	if !ms.CameraEnabled {
		ps.latestFrame = nil
	}
}

func (ps *PeerSession) GetMediaState() MediaState {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ps.mediaState
}

func (ps *PeerSession) LatestVideoFrame() ([]byte, bool) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	if !ps.mediaState.CameraEnabled || len(ps.latestFrame) == 0 {
		return nil, false
	}
	cp := make([]byte, len(ps.latestFrame))
	copy(cp, ps.latestFrame)
	return cp, true
}

func (ps *PeerSession) Close() {
	ps.mu.Lock()
	if ps.closed {
		ps.mu.Unlock()
		return
	}
	ps.closed = true
	ps.latestFrame = nil
	ps.mu.Unlock()

	if ps.cancel != nil {
		ps.cancel()
	}
	if ps.PC != nil {
		_ = ps.PC.Close()
	}
	if ps.OnClose != nil {
		ps.OnClose()
	}
}

type Manager struct {
	api      *webrtc.API
	iceConf  webrtc.Configuration
	logger   *slog.Logger
	mu       sync.RWMutex
	sessions map[string]*PeerSession
	udpConn  *net.UDPConn
	tcpLn    net.Listener
}

func NewManager(cfg Config, logger *slog.Logger) (*Manager, error) {
	me := &webrtc.MediaEngine{}
	if err := me.RegisterDefaultCodecs(); err != nil {
		return nil, fmt.Errorf("register codecs: %w", err)
	}

	se := webrtc.SettingEngine{}
	var udpConn *net.UDPConn
	var tcpLn net.Listener

	if cfg.UDPMuxPort > 0 {
		addr := &net.UDPAddr{IP: net.IPv4zero, Port: cfg.UDPMuxPort}
		var err error
		udpConn, err = net.ListenUDP("udp4", addr)
		if err != nil {
			return nil, fmt.Errorf("listen udp mux port %d: %w", cfg.UDPMuxPort, err)
		}
		se.SetICEUDPMux(webrtc.NewICEUDPMux(nil, udpConn))

		tcpAddr := &net.TCPAddr{IP: net.IPv4zero, Port: cfg.UDPMuxPort}
		tcpLn, err = net.ListenTCP("tcp4", tcpAddr)
		if err == nil {
			se.SetICETCPMux(webrtc.NewICETCPMux(nil, tcpLn, 8))
		}
	}

	if cfg.PublicIP != "" {
		se.SetNAT1To1IPs([]string{cfg.PublicIP}, webrtc.ICECandidateTypeHost)
	}

	api := webrtc.NewAPI(webrtc.WithMediaEngine(me), webrtc.WithSettingEngine(se))

	var iceServers []webrtc.ICEServer
	if len(cfg.STUNURLs) > 0 {
		iceServers = append(iceServers, webrtc.ICEServer{URLs: cfg.STUNURLs})
	}

	return &Manager{
		api:      api,
		iceConf:  webrtc.Configuration{ICEServers: iceServers},
		logger:   logger,
		sessions: make(map[string]*PeerSession),
		udpConn:  udpConn,
		tcpLn:    tcpLn,
	}, nil
}

func (m *Manager) Close() {
	m.mu.Lock()
	peers := make([]*PeerSession, 0, len(m.sessions))
	for _, p := range m.sessions {
		peers = append(peers, p)
	}
	m.sessions = make(map[string]*PeerSession)
	m.mu.Unlock()

	for _, p := range peers {
		p.Close()
	}
	if m.udpConn != nil {
		_ = m.udpConn.Close()
	}
	if m.tcpLn != nil {
		_ = m.tcpLn.Close()
	}
}

func (m *Manager) RevokeByUser(userID string) {
	m.mu.RLock()
	var toClose []*PeerSession
	for _, p := range m.sessions {
		if p.UserID == userID {
			toClose = append(toClose, p)
		}
	}
	m.mu.RUnlock()
	for _, p := range toClose {
		p.Close()
	}
}

func (m *Manager) RevokeBySession(sessionID string) {
	m.mu.RLock()
	var toClose []*PeerSession
	for _, p := range m.sessions {
		if p.SessionID == sessionID {
			toClose = append(toClose, p)
		}
	}
	m.mu.RUnlock()
	for _, p := range toClose {
		p.Close()
	}
}

func (m *Manager) HandleOffer(ctx context.Context, userID, sessionID, sdpOffer string, onReady func(*PeerSession)) (string, string, error) {
	pc, err := m.api.NewPeerConnection(m.iceConf)
	if err != nil {
		return "", "", fmt.Errorf("create peer connection: %w", err)
	}

	audioOut, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2},
		"audio",
		"aegis-assistant",
	)
	if err != nil {
		_ = pc.Close()
		return "", "", fmt.Errorf("create outbound audio track: %w", err)
	}
	if _, err := pc.AddTrack(audioOut); err != nil {
		_ = pc.Close()
		return "", "", fmt.Errorf("add outbound audio track: %w", err)
	}

	peerCtx, cancel := context.WithCancel(context.Background())
	ps := &PeerSession{
		ID:        uuid.NewString(),
		UserID:    userID,
		SessionID: sessionID,
		PC:        pc,
		AudioOut:  audioOut,
		AudioIn:   make(chan AudioFrame, 128),
		mediaState: MediaState{
			MicEnabled:     true,
			SpeakerEnabled: true,
			CameraEnabled:  false,
		},
		cancel: cancel,
		ctx:    peerCtx,
	}

	m.mu.Lock()
	m.sessions[ps.ID] = ps
	m.mu.Unlock()

	cleanup := func() {
		m.mu.Lock()
		delete(m.sessions, ps.ID)
		m.mu.Unlock()
		ps.Close()
	}

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		if state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed {
			cleanup()
		}
	})

	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		if dc.Label() != "events" {
			return
		}
		ps.mu.Lock()
		ps.eventsDC = dc
		ps.mu.Unlock()

		dc.OnOpen(func() {
			ps.SendEvent(NewStateEvent("listening"))
			if onReady != nil {
				go onReady(ps)
			}
		})

		dc.OnMessage(func(msg webrtc.DataChannelMessage) {
			if msg.IsString {
				var ev Event
				if err := json.Unmarshal(msg.Data, &ev); err == nil {
					switch ev.Type {
					case "media.toggle":
						if ev.Media != nil {
							ps.SetMediaState(*ev.Media)
						}
					case "barge_in":
						if ps.OnBargeIn != nil {
							ps.OnBargeIn()
						}
					case "vad.speech_start":
						if ps.OnSpeechStart != nil {
							ps.OnSpeechStart()
						}
					case "vad.speech_end":
						if ps.OnSpeechEnd != nil {
							ps.OnSpeechEnd()
						}
					case "vision.frame":
						if ev.Text != "" {
							ps.mu.Lock()
							if ps.mediaState.CameraEnabled {
								ps.latestFrame = []byte(ev.Text)
								ps.lastFrameTime = time.Now()
							}
							ps.mu.Unlock()
						}
					}
				}
			}
		})
	})

	pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		kind := track.Kind()
		if kind == webrtc.RTPCodecTypeAudio {
			go func() {
				for {
					select {
					case <-peerCtx.Done():
						return
					default:
					}
					pkt, _, err := track.ReadRTP()
					if err != nil {
						if err != io.EOF && m.logger != nil {
							m.logger.Debug("audio track read ended", "peerId", ps.ID)
						}
						return
					}
					ps.mu.RLock()
					micOn := ps.mediaState.MicEnabled
					ps.mu.RUnlock()
					if !micOn || len(pkt.Payload) == 0 {
						continue
					}
					select {
					case ps.AudioIn <- AudioFrame{Payload: pkt.Payload, Timestamp: pkt.Timestamp}:
					default:
						// Drop oldest frame if buffer full to preserve real-time latency
					}
				}
			}()
		} else if kind == webrtc.RTPCodecTypeVideo {
			go func() {
				for {
					select {
					case <-peerCtx.Done():
						return
					default:
					}
					pkt, _, err := track.ReadRTP()
					if err != nil {
						return
					}
					// On-demand frame sampling: store latest payload at most every 500ms while camera enabled
					ps.mu.Lock()
					if ps.mediaState.CameraEnabled && time.Since(ps.lastFrameTime) >= 500*time.Millisecond && len(pkt.Payload) > 0 {
						cp := make([]byte, len(pkt.Payload))
						copy(cp, pkt.Payload)
						ps.latestFrame = cp
						ps.lastFrameTime = time.Now()
					}
					ps.mu.Unlock()
				}
			}()
		}
	})

	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  sdpOffer,
	}
	if err := pc.SetRemoteDescription(offer); err != nil {
		cleanup()
		return "", "", fmt.Errorf("set remote description: %w", err)
	}

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		cleanup()
		return "", "", fmt.Errorf("create answer: %w", err)
	}

	gatherComplete := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(answer); err != nil {
		cleanup()
		return "", "", fmt.Errorf("set local description: %w", err)
	}

	select {
	case <-gatherComplete:
	case <-ctx.Done():
		cleanup()
		return "", "", ctx.Err()
	case <-time.After(5 * time.Second):
	}

	localDesc := pc.LocalDescription()
	if localDesc == nil {
		cleanup()
		return "", "", fmt.Errorf("missing local description")
	}
	return ps.ID, localDesc.SDP, nil
}
