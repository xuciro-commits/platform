package gateway

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
)

const gatewayUser = "gateway"

var (
	ErrNotRegistered = errors.New("voice gateway: the trunk is not registered")
	ErrBusy          = errors.New("voice gateway: the trunk is on another call")
	ErrNumber        = errors.New("voice gateway: the number cannot be dialed")
	ErrUnanswered    = errors.New("voice gateway: the far end did not answer in time")
	errNotServing    = errors.New("voice gateway: not serving yet")
	errRingTimeout   = errors.New("ring timeout")
)

var emergencyNumbers = map[string]bool{"000": true, "110": true, "112": true, "119": true, "120": true, "122": true, "911": true, "999": true}

type DialRequest struct {
	Trunk   string
	Number  string
	Offer   []byte
	Ringing func()
}

type DialError struct {
	Status int
	Reason string
}

func (e *DialError) Error() string {
	return fmt.Sprintf("voice gateway: the far end answered %d %s", e.Status, e.Reason)
}

func (g *Gateway) Dial(ctx context.Context, r DialRequest) (*Call, error) {
	if len(r.Offer) == 0 {
		return nil, ErrNoSession
	}
	if !dialable(r.Number, g.cfg.DialPrefixes) {
		return nil, ErrNumber
	}
	g.mu.Lock()
	out := g.outgoing
	g.mu.Unlock()
	if out == nil {
		return nil, errNotServing
	}
	reg, ok := g.registration(r.Trunk)
	if !ok {
		return nil, ErrNotRegistered
	}
	var contact sip.Uri
	if err := sip.ParseUri(reg.Contact, &contact); err != nil {
		return nil, fmt.Errorf("voice gateway: trunk contact %q: %w", reg.Contact, err)
	}
	call := newCall(Outbound, r.Trunk, Dialing)
	call.Dialed = r.Number
	call.settle()
	if !g.claim(call) {
		return nil, ErrBusy
	}
	log := g.log.With("trunk", r.Trunk)
	target := sip.Uri{Scheme: "sip", User: r.Number, Host: contact.Host, Port: contact.Port}
	req := sip.NewRequest(sip.INVITE, target)
	from := &sip.FromHeader{Address: sip.Uri{Scheme: "sip", User: gatewayUser, Host: g.cfg.Realm}, Params: sip.NewParams()}
	from.Params.Add("tag", sip.GenerateTagN(16))
	req.AppendHeader(from)
	req.AppendHeader(&sip.ToHeader{Address: target, Params: sip.NewParams()})
	req.AppendHeader(sip.NewHeader("Content-Type", "application/sdp"))
	dialing := g.dialingFor(r.Trunk)
	if dialing.NumberHeader != "" {
		req.AppendHeader(sip.NewHeader(dialing.NumberHeader, r.Number))
	}
	for _, name := range slices.Sorted(maps.Keys(dialing.Headers)) {
		req.AppendHeader(sip.NewHeader(name, dialing.Headers[name]))
	}
	req.SetBody(r.Offer)
	if reg.Source != "" {
		req.SetDestination(reg.Source)
	}
	session, err := out.WriteInvite(ctx, req)
	if err != nil {
		call.end(Failed)
		log.Warn("outgoing call could not be placed", "error", err)
		return call, fmt.Errorf("voice gateway: invite: %w", err)
	}
	call.ID = req.CallID().Value()
	log = log.With("call", call.ID)
	call.log = log
	log.Info("outgoing call dialing", "number", redact(r.Number))
	waiting, stop := context.WithCancelCause(ctx)
	defer stop(nil)
	timer := time.AfterFunc(g.cfg.RingTimeout, func() { stop(errRingTimeout) })
	defer timer.Stop()
	var announced sync.Once
	err = session.WaitAnswer(waiting, sipgo.AnswerOptions{OnResponse: func(res *sip.Response) error {
		if res.StatusCode == sip.StatusRinging || res.StatusCode == sip.StatusSessionInProgress {
			call.ring()
			if r.Ringing != nil {
				announced.Do(r.Ringing)
			}
		}
		return nil
	}})
	if err != nil {
		session.Close()
		cause, reported := dialFailure(err, ctx, waiting)
		call.end(cause)
		log.Info("outgoing call did not connect", "cause", cause, "error", reported)
		return call, reported
	}
	if err := session.Ack(context.Background()); err != nil {
		session.Close()
		call.end(Failed)
		log.Warn("outgoing call could not be acknowledged", "error", err)
		return call, fmt.Errorf("voice gateway: ack: %w", err)
	}
	call.Remote = session.InviteResponse.Body()
	call.bye = session.Bye
	call.connect()
	log.Info("outgoing call connected")
	go func() {
		<-session.Context().Done()
		session.Close()
		call.end(Completed)
		log.Info("call ended")
	}()
	return call, nil
}

func dialFailure(err error, ctx, waiting context.Context) (Cause, error) {
	var refused *sipgo.ErrDialogResponse
	switch {
	case errors.As(err, &refused):
		return refusalCause(refused.Res.StatusCode), &DialError{Status: refused.Res.StatusCode, Reason: refused.Res.Reason}
	case errors.Is(context.Cause(waiting), errRingTimeout):
		return Unanswered, ErrUnanswered
	case ctx.Err() != nil:
		return Canceled, ctx.Err()
	}
	return Failed, err
}

func refusalCause(status int) Cause {
	switch status {
	case sip.StatusBusyHere, sip.StatusGlobalBusyEverywhere, sip.StatusGlobalDecline:
		return Rejected
	case sip.StatusTemporarilyUnavailable, sip.StatusRequestTimeout, sip.StatusRequestTerminated:
		return Unanswered
	}
	return Failed
}

func dialable(number string, prefixes []string) bool {
	if number == "" || len(number) > 32 {
		return false
	}
	for i, r := range number {
		if !(r >= '0' && r <= '9' || r == '*' || r == '#' || r == '+' && i == 0) {
			return false
		}
	}
	if emergencyNumbers[strings.TrimPrefix(number, "+")] {
		return false
	}
	return len(prefixes) == 0 || slices.ContainsFunc(prefixes, func(p string) bool { return strings.HasPrefix(number, p) })
}
