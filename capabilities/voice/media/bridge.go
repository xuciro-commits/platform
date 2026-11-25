// Package media relays a call's audio between the phone line's plain RTP and the
// browser's WebRTC. Both sides use a codec they share (Opus or PCMU); packets are
// forwarded as they are, only the payload type and SSRC are rewritten. Key presses
// (RFC 4733 telephone-event) travel the same way.
package media

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

var (
	ErrSession = errors.New("voice media: the session description cannot be used")
	ErrCodec   = errors.New("voice media: the phone and the browser share no codec")
	ErrStarted = errors.New("voice media: the bridge is already connected")
	ErrAddress = errors.New("voice media: an address the phone and the browser can reach is required")
)

// Bridge joins one call's phone leg and browser leg. Create it with the browser's
// offer, then either Answer the phone's offer (incoming call) or send Offer to the
// phone and Accept its answer (outgoing call). Close it when the call ends.
type Bridge struct {
	address net.IP
	offer   []byte
	browser description
	conn    *net.UDPConn
	track   *relayTrack
	phoneIP net.IP
	phone   atomic.Pointer[net.UDPAddr]
	toPhone map[uint8]uint8
	toWeb   map[uint8]uint8
	done    chan struct{}
	once    sync.Once
	mu      sync.Mutex
	started bool
	pc      *webrtc.PeerConnection
	stats   counters
}

type counters struct {
	fromPhone, strangers, unknownPhone, fromBrowser, unknownBrowser, toPhone atomic.Int64
	phoneBytes, browserBytes, browserLoud, toBrowser                         atomic.Int64
}

// Stats counts what the bridge carried: packets from the phone and the browser,
// those it dropped (wrong source, payload type not negotiated), and where it sent.
type Stats struct {
	Phone          string
	FromPhone      int64
	Strangers      int64
	UnknownPhone   int64
	FromBrowser    int64
	UnknownBrowser int64
	ToPhone        int64
	ToBrowser      int64
	Browser        string
	// PhoneBytes and BrowserBytes are payload bytes; BrowserLoud counts browser
	// packets large enough to carry speech rather than comfort noise.
	PhoneBytes   int64
	BrowserBytes int64
	BrowserLoud  int64
}

func (b *Bridge) Stats() Stats {
	s := Stats{FromPhone: b.stats.fromPhone.Load(), Strangers: b.stats.strangers.Load(), UnknownPhone: b.stats.unknownPhone.Load(),
		FromBrowser: b.stats.fromBrowser.Load(), UnknownBrowser: b.stats.unknownBrowser.Load(), ToPhone: b.stats.toPhone.Load(),
		PhoneBytes: b.stats.phoneBytes.Load(), BrowserBytes: b.stats.browserBytes.Load(), BrowserLoud: b.stats.browserLoud.Load(), ToBrowser: b.stats.toBrowser.Load()}
	if p := b.phone.Load(); p != nil {
		s.Phone = p.String()
	}
	b.mu.Lock()
	if b.pc != nil {
		s.Browser = b.pc.ConnectionState().String()
	}
	b.mu.Unlock()
	return s
}

// New opens the phone-side RTP port on address, the one address the phone and the
// browser reach the gateway at; WebRTC gathers its candidates on it as well.
func New(address net.IP, browserOffer []byte) (*Bridge, error) {
	if address == nil || address.IsUnspecified() {
		return nil, ErrAddress
	}
	browser, err := parse(browserOffer)
	if err != nil {
		return nil, err
	}
	if _, ok := browser.voice(); !ok {
		return nil, ErrCodec
	}
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: address})
	if err != nil {
		return nil, err
	}
	return &Bridge{address: address, offer: browserOffer, browser: browser, conn: conn, track: &relayTrack{}, done: make(chan struct{})}, nil
}

// Offer is the session description to send the phone when the gateway dials out:
// every codec of Opus and PCMU the browser offered, with their key-press events.
func (b *Bridge) Offer() []byte {
	var codecs, events []codec
	for _, want := range voices {
		if c, ok := b.browser.find(want.name, want.rate); ok {
			codecs = append(codecs, c)
			if e, ok := b.browser.find(eventName, want.rate); ok {
				events = append(events, e)
			}
		}
	}
	return describe(b.local(), append(codecs, events...))
}

