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
	"syscall"
	"time"

	"platformvoice/gateway"
)

type settings struct {
	Realm       string            `json:"realm"`
	Trunks      map[string]string `json:"trunks"`
	RingSeconds int               `json:"ringSeconds"`
}

func main() {
	addr := flag.String("addr", "0.0.0.0:5060", "UDP address to listen on")
	host := flag.String("host", "", "address the trunks reach the gateway at; required when listening on all interfaces")
	config := flag.String("config", "", "JSON file with the realm, the trunk accounts and the ring time")
	flag.Parse()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger, *addr, *host, *config); err != nil {
		logger.Error("voice gateway stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger, addr, host, path string) error {
	if path == "" {
		return errors.New("-config is required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	var s settings
	if err := json.Unmarshal(raw, &s); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	g, err := gateway.New(gateway.Config{
		Realm: s.Realm, Host: host, Trunks: s.Trunks, RingTimeout: time.Duration(s.RingSeconds) * time.Second, Logger: logger,
	}, func(c *gateway.Call) {
		if err := c.Unavailable(context.Background()); err != nil && !errors.Is(err, gateway.ErrDecided) {
			logger.Warn("could not refuse the call", "call", c.ID, "error", err)
		}
	})
	if err != nil {
		return err
	}
	conn, err := net.ListenPacket("udp", addr)
	if err != nil {
		g.Close()
		return err
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
