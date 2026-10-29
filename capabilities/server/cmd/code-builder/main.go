package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	computeowner "platformserver/engine/compute"
	"syscall"
)

func main() {
	socket := flag.String("socket", "", "Owner-only Unix socket")
	check := flag.Bool("check", false, "Check the existing build driver socket")
	flag.Parse()
	if *check {
		if err := computeowner.CheckComputeSocket(*socket); err != nil {
			log.Fatal(err)
		}
		return
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	compiler := computeowner.ContainerCompiler{GoImage: os.Getenv("PLATFORM_GO_WASM_IMAGE"), TinyGoImage: os.Getenv("PLATFORM_TINYGO_WASM_IMAGE")}
	if err := computeowner.ServeCodeBuilder(ctx, *socket, compiler); err != nil {
		log.Fatal(err)
	}
}