// Answer takes the phone's offer of an incoming call and returns the answer for
// the phone's 200 OK and the answer for the browser.
func (b *Bridge) Answer(ctx context.Context, phoneOffer []byte) (phone, browser []byte, err error) {
	desc, err := parse(phoneOffer)
	if err != nil {
		return nil, nil, err
	}
	pv, bv, ok := shared(desc, b.browser)
	if !ok {
		return nil, nil, ErrCodec
	}
	pe, be := events(desc, b.browser, pv.rate)
	browser, err = b.start(ctx, desc, pv, bv, pe, be)
	if err != nil {
		return nil, nil, err
	}
	codecs := []codec{pv}
	if pe != nil {
		codecs = append(codecs, *pe)
	}
	return describe(b.local(), codecs), browser, nil
}

// Accept takes the phone's answer to Offer and returns the answer for the browser.
func (b *Bridge) Accept(ctx context.Context, phoneAnswer []byte) ([]byte, error) {
	desc, err := parse(phoneAnswer)
	if err != nil {
		return nil, err
	}
	pv, bv, ok := shared(desc, b.browser)
	if !ok {
		return nil, ErrCodec
	}
	pe, be := events(desc, b.browser, pv.rate)
	return b.start(ctx, desc, pv, bv, pe, be)
}

// Done is closed when the bridge closes, including when the browser's connection fails.
func (b *Bridge) Done() <-chan struct{} { return b.done }

func (b *Bridge) Close() error {
	b.once.Do(func() {
		b.conn.Close()
		b.mu.Lock()
		pc := b.pc
		b.mu.Unlock()
		if pc != nil {
			pc.Close()
		}
		close(b.done)
	})
	return nil
}

var voices = []codec{{name: "opus", rate: 48000}, {name: "pcmu", rate: 8000}}

func shared(phone, browser description) (codec, codec, bool) {
	for _, want := range voices {
		p, inPhone := phone.find(want.name, want.rate)
		w, inBrowser := browser.find(want.name, want.rate)
		if inPhone && inBrowser {
			return p, w, true
		}
	}
	return codec{}, codec{}, false
}

func events(phone, browser description, rate uint32) (*codec, *codec) {
	p, inPhone := phone.find(eventName, rate)
	w, inBrowser := browser.find(eventName, rate)
	if !inPhone || !inBrowser {
		return nil, nil
	}
	return &p, &w
}

func (b *Bridge) local() *net.UDPAddr { return b.conn.LocalAddr().(*net.UDPAddr) }

func (b *Bridge) start(ctx context.Context, phone description, pv, bv codec, pe, be *codec) ([]byte, error) {
	if phone.addr == nil {
		return nil, ErrSession
	}
	b.mu.Lock()
	if b.started {
		b.mu.Unlock()
		return nil, ErrStarted
	}
	b.started = true
	b.mu.Unlock()
	b.phoneIP = phone.addr.IP
	b.phone.Store(phone.addr)
	b.toWeb, b.toPhone = map[uint8]uint8{pv.pt: bv.pt}, map[uint8]uint8{bv.pt: pv.pt}
	m := &webrtc.MediaEngine{}
	register := []codec{bv}
	if pe != nil {
		b.toWeb[pe.pt], b.toPhone[be.pt] = be.pt, pe.pt
		register = append(register, *be)
	}
	for _, c := range register {
		params := webrtc.RTPCodecParameters{PayloadType: webrtc.PayloadType(c.pt),
			RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: c.mime(), ClockRate: c.rate, Channels: c.channels, SDPFmtpLine: c.fmtp}}
		if err := m.RegisterCodec(params, webrtc.RTPCodecTypeAudio); err != nil {
			return nil, err
		}
	}
	api := webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithSettingEngine(settings(b.address)))
	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	b.pc = pc
	b.mu.Unlock()
	select {
	case <-b.done:
		pc.Close()
		return nil, net.ErrClosed
	default:
	}
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		if s == webrtc.PeerConnectionStateFailed || s == webrtc.PeerConnectionStateClosed {
			go b.Close()
		}
	})
	pc.OnTrack(func(t *webrtc.TrackRemote, _ *webrtc.RTPReceiver) { go b.fromBrowser(t) })
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: string(b.offer)}); err != nil {
		return nil, errors.Join(ErrSession, err)
	}
	sender, err := pc.AddTrack(b.track)
	if err != nil {
		return nil, err
	}
	go drain(sender)
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		return nil, err
	}
	gathered := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(answer); err != nil {
		return nil, err
	}
	select {
	case <-gathered:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	go b.fromPhone()
	return []byte(pc.LocalDescription().SDP), nil
}

