// Command mes-agent is the thin adapter an AI agent (or a script) uses to act on
// the plant (ADR-0008): it lists the caller's own action catalog and submits one
// of those actions through the kernel's submission path. It knows no action
// itself; what the caller may do comes from the server.
//
//	mes-agent actions
//	mes-agent do mes.downtime.reason CNC-11#1 '{"reason":"Setup"}'
//
// Credentials: -token, or client credentials from -oidc-token with
// MES_AGENT_CLIENT and MES_AGENT_SECRET.
package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"slices"

	"platformserver/platform"
)

func main() {
	server := flag.String("server", "http://127.0.0.1:8490", "mes-server URL")
	token := flag.String("token", "", "bearer token")
	tokenURL := flag.String("oidc-token", "", "token endpoint for client credentials")
	flag.Parse()
	if *tokenURL != "" {
		*token = clientToken(*tokenURL, os.Getenv("MES_AGENT_CLIENT"), os.Getenv("MES_AGENT_SECRET"))
	}
	call := func(method, path string, body any) []byte {
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequest(method, *server+path, bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+*token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Fatal(err)
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == http.StatusUnauthorized {
			log.Fatal("not authenticated")
		}
		return out
	}
	var actions []platform.Action
	json.Unmarshal(call("GET", "/v1/actions", nil), &actions)
	switch args := flag.Args(); {
	case len(args) == 1 && args[0] == "actions":
		out, _ := json.MarshalIndent(actions, "", "  ")
		fmt.Println(string(out))
	case len(args) == 4 && args[0] == "do":
		i := slices.IndexFunc(actions, func(a platform.Action) bool { return a.Schema == args[1] })
		if i < 0 {
			log.Fatalf("%s is not in your catalog", args[1]) // the server would refuse it too
		}
		var me struct{ TenantID, PrincipalID string }
		json.Unmarshal(call("GET", "/v1/me", nil), &me)
		key := make([]byte, 12)
		rand.Read(key)
		fmt.Println(string(call("POST", "/v1/submissions", map[string]any{
			"tenantId": me.TenantID, "principalId": me.PrincipalID, "authority": "plant-server",
			"target": map[string]string{"type": actions[i].Target, "id": args[2]},
			"schema": map[string]any{"name": args[1], "version": 1}, "idempotencyKey": base64.RawURLEncoding.EncodeToString(key),
			"payload": base64.StdEncoding.EncodeToString([]byte(args[3]))})))
	default:
		log.Fatal("usage: mes-agent actions | mes-agent do <schema> <target-id> <payload-json>")
	}
}

func clientToken(endpoint, client, secret string) string {
	resp, err := http.PostForm(endpoint, url.Values{"grant_type": {"client_credentials"}, "client_id": {client}, "client_secret": {secret}})
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
