package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	server "platformserver"
	"platformserver/gateway"
	"platformserver/platform"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8510", "gateway HTTP listen address")
	packages := flag.String("packages", "", "trusted package directory")
	state := flag.String("state", "", "persistent technical receipt directory")
	legacyTenant := flag.String("legacy-tenant", "", "explicit tenant owner of retained unscoped bindings; required for platform cleanup of legacy connections")
	tokenEnv := flag.String("token-env", "PLATFORM_GATEWAY_TOKEN", "environment variable containing the bearer token")
	flag.Parse()
	gw, err := gateway.New(gateway.Config{Directory: *packages, StateDir: *state, LegacyTenant: *legacyTenant, Token: os.Getenv(*tokenEnv), Samples: os.Stdout, Resolve: func(binding platform.DriverBinding) (platform.DriverRuntime, error) {
		index, err := server.LoadPackageIndex(*packages)
		if err != nil {
			return platform.DriverRuntime{}, err
		}
		return index.ResolveDriver(binding.Package, binding.Version, binding.Digest, binding.Profile)
	}})
	if err != nil {
		log.Fatal(err)
	}
	defer gw.Close()
	httpServer := &http.Server{Addr: *listen, Handler: gw, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: time.Minute, MaxHeaderBytes: 16 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdown)
	}()
	log.Printf("protocol gateway listening on %s", *listen)
	if err = httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
