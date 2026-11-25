package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

var loopback = net.IPv4(127, 0, 0, 1)

const (
	opusPT  = 111
	pcmuPT  = 0
	event48 = 110
	event8  = 126
)

var (
	voice = []byte{0xde, 0xad, 0xbe, 0xef, 1, 2, 3, 4}
	digit = []byte{5, 0x80 | 10, 0, 160}
)

type browser struct {
	pc    *webrtc.PeerConnection
	track *relayTrack
	got   chan *rtp.Packet
	offer []byte
}

func newBrowser(t *testing.T) *browser {
	t.Helper()
	m := &webrtc.MediaEngine{}
	for _, c := range []webrtc.RTPCodecParameters{
		{PayloadType: opusPT, RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2, SDPFmtpLine: "minptime=10;useinbandfec=1"}},
		{PayloadType: pcmuPT, RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypePCMU, ClockRate: 8000}},
		{PayloadType: event48, RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: "audio/telephone-event", ClockRate: 48000, SDPFmtpLine: "0-15"}},
		{PayloadType: event8, RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: "audio/telephone-event", ClockRate: 8000, SDPFmtpLine: "0-15"}},
	} {
		if err := m.RegisterCodec(c, webrtc.RTPCodecTypeAudio); err != nil {
			t.Fatal(err)
		}
	}
	pc, err := webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithSettingEngine(settings(loopback))).NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	b := &browser{pc: pc, track: &relayTrack{}, got: make(chan *rtp.Packet, 512)}
	sender, err := pc.AddTrack(b.track)
	if err != nil {
		t.Fatal(err)
	}
	go drain(sender)
	pc.OnTrack(func(tr *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		go func() {
			for {
				p, _, err := tr.ReadRTP()
				if err != nil {
					return
				}
				select {
				case b.got <- p:
				default:
				}
			}
		}()
	})
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gathered := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	<-gathered
	b.offer = []byte(pc.LocalDescription().SDP)
	return b
}

func (b *browser) accept(t *testing.T, answer []byte) {
	t.Helper()
	if err := b.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: string(answer)}); err != nil {
		t.Fatalf("the browser cannot use the answer: %v\n%s", err, answer)
	}
}

func (b *browser) send(pt uint8, seq uint16, payload []byte) {
	b.track.write(rtp.Header{Version: 2, PayloadType: pt, SequenceNumber: seq, Timestamp: uint32(seq) * 160}, payload)
}

type phone struct {
	conn *net.UDPConn
	got  chan *rtp.Packet
}

func newPhone(t *testing.T) *phone {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: loopback})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	p := &phone{conn: conn, got: make(chan *rtp.Packet, 512)}
	go func() {
		buf := make([]byte, 1500)
		for {
			n, _, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			var pkt rtp.Packet
			if pkt.Unmarshal(append([]byte(nil), buf[:n]...)) == nil {
				select {
				case p.got <- &pkt:
				default:
				}
			}
		}
	}()
	return p
}

func (p *phone) describe(lines ...string) []byte {
	port := p.conn.LocalAddr().(*net.UDPAddr).Port
	var formats []string
	for _, l := range lines {
		pt, _, _ := strings.Cut(strings.TrimPrefix(l, "a=rtpmap:"), " ")
		if strings.HasPrefix(l, "a=rtpmap:") {
			formats = append(formats, pt)
		}
	}
	return []byte(fmt.Sprintf("v=0\r\no=- 7 7 IN IP4 127.0.0.1\r\ns=phone\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\nm=audio %d RTP/AVP %s\r\n%s\r\na=sendrecv\r\n",
		port, strings.Join(formats, " "), strings.Join(lines, "\r\n")))
}

func (p *phone) send(t *testing.T, to *net.UDPAddr, pt uint8, seq uint16, payload []byte) {
	t.Helper()
	raw, err := (&rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: pt, SequenceNumber: seq, Timestamp: uint32(seq) * 160, SSRC: 0x1234}, Payload: payload}).Marshal()
	if err != nil {
		t.Fatal(err)
	}
	p.conn.WriteToUDP(raw, to)
}

// arrives sends a packet every 20 ms until one with the wanted payload type and
// payload arrives, as the first packets go before the browser's connection is up.
func arrives(t *testing.T, what string, send func(seq uint16), got <-chan *rtp.Packet, pt uint8, payload []byte) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for seq := uint16(1); ; {
		select {
		case p := <-got:
			if p.PayloadType == pt && bytes.Equal(p.Payload, payload) {
				return
			}
			if bytes.Equal(p.Payload, payload) {
				t.Fatalf("%s arrived with payload type %d, want %d", what, p.PayloadType, pt)
			}
		case <-tick.C:
			send(seq)
			seq++
		case <-deadline:
			t.Fatalf("%s did not arrive", what)
		}
	}
}

