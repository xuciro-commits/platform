// Command gateway-sim plays the plant's edge: a line gateway pushing equipment
// state batches, with a stop now and then. With -oidc-token it authenticates as
// the OIDC client mes-gateway (secret in MES_GATEWAY_SECRET).
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	"mes"
)

var tokenURL = flag.String("oidc-token", "", "token endpoint for client credentials (empty: demo tokens)")

// credential is the demo token, or a fresh client-credentials access token.
func credential(demo, client, secretEnv string) string {
	if *tokenURL == "" {
		return demo
	}
	resp, err := http.PostForm(*tokenURL, url.Values{"grant_type": {"client_credentials"},
		"client_id": {client}, "client_secret": {os.Getenv(secretEnv)}})
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	var t struct {
		AccessToken string `json:"access_token"`
	}
	if json.NewDecoder(resp.Body).Decode(&t) != nil || t.AccessToken == "" {
		log.Fatalf("no token for %s (%s)", client, resp.Status)
	}
	return t.AccessToken
}

func post(server, token, path string, body any) int {
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, server+path, bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Print(err)
		return 0
	}
	resp.Body.Close()
	return resp.StatusCode
}

func main() {
	server := flag.String("server", "http://127.0.0.1:8490", "plant host URL")
	every := flag.Duration("every", 5*time.Second, "time between gateway batches")
	batches := flag.Int("batches", 0, "stop after this many batches (0: run forever)")
	flag.Parse()

	gateway := credential("gateway-l1", "mes-gateway", "MES_GATEWAY_SECRET")
	resources := []string{"FURNACE-1", "CNC-11", "CNC-12", "CMM-1"}
	for n := 1; *batches == 0 || n <= *batches; n++ {
		start := time.Now().Add(-*every)
		for r, resource := range resources {
			var samples []mes.Sample
			for s := 0; s < 10; s++ {
				state := "run"
				if (n+r)%4 == 0 && s >= 3 && s < 7 {
					state = "down"
				} else if s == 9 && r == 3 {
					state = "idle"
				}
				samples = append(samples, mes.Sample{At: start.Add(time.Duration(s) * *every / 10), State: state})
			}
			fmt.Println("batch", n, resource, post(*server, gateway, "/v1/connectors/states",
				mes.StateBatch{BatchID: fmt.Sprintf("%s-%d", resource, n), Resource: resource, Samples: samples}))
		}
		if *batches == 0 || n < *batches {
			time.Sleep(*every)
			gateway = credential("gateway-l1", "mes-gateway", "MES_GATEWAY_SECRET") // tokens expire
		}
	}
}
