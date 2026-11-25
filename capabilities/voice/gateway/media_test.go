package gateway

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

var sample = []byte{0xca, 0xfe, 0xba, 0xbe, 9, 8, 7, 6}

type agent struct {
	pc    *webrtc.PeerConnection
	track *webrtc.TrackLocalStaticRTP
	got   chan *rtp.Packet
	offer []byte
}

func newAgent(t *testing.T) *agent {
	t.Helper()
	m := &webrtc.MediaEngine{}
	if err := m.RegisterDefaultCodecs(); err != nil {
		t.Fatal(err)
	}
	// Browsers offer key-press events next to their voice codecs; pion's defaults do not.
	event := webrtc.RTPCodecParameters{PayloadType: 126, RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: "audio/telephone-event", ClockRate: 8000}}
	if err := m.RegisterCodec(event, webrtc.RTPCodecTypeAudio); err != nil {
		t.Fatal(err)
	}
	var s webrtc.SettingEngine
	s.SetIncludeLoopbackCandidate(true)
	s.SetIPFilter(func(ip net.IP) bool { return ip.IsLoopback() })
	pc, err := webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithSettingEngine(s)).NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	track, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypePCMU, ClockRate: 8000}, "voice", "agent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pc.AddTrack(track); err != nil {
		t.Fatal(err)
	}
	a := &agent{pc: pc, track: track, got: make(chan *rtp.Packet, 256)}
	pc.OnTrack(func(tr *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		go func() {
			for {
				p, _, err := tr.ReadRTP()
				if err != nil {
					return
				}
				select {
				case a.got <- p:
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
	a.offer = []byte(pc.LocalDescription().SDP)
	return a
}

func (a *agent) accept(t *testing.T, answer []byte) {
	t.Helper()
	if err := a.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: string(answer)}); err != nil {
		t.Fatalf("the browser cannot use the gateway's answer: %v", err)
	}
}

type line struct {
	conn *net.UDPConn
	got  chan *rtp.Packet
}

func newLine(t *testing.T) *line {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	l := &line{conn: conn, got: make(chan *rtp.Packet, 256)}
	go func() {
		buf := make([]byte, 1500)
		for {
			n, _, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			var p rtp.Packet
			if p.Unmarshal(append([]byte(nil), buf[:n]...)) == nil {
				select {
				case l.got <- &p:
				default:
				}
			}
		}
	}()
	return l
}

func (l *line) sdp() []byte {
	return []byte(fmt.Sprintf("v=0\r\no=- 3 3 IN IP4 127.0.0.1\r\ns=-\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\nm=audio %d RTP/AVP 0 101\r\na=rtpmap:0 PCMU/8000\r\na=rtpmap:101 telephone-event/8000\r\na=sendrecv\r\n",
		l.conn.LocalAddr().(*net.UDPAddr).Port))
}

func (l *line) send(to *net.UDPAddr, seq uint16) {
	raw, _ := (&rtp.Packet{Header: rtp.Header{Version: 2, PayloadType: 0, SequenceNumber: seq, Timestamp: uint32(seq) * 160, SSRC: 7}, Payload: sample}).Marshal()
	l.conn.WriteToUDP(raw, to)
}

func mediaOf(t *testing.T, sdp []byte) *net.UDPAddr {
	t.Helper()
	var host string
	var port int
	for _, l := range strings.Split(string(sdp), "\r\n") {
		if v, ok := strings.CutPrefix(l, "c=IN IP4 "); ok {
			host = v
		}
		if v, ok := strings.CutPrefix(l, "m=audio "); ok {
			fmt.Sscanf(v, "%d", &port)
		}
	}
	if host == "" || port == 0 {
		t.Fatalf("no media address in %q", sdp)
	}
	return &net.UDPAddr{IP: net.ParseIP(host), Port: port}
}

// talk checks voice both ways: the line's packets reach the browser and the browser's reach the line.
func talk(t *testing.T, a *agent, l *line, gateway *net.UDPAddr) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	heard, spoke := false, false
	for seq := uint16(1); !heard || !spoke; seq++ {
		select {
		case p := <-a.got:
			heard = heard || bytes.Equal(p.Payload, sample)
		case p := <-l.got:
			spoke = spoke || bytes.Equal(p.Payload, sample) && p.PayloadType == 0
		case <-tick.C:
			l.send(gateway, seq)
			a.track.WriteRTP(&rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: seq, Timestamp: uint32(seq) * 160}, Payload: sample})
		case <-deadline:
			t.Fatalf("voice did not pass: browser heard %v, line heard %v", heard, spoke)
		}
	}
}

func TestBrowserAnswersIncomingCall(t *testing.T) {
	_, gw, calls := startGateway(t, 5*time.Second)
	tr := newTrunk(t, gw)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	l, a := newLine(t), newAgent(t)
	recipient := sip.Uri{Scheme: "sip", User: "100", Host: gw.Host, Port: gw.Port}
	session, err := tr.dialogs.Invite(ctx, recipient, l.sdp(), sip.NewHeader("Content-Type", "application/sdp"))
	if err != nil {
		t.Fatal(err)
	}
	connected := make(chan []byte, 1)
	go func() {
		call := nextCall(t, calls)
		answer, err := call.Connect(ctx, a.offer)
		if err != nil {
			t.Errorf("the browser could not answer: %v", err)
		}
		connected <- answer
	}()
	if err := session.WaitAnswer(ctx, answerOptions(testPassword, nil)); err != nil {
		t.Fatal(err)
	}
	if err := session.Ack(ctx); err != nil {
		t.Fatal(err)
	}
	ok := string(session.InviteResponse.Body())
	if !strings.Contains(ok, " RTP/AVP 0 101\r\n") {
		t.Fatalf("the 200 OK does not carry the negotiated codecs:\n%s", ok)
	}
	a.accept(t, <-connected)
	talk(t, a, l, mediaOf(t, session.InviteResponse.Body()))
	if err := session.Bye(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestBrowserDialsOut(t *testing.T) {
	g, tr, _ := registered(t, 5*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	l, a := newLine(t), newAgent(t)
	type result struct {
		call   *Call
		answer []byte
		err    error
	}
	pending := make(chan result, 1)
	go func() {
		call, answer, err := g.DialBrowser(ctx, DialRequest{Trunk: testTrunk, Number: testNumber}, a.offer)
		pending <- result{call, answer, err}
	}()
	o := tr.offer(t)
	invite := o.req.Body()
	if !strings.Contains(string(invite), "a=rtpmap:111 opus/48000/2") || !strings.Contains(string(invite), "a=rtpmap:0 PCMU/8000") {
		t.Fatalf("the trunk was not offered the browser's codecs:\n%s", invite)
	}
	if err := o.session.RespondSDP(l.sdp()); err != nil {
		t.Fatal(err)
	}
	got := <-pending
	if got.err != nil {
		t.Fatalf("dialing for the browser failed: %v", got.err)
	}
	a.accept(t, got.answer)
	talk(t, a, l, mediaOf(t, invite))
	a.pc.Close()
	waitEnded(t, got.call, Completed)
}
