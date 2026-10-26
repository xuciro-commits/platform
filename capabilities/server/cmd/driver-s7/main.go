package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"math"
	"os"
	"os/signal"
	"syscall"

	"platformserver/driver"
	"platformserver/drivers/s7"
)

func main() {
	listen := flag.String("test-listen", "", "Run the local S7 protocol test service at this TCP address")
	dataFile := flag.String("test-db", "", "Optional raw DB1 bytes for the test service")
	flag.Parse()
	var err error
	if *listen == "" {
		err = driver.Serve(os.Stdin, os.Stdout, s7.New())
	} else {
		data := make([]byte, 16)
		data[0] = 1
		binary.BigEndian.PutUint16(data[2:], 123)
		binary.BigEndian.PutUint32(data[4:], 123456)
		binary.BigEndian.PutUint32(data[8:], math.Float32bits(12.5))
		if *dataFile != "" {
			data, err = os.ReadFile(*dataFile)
		}
		if err == nil {
			var service *s7.TestServer
			service, err = s7.ListenTest(*listen, map[uint16][]byte{1: data}, 480)
			if err == nil {
				fmt.Fprintf(os.Stderr, "Local S7 protocol test service at %s; not a real PLC\n", service.Address())
				stop := make(chan os.Signal, 1)
				signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
				<-stop
				signal.Stop(stop)
				_ = service.Close()
			}
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
