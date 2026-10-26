// Command webhook-sink runs the local sink stand-alone for manual debugging.
// Deployments embed the same receiver in platform-server through
// PLATFORM_SINK_ADDRESS (internal/sink); this binary stays for ad-hoc runs.
package main

import (
	"flag"
	"log"
	"os"

	"platformserver/internal/sink"
)

func main() {
	addr := flag.String("addr", "0.0.0.0:8080", "listen address")
	smtpAddr := flag.String("smtp", "0.0.0.0:2525", "SMTP listen address; empty disables mail")
	flag.Parse()
	s := sink.New([]byte(os.Getenv("WEBHOOK_SECRET")))
	if *smtpAddr != "" {
		go func() {
			if err := s.KeepSMTP(*smtpAddr); err != nil {
				log.Fatal(err)
			}
		}()
	}
	log.Fatal(s.ListenAndServe(*addr))
}
