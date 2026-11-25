package media

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/pion/sdp/v3"
)

const eventName = "telephone-event"

type codec struct {
	pt       uint8
	name     string
	rate     uint32
	channels uint16
	fmtp     string
}

func (c codec) mime() string { return "audio/" + c.name }

func (c codec) encoding() string {
	name := c.name
	if name == "pcmu" {
		name = "PCMU"
	}
	out := name + "/" + strconv.FormatUint(uint64(c.rate), 10)
	if c.channels > 1 {
		out += "/" + strconv.FormatUint(uint64(c.channels), 10)
	}
	return out
}

type description struct {
	addr   *net.UDPAddr
	codecs []codec
}

func (d description) find(name string, rate uint32) (codec, bool) {
	for _, c := range d.codecs {
		if c.name == name && c.rate == rate {
			return c, true
		}
	}
	return codec{}, false
}

func (d description) voice() (codec, bool) {
	for _, c := range d.codecs {
		if voiceCodec(c.name, c.rate) {
			return c, true
		}
	}
	return codec{}, false
}

func voiceCodec(name string, rate uint32) bool {
	return name == "opus" && rate == 48000 || name == "pcmu" && rate == 8000
}

func parse(raw []byte) (description, error) {
	var s sdp.SessionDescription
	if err := s.Unmarshal(raw); err != nil {
		return description{}, fmt.Errorf("%w: %v", ErrSession, err)
	}
	for _, m := range s.MediaDescriptions {
		if m.MediaName.Media != "audio" || m.MediaName.Port.Value == 0 {
			continue
		}
		var d description
		conn := m.ConnectionInformation
		if conn == nil {
			conn = s.ConnectionInformation
		}
		if conn != nil && conn.Address != nil {
			if ip := net.ParseIP(conn.Address.Address); ip != nil && !ip.IsUnspecified() {
				d.addr = &net.UDPAddr{IP: ip, Port: m.MediaName.Port.Value}
			}
		}
		rtpmap, fmtp := map[string]string{}, map[string]string{}
		for _, a := range m.Attributes {
			pt, rest, _ := strings.Cut(a.Value, " ")
			switch a.Key {
			case "rtpmap":
				rtpmap[pt] = strings.TrimSpace(rest)
			case "fmtp":
				fmtp[pt] = strings.TrimSpace(rest)
			}
		}
		for _, f := range m.MediaName.Formats {
			n, err := strconv.ParseUint(f, 10, 7)
			if err != nil {
				continue
			}
			c := codec{pt: uint8(n), fmtp: fmtp[f]}
			if mapping, ok := rtpmap[f]; ok {
				parts := strings.Split(mapping, "/")
				c.name = strings.ToLower(parts[0])
				if len(parts) > 1 {
					rate, _ := strconv.ParseUint(parts[1], 10, 32)
					c.rate = uint32(rate)
				}
				if len(parts) > 2 {
					channels, _ := strconv.ParseUint(parts[2], 10, 16)
					c.channels = uint16(channels)
				}
			} else if n == 0 {
				c.name, c.rate = "pcmu", 8000
			} else {
				continue
			}
			d.codecs = append(d.codecs, c)
		}
		return d, nil
	}
	return description{}, fmt.Errorf("%w: no audio stream", ErrSession)
}

func describe(addr *net.UDPAddr, codecs []codec) []byte {
	var id [4]byte
	rand.Read(id[:])
	session := binary.BigEndian.Uint32(id[:]) >> 1
	family := "IP4"
	if addr.IP.To4() == nil {
		family = "IP6"
	}
	formats := make([]string, len(codecs))
	for i, c := range codecs {
		formats[i] = strconv.Itoa(int(c.pt))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "v=0\r\no=- %d %d IN %s %s\r\ns=platform-voice\r\nc=IN %s %s\r\nt=0 0\r\n", session, session, family, addr.IP, family, addr.IP)
	fmt.Fprintf(&b, "m=audio %d RTP/AVP %s\r\n", addr.Port, strings.Join(formats, " "))
	for _, c := range codecs {
		fmt.Fprintf(&b, "a=rtpmap:%d %s\r\n", c.pt, c.encoding())
		if c.fmtp != "" {
			fmt.Fprintf(&b, "a=fmtp:%d %s\r\n", c.pt, c.fmtp)
		}
	}
	b.WriteString("a=ptime:20\r\na=sendrecv\r\n")
	return []byte(b.String())
}
