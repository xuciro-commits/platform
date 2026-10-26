package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"platformserver/driver"
	"platformserver/drivers/modbus"
)

func main() {
	listen := flag.String("test-listen", "", "Run a local read-only Modbus TCP protocol service")
	dataFile := flag.String("test-data", "", "JSON unit IDs and raw memory values for the local protocol service")
	flag.Parse()
	var err error
	if *listen == "" {
		err = driver.Serve(os.Stdin, os.Stdout, modbus.New())
	} else {
		var raw []byte
		var units map[byte]modbus.Memory
		raw, err = os.ReadFile(*dataFile)
		if err == nil {
			err = json.Unmarshal(raw, &units)
		}
		if err == nil {
			var service *modbus.TestServer
			service, err = modbus.ListenTest(*listen, units)
			if err == nil {
				fmt.Fprintf(os.Stderr, "Local Modbus protocol test service at %s; simulated values, not real equipment\n", service.Address())
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
