package gateway

import (
	"context"
	"net"
	"strings"
	"time"

	"platformvoice/media"
)

// Connect answers an incoming call for a browser: the phone's 200 OK carries the
// codec both sides share, and the returned answer goes back to the browser.
func (c *Call) Connect(ctx context.Context, browserOffer []byte) ([]byte, error) {
	if c.Direction != Inbound {
		return nil, ErrDecided
	}
	br, err := media.New(c.media, browserOffer)
	if err != nil {
		return nil, err
	}
	phone, browser, err := br.Answer(ctx, c.Remote)
	if err == nil {
		err = c.Answer(ctx, phone)
	}
	if err != nil {
		br.Close()
		return nil, err
	}
	c.logSessions(c.Remote, phone)
	c.join(br)
	return browser, nil
}

// logSessions records where each side of the phone leg sends its audio and with which payload types.
func (c *Call) logSessions(remote, local []byte) {
	if c.log != nil {
		c.log.Info("phone media", "phone", sessionLines(remote), "gateway", sessionLines(local))
	}
}

func sessionLines(sdp []byte) string {
	var keep []string
	for _, line := range strings.Split(strings.ReplaceAll(string(sdp), "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, "c=") || strings.HasPrefix(line, "m=") || strings.HasPrefix(line, "a=rtpmap") ||
			line == "a=sendrecv" || line == "a=sendonly" || line == "a=recvonly" || line == "a=inactive" {
			keep = append(keep, line)
		}
	}
	return strings.Join(keep, " | ")
}

// DialBrowser dials out for a browser: the phone is offered the browser's codecs,
// and the returned answer goes back to the browser once the far end picks up.
func (g *Gateway) DialBrowser(ctx context.Context, r DialRequest, browserOffer []byte) (*Call, []byte, error) {
	g.mu.Lock()
	address := g.media
	g.mu.Unlock()
	br, err := media.New(address, browserOffer)
	if err != nil {
		return nil, nil, err
	}
	r.Offer = br.Offer()
	call, err := g.Dial(ctx, r)
	if err != nil {
		br.Close()
		return call, nil, err
	}
	browser, err := br.Accept(ctx, call.Remote)
	if err != nil {
		br.Close()
		call.Hangup(context.WithoutCancel(ctx))
		return call, nil, err
	}
	call.logSessions(call.Remote, r.Offer)
	call.join(br)
	return call, browser, nil
}

// join ties the call and its media: the media stops when the call ends, and the
// call hangs up when the browser's connection is lost.
func (c *Call) join(br *media.Bridge) {
	go func() {
		tick := time.NewTicker(5 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-c.done:
				br.Close()
				c.logMedia("call media closed", br.Stats())
				return
			case <-br.Done():
				c.logMedia("browser media closed", br.Stats())
				c.Hangup(context.Background())
				return
			case <-tick.C:
				c.logMedia("call media", br.Stats())
			}
		}
	}()
}

func (c *Call) logMedia(msg string, s media.Stats) {
	if c.log == nil {
		return
	}
	c.log.Info(msg, "phone", s.Phone, "browser", s.Browser, "fromPhone", s.FromPhone, "strangers", s.Strangers, "unknownPhone", s.UnknownPhone,
		"fromBrowser", s.FromBrowser, "unknownBrowser", s.UnknownBrowser, "toPhone", s.ToPhone,
		"phoneBytes", s.PhoneBytes, "browserBytes", s.BrowserBytes, "browserLoud", s.BrowserLoud, "toBrowser", s.ToBrowser)
}

func mediaAddress(host string) net.IP {
	ip := net.ParseIP(host)
	if ip == nil {
		if addrs, err := net.LookupIP(host); err == nil && len(addrs) > 0 {
			ip = addrs[0]
		}
	}
	return ip
}