func settings(address net.IP) webrtc.SettingEngine {
	var s webrtc.SettingEngine
	s.SetIPFilter(func(ip net.IP) bool { return ip.Equal(address) })
	s.SetIncludeLoopbackCandidate(address.IsLoopback())
	return s
}

func drain(sender *webrtc.RTPSender) {
	buf := make([]byte, 1500)
	for {
		if _, _, err := sender.Read(buf); err != nil {
			return
		}
	}
}

func (b *Bridge) fromPhone() {
	buf := make([]byte, 1500)
	var p rtp.Packet
	for {
		n, src, err := b.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		if !src.IP.Equal(b.phoneIP) || p.Unmarshal(buf[:n]) != nil {
			b.stats.strangers.Add(1)
			continue
		}
		pt, ok := b.toWeb[p.PayloadType]
		if !ok {
			b.stats.unknownPhone.Add(1)
			continue
		}
		b.stats.fromPhone.Add(1)
		b.stats.phoneBytes.Add(int64(len(p.Payload)))
		if cur := b.phone.Load(); cur.Port != src.Port || !cur.IP.Equal(src.IP) {
			b.phone.Store(src)
		}
		h := plain(p.Header)
		h.PayloadType = pt
		if b.track.write(h, p.Payload) {
			b.stats.toBrowser.Add(1)
		}
	}
}

func (b *Bridge) fromBrowser(t *webrtc.TrackRemote) {
	buf := make([]byte, 1500)
	for {
		p, _, err := t.ReadRTP()
		if err != nil {
			return
		}
		pt, ok := b.toPhone[p.PayloadType]
		if !ok {
			b.stats.unknownBrowser.Add(1)
			continue
		}
		b.stats.fromBrowser.Add(1)
		b.stats.browserBytes.Add(int64(len(p.Payload)))
		if len(p.Payload) > 20 {
			b.stats.browserLoud.Add(1)
		}
		out := rtp.Packet{Header: plain(p.Header), Payload: p.Payload}
		out.PayloadType = pt
		n, err := out.MarshalTo(buf)
		if err != nil {
			continue
		}
		if _, err := b.conn.WriteToUDP(buf[:n], b.phone.Load()); err == nil {
			b.stats.toPhone.Add(1)
		}
	}
}

// plain drops what one side negotiated and the other did not: header extensions and padding.
func plain(h rtp.Header) rtp.Header {
	h.Padding, h.Extension, h.Extensions, h.ExtensionProfile = false, false, nil, 0
	return h
}

// relayTrack sends packets as they arrive, keeping their payload type, so voice and
// key-press events share one track. It binds to the one voice codec negotiated.
type relayTrack struct {
	mu     sync.RWMutex
	writer webrtc.TrackLocalWriter
	ssrc   webrtc.SSRC
}

func (t *relayTrack) Bind(ctx webrtc.TrackLocalContext) (webrtc.RTPCodecParameters, error) {
	for _, c := range ctx.CodecParameters() {
		if voiceCodec(strings.ToLower(strings.TrimPrefix(c.MimeType, "audio/")), c.ClockRate) {
			t.mu.Lock()
			t.writer, t.ssrc = ctx.WriteStream(), ctx.SSRC()
			t.mu.Unlock()
			return c, nil
		}
	}
	return webrtc.RTPCodecParameters{}, webrtc.ErrUnsupportedCodec
}

func (t *relayTrack) Unbind(webrtc.TrackLocalContext) error {
	t.mu.Lock()
	t.writer = nil
	t.mu.Unlock()
	return nil
}

func (t *relayTrack) ID() string                { return "voice" }
func (t *relayTrack) RID() string               { return "" }
func (t *relayTrack) StreamID() string          { return "call" }
func (t *relayTrack) Kind() webrtc.RTPCodecType { return webrtc.RTPCodecTypeAudio }

func (t *relayTrack) write(h rtp.Header, payload []byte) bool {
	t.mu.RLock()
	w, ssrc := t.writer, t.ssrc
	t.mu.RUnlock()
	if w == nil {
		return false
	}
	h.SSRC = uint32(ssrc)
	_, err := w.WriteRTP(&h, payload)
	return err == nil
}
