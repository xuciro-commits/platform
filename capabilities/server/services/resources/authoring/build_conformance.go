package authoring

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/protocol"
	"platformserver/platform"
	dataowner "platformserver/services/data"
)

type CheckEnvironment struct {
	Connection func(dataowner.Connection) (string, error)
	Credential func(name, consumer, address string) ([]byte, bool)
	Send       func(*http.Request, bool) (*http.Response, error)
}

type PendingCheck struct {
	connection     *dataowner.Connection
	setup          ConnectorSetup
	contract       protocol.ProtocolContract
	implementation protocol.ProtocolImplementation
}

func (b *Build) PendingChecks(c platform.Caller) ([]PendingCheck, *platform.Error) {
	requested := platform.Query{Domain: json.RawMessage(`[["requested","=",true]]`), Sort: []string{"id"}, Limit: 50}
	connections, _, refusal := platform.Find[dataowner.Connection](c, requested)
	if refusal != nil {
		return nil, refusal
	}
	out := make([]PendingCheck, 0, len(connections))
	for i := range connections {
		out = append(out, PendingCheck{connection: &connections[i]})
	}
	requested.Limit = 20
	setups, _, refusal := platform.Find[ConnectorSetup](c, requested)
	for _, setup := range setups {
		if contract, implementation, found := b.ConnectorParts(setup.Implementation); found {
			out = append(out, PendingCheck{setup: setup, contract: contract, implementation: implementation})
		}
	}
	return out, refusal
}

func (p PendingCheck) Run(tenant string, caller platform.Member, env CheckEnvironment, now time.Time) *pb.Submission {
	if p.connection != nil {
		detail, err := env.Connection(*p.connection)
		return dataowner.ConnectionCheckSubmission(tenant, ID, caller, *p.connection, detail, err, now)
	}
	var credential []byte
	allowed := false
	if p.setup.Secret != "" {
		credential, allowed = env.Credential(p.setup.Secret, "implementation/"+p.setup.Implementation, p.setup.Address)
	}
	run := runConformance(p.contract, p.implementation, p.setup.Address, credential, allowed, env.Send, p.setup.ID, now)
	run.Fingerprint = protocol.ConnectorFingerprint(p.contract, p.implementation, p.setup.Address, p.setup.Secret)
	return ConnectorTestedSubmission(tenant, caller, p.setup, run)
}

func runConformance(c protocol.ProtocolContract, i protocol.ProtocolImplementation, address string, credential []byte, allowed bool,
	send func(*http.Request, bool) (*http.Response, error), key string, now time.Time) protocol.ConformanceRun {
	run := protocol.ConformanceRun{At: now, Passed: len(c.Tests) > 0, Cases: []protocol.ConformanceCase{}}
	for _, test := range c.Tests {
		outcome := conformanceCase(i, test, address, credential, allowed, send, key)
		run.Passed = run.Passed && outcome.OK
		run.Cases = append(run.Cases, outcome)
	}
	return run
}

func conformanceCase(i protocol.ProtocolImplementation, test protocol.ContractTest, address string, credential []byte, allowed bool,
	send func(*http.Request, bool) (*http.Response, error), key string) protocol.ConformanceCase {
	out := protocol.ConformanceCase{Name: test.Name}
	at := slices.IndexFunc(i.Requests, func(r protocol.ImplementationRequest) bool { return r.Action == test.Action })
	if at < 0 || !slices.Contains(i.Origins, protocol.ConnectorOrigin(address)) {
		out.Detail = "the connector has no request for " + test.Action + " at " + address
		return out
	}
	request := i.Requests[at]
	path, err := protocol.ConnectorPath(request.Path, test.Payload)
	if err != nil {
		out.Detail = err.Error()
		return out
	}
	var body io.Reader
	if request.Method != http.MethodGet && request.Method != http.MethodDelete {
		payload := test.Payload
		if payload == nil {
			payload = map[string]any{}
		}
		raw, _ := json.Marshal(payload)
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(request.Method, address+path, body)
	if err != nil {
		out.Detail = err.Error()
		return out
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Idempotency-Key", key+":"+test.Name)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if i.Credential != "" {
		if !allowed {
			out.Detail = "the credential is missing, or its allowed uses do not include this connector at " + address
			return out
		}
		header := "Authorization"
		if i.Credential == "api-key" {
			header = "X-API-Key"
		}
		req.Header.Set(header, string(credential))
	}
	resp, err := send(req, false)
	if err != nil {
		out.Detail = "no answer: " + err.Error()
		return out
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		out.Detail = fmt.Sprintf("answered %d", resp.StatusCode)
		return out
	}
	if len(test.Expect) > 0 {
		var fields map[string]json.RawMessage
		if json.Unmarshal(answer, &fields) != nil {
			out.Detail = "the answer is not a JSON object"
			return out
		}
		var missing []string
		for _, field := range test.Expect {
			if _, found := fields[field]; !found {
				missing = append(missing, field)
			}
		}
		if len(missing) > 0 {
			out.Detail = "the answer has no " + strings.Join(missing, ", ")
			return out
		}
	}
	out.OK = true
	return out
}
