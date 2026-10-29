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
	concurrency := flag.Int("concurrency", 4, "Maximum simultaneous isolated instances")
	check := flag.Bool("check", false, "Check the existing worker socket")
	flag.Parse()
	if *check {
		if err := computeowner.CheckComputeSocket(*socket); err != nil {
			log.Fatal(err)
		}
		return
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := computeowner.ServeWasmWorker(ctx, *socket, *concurrency); err != nil {
		log.Fatal(err)
	}
}