func bridge(t *testing.T, b *browser) *Bridge {
	t.Helper()
	br, err := New(loopback, b.offer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { br.Close() })
	return br
}

func address(t *testing.T, raw []byte) *net.UDPAddr {
	t.Helper()
	d, err := parse(raw)
	if err != nil || d.addr == nil {
		t.Fatalf("the phone cannot read %q: %v", raw, err)
	}
	return d.addr
}

func exchange(t *testing.T, b *browser, p *phone, gateway *net.UDPAddr, phoneVoice, phoneEvent, webVoice, webEvent uint8) {
	t.Helper()
	arrives(t, "the phone's voice", func(seq uint16) { p.send(t, gateway, phoneVoice, seq, voice) }, b.got, webVoice, voice)
	arrives(t, "the browser's voice", func(seq uint16) { b.send(webVoice, seq, voice) }, p.got, phoneVoice, voice)
	arrives(t, "a key pressed on the phone", func(seq uint16) { p.send(t, gateway, phoneEvent, seq, digit) }, b.got, webEvent, digit)
	arrives(t, "a key pressed in the browser", func(seq uint16) { b.send(webEvent, seq, digit) }, p.got, phoneEvent, digit)
}

func TestIncomingCallRelaysVoiceAndKeys(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	b, p := newBrowser(t), newPhone(t)
	br := bridge(t, b)
	phoneAnswer, browserAnswer, err := br.Answer(ctx, p.describe("a=rtpmap:8 PCMA/8000", "a=rtpmap:0 PCMU/8000", "a=rtpmap:101 telephone-event/8000", "a=fmtp:101 0-16"))
	if err != nil {
		t.Fatal(err)
	}
	answer := string(phoneAnswer)
	if !strings.Contains(answer, "m=audio ") || !strings.Contains(answer, " RTP/AVP 0 101\r\n") || !strings.Contains(answer, "a=rtpmap:101 telephone-event/8000") || strings.Contains(answer, "PCMA") {
		t.Fatalf("the phone's answer does not pick PCMU with key events:\n%s", answer)
	}
	b.accept(t, browserAnswer)
	exchange(t, b, p, address(t, phoneAnswer), pcmuPT, 101, pcmuPT, event8)
	if _, _, err := br.Answer(ctx, p.describe("a=rtpmap:0 PCMU/8000")); !errors.Is(err, ErrStarted) {
		t.Fatalf("a connected bridge answers again: %v", err)
	}
	br.Close()
	select {
	case <-br.Done():
	case <-time.After(time.Second):
		t.Fatal("the bridge did not close")
	}
}

func TestOutgoingCallPrefersOpus(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	b, p := newBrowser(t), newPhone(t)
	br := bridge(t, b)
	offer := string(br.Offer())
	for _, want := range []string{" RTP/AVP 111 0 110 126\r\n", "a=rtpmap:111 opus/48000/2", "a=rtpmap:0 PCMU/8000", "a=rtpmap:126 telephone-event/8000", "c=IN IP4 127.0.0.1"} {
		if !strings.Contains(offer, want) {
			t.Fatalf("the phone's offer lacks %q:\n%s", want, offer)
		}
	}
	browserAnswer, err := br.Accept(ctx, p.describe("a=rtpmap:96 opus/48000/2", "a=rtpmap:97 telephone-event/48000"))
	if err != nil {
		t.Fatal(err)
	}
	b.accept(t, browserAnswer)
	exchange(t, b, p, address(t, []byte(offer)), 96, 97, opusPT, event48)
}

func TestNoSharedCodec(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	b, p := newBrowser(t), newPhone(t)
	br := bridge(t, b)
	if _, _, err := br.Answer(ctx, p.describe("a=rtpmap:8 PCMA/8000")); !errors.Is(err, ErrCodec) {
		t.Fatalf("a phone without Opus or PCMU was answered: %v", err)
	}
	if _, err := New(loopback, []byte("v=0\r\n")); !errors.Is(err, ErrSession) {
		t.Fatalf("a broken browser offer was taken: %v", err)
	}
	if _, err := New(net.IPv4zero, b.offer); !errors.Is(err, ErrAddress) {
		t.Fatalf("an unspecified address was taken: %v", err)
	}
}
