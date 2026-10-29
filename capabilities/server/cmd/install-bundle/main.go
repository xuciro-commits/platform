package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

type Bundle struct {
	Tenant      string            `json:"tenant"`
	Application json.RawMessage   `json:"application"`
	Objects     []json.RawMessage `json:"objects"`
	Pages       []json.RawMessage `json:"pages"`
}

type schemaVersion struct {
	Version string `json:"version"`
	Family  int    `json:"family"`
}

type action struct {
	Schema        string        `json:"schema"`
	SchemaVersion schemaVersion `json:"schemaVersion"`
}

type installer struct {
	host, tenant, principal, authority, token string
	client                                    *http.Client
	versions                                  map[string]schemaVersion
	steps                                     int
}

func (in *installer) do(method, path string, body any) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, strings.TrimRight(in.host, "/")+path, reader)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+in.token)
	request.Header.Set("Platform-Tenant", in.tenant)
	request.Header.Set("Content-Type", "application/json")
	response, err := in.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	answer, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= 300 {
		return nil, fmt.Errorf("%s %s: %s", method, path, strings.TrimSpace(string(answer)))
	}
	return answer, nil
}

func (in *installer) decide(schema, target, id string, payload any) error {
	if _, ok := in.versions[schema]; !ok {
		return fmt.Errorf("%s is not an action here", schema)
	}
	values, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	in.steps++
	body := map[string]any{
		"schema":         map[string]any{"name": schema, "version": 1},
		"tenantId":       in.tenant,
		"principalId":    in.principal,
		"authority":      in.authority,
		"target":         map[string]any{"type": target, "id": id},
		"payload":        base64.StdEncoding.EncodeToString(values),
		"idempotencyKey": fmt.Sprintf("bundle-%s-%d", in.tenant, in.steps),
	}
	answer, err := in.do("POST", "/v1/submissions", body)
	if err != nil {
		return err
	}
	var accepted struct {
		Record struct {
			ChangeID string `json:"changeId"`
		} `json:"record"`
	}
	if json.Unmarshal(answer, &accepted) != nil || accepted.Record.ChangeID == "" {
		return fmt.Errorf("%s was not accepted: %s", schema, strings.TrimSpace(string(answer)))
	}
	fmt.Printf("  %-28s %-14s %s\n", schema, id, accepted.Record.ChangeID)
	return nil
}

func named(raw json.RawMessage) (name, id string, payload map[string]any, err error) {
	if err = json.Unmarshal(raw, &payload); err != nil {
		return "", "", nil, err
	}
	name, _ = payload["name"].(string)
	id, _ = payload["id"].(string)
	delete(payload, "id")
	if name == "" || id == "" {
		err = fmt.Errorf("a declaration needs a name and an id")
	}
	return name, id, payload, err
}

func main() {
	host := flag.String("host", "http://127.0.0.1:8495", "the platform's address")
	tenant := flag.String("tenant", "", "the tenant the bundle is installed into")
	principal := flag.String("principal", "builder", "the member who decides")
	authority := flag.String("authority", "build", "the authority the decisions name")
	tokenFile := flag.String("token-file", "", "a file holding the member's token")
	flag.Parse()
	if *tenant == "" || *tokenFile == "" || flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: install-bundle -tenant <tenant> -token-file <file> <application.json>")
		os.Exit(2)
	}
	token, err := os.ReadFile(*tokenFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	raw, err := os.ReadFile(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var bundle Bundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	in := &installer{host: *host, tenant: *tenant, principal: *principal, authority: *authority,
		token: strings.TrimSpace(string(token)), client: &http.Client{}, versions: map[string]schemaVersion{}}
	catalog, err := in.do("GET", "/v1/actions", nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var actions []action
	if err := json.Unmarshal(catalog, &actions); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, a := range actions {
		in.versions[a.Schema] = a.SchemaVersion
	}
	for _, object := range bundle.Objects {
		name, id, payload, err := named(object)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("object %s\n", name)
		if err := in.decide("build.object.create", "build.object", id, payload); err != nil {
			fmt.Fprintln(os.Stderr, "  ", err)
			os.Exit(1)
		}
		if err := in.decide("build.object.publish", "build.object", id, map[string]any{}); err != nil {
			fmt.Fprintln(os.Stderr, "  ", err)
			os.Exit(1)
		}
	}
	for _, page := range bundle.Pages {
		name, id, payload, err := named(page)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("page %s\n", name)
		if err := in.decide("build.page.create", "build.page", id, payload); err != nil {
			fmt.Fprintln(os.Stderr, "  ", err)
			os.Exit(1)
		}
		if err := in.decide("build.page.publish", "build.page", id, map[string]any{}); err != nil {
			fmt.Fprintln(os.Stderr, "  ", err)
			os.Exit(1)
		}
	}
	var application map[string]any
	if err := json.Unmarshal(bundle.Application, &application); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	name, _ := application["name"].(string)
	fmt.Printf("application %s\n", name)
	if err := in.decide("build.app.create", "build.app", "app-"+name, application); err != nil {
		fmt.Fprintln(os.Stderr, "  ", err)
		os.Exit(1)
	}
	if err := in.decide("build.app.publish", "build.app", "app-"+name, map[string]any{}); err != nil {
		fmt.Fprintln(os.Stderr, "  ", err)
		os.Exit(1)
	}
	fmt.Printf("installed %s in %s\n", name, *tenant)
}
