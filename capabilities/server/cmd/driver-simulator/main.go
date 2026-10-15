package main

import (
	"fmt"
	"os"

	"platformserver/driver"
	"platformserver/drivers/simulator"
)

func main() {
	if err := driver.Serve(os.Stdin, os.Stdout, simulator.New()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
