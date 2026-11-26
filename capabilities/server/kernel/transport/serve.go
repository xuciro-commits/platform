package transport

import (
	"context"
	"log"
	"net"
	"net/http"
	"os/signal"
	"syscall"
	"time"
)

type shutdownStreamKey struct{}

func streamClosing(r *http.Request) <-chan struct{} {
	closing, _ := r.Context().Value(shutdownStreamKey{}).(<-chan struct{})
	return closing
}

func Serve(ctx context.Context, addr string, handler http.Handler, atExit func(context.Context) error) error {
	closing := make(chan struct{})
	server := &http.Server{
		Addr:    addr,
		Handler: handler,
		BaseContext: func(net.Listener) context.Context {
			return context.WithValue(context.WithoutCancel(ctx), shutdownStreamKey{}, (<-chan struct{})(closing))
		},
	}
	server.RegisterOnShutdown(func() { close(closing) })
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
	shutdown, done := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer done()
	if err := server.Shutdown(shutdown); err != nil {
		return err
	}
	return atExit(shutdown)
}
