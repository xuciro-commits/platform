package transport

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"
)

func Serve(ctx context.Context, addr string, handler http.Handler, atExit func(context.Context) error) error {
	server := &http.Server{Addr: addr, Handler: handler}
	stop, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	failed := make(chan error, 1)
	go func() { failed <- server.ListenAndServe() }()
	log.Printf("host on http://%s", addr)
	select {
	case err := <-failed:
		return err
	case <-stop.Done():
	}
	shutdown, done := context.WithTimeout(ctx, 10*time.Second)
	defer done()
	if err := server.Shutdown(shutdown); err != nil {
		return err
	}
	return atExit(shutdown)
}
