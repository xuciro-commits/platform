package gateway

import (
	"context"
	"net"

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
	c.join(br)
	return browser, nil
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
	call.join(br)
	return call, browser, nil
}

// join ties the call and its media: the media stops when the call ends, and the
// call hangs up when the browser's connection is lost.
func (c *Call) join(br *media.Bridge) {
	go func() {
		select {
		case <-c.done:
			br.Close()
		case <-br.Done():
			c.Hangup(context.Background())
		}
	}()
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
