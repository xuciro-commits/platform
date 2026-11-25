package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"platformvoice/gateway"
	"platformvoice/records"
)

type settings struct {
	Realm        string                          `json:"realm"`
	Trunks       map[string]string               `json:"trunks"`
	Dialing      map[string]gateway.TrunkDialing `json:"dialing"`
	DialPrefixes []string                        `json:"dialPrefixes"`
	RingSeconds  int                             `json:"ringSeconds"`
	Owners       []records.Owner                 `json:"owners"`
	PreferSecs   int                             `json:"preferSeconds"`
}

type options struct {
	addr, host, config     string
	platform, tenant, cred string
}

func main() {
	var o options
	flag.StringVar(&o.addr, "addr", "0.0.0.0:5060", "UDP address to listen on")
	flag.StringVar(&o.host, "host", "", "address the trunks and browsers reach the gateway at; required when listening on all interfaces")
	flag.StringVar(&o.config, "config", "", "JSON file with the realm, the trunk accounts and the ring time")
	flag.StringVar(&o.platform, "platform", "", "the platform's address, e.g. http://127.0.0.1:18511; without it every call is answered 480")
	flag.StringVar(&o.tenant, "tenant", "", "the tenant whose phone app records the calls")
	flag.StringVar(&o.cred, "token-file", "", "file holding the access token of the gateway's member, who holds the phone app's gateway role; read again on every request")
	flag.Parse()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger, o); err != nil {
		logger.Error("voice gateway stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger, o options) error {
	if o.config == "" {
		return errors.New("-config is required")
	}
	raw, err := os.ReadFile(o.config)
	if err != nil {
		return fmt.Errorf("read %s: %w", o.config, err)
	}
	var s settings
	if err := json.Unmarshal(raw, &s); err != nil {
		return fmt.Errorf("parse %s: %w", o.config, err)
	}
	onCall := func(c *gateway.Call) {
		if err := c.Unavailable(context.Background()); err != nil && !errors.Is(err, gateway.ErrDecided) {
			logger.Warn("could not refuse the call", "call", c.ID, "error", err)
		}
	}
	var desk *gateway.Desk
	if o.platform != "" {
		if o.tenant == "" || o.cred == "" {
			return errors.New("-platform needs -tenant and -token-file")
		}
		client := records.New(o.platform, o.tenant, tokenFrom(o.cred), logger)
		client.OwnedBy(s.Owners)
		desk = gateway.NewDesk(client, logger)
		desk.PreferFor(time.Duration(s.PreferSecs) * time.Second)
		onCall = desk.Incoming
		go client.Follow(ctx)
	}
	g, err := gateway.New(gateway.Config{
		Realm: s.Realm, Host: o.host, Trunks: s.Trunks, Dialing: s.Dialing, DialPrefixes: s.DialPrefixes, RingTimeout: time.Duration(s.RingSeconds) * time.Second, Logger: logger,
	}, onCall)
	if err != nil {
		return err
	}
	conn, err := net.ListenPacket("udp", o.addr)
	if err != nil {
		g.Close()
		return err
	}
	if desk != nil {
		desk.Attach(g)
		go desk.Run(ctx)
	}
	go func() {
		<-ctx.Done()
		g.Close()
		conn.Close()
	}()
	if err := g.Serve(conn); err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}

func tokenFrom(path string) func() (string, error) {
	return func() (string, error) {
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read the gateway's token: %w", err)
		}
		token := strings.TrimSpace(string(raw))
		if token == "" {
			return "", fmt.Errorf("%s holds no token", path)
		}
		return token, nil
	}
}
