package actions

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"platformserver/platform"
	"strings"
	"time"
)

const A2AVersion = "1.0"

type A2APart struct {
	Text      string          `json:"text,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
	MediaType string          `json:"mediaType,omitempty"`
}

type A2AMessage struct {
	MessageID string    `json:"messageId"`
	ContextID string    `json:"contextId,omitempty"`
	TaskID    string    `json:"taskId,omitempty"`
	Role      string    `json:"role"`
	Parts     []A2APart `json:"parts"`
}

type A2ATask struct {
	ID        string `json:"id"`
	ContextID string `json:"contextId"`
	Status    struct {
		State     string      `json:"state"`
		Message   *A2AMessage `json:"message,omitempty"`
		Timestamp time.Time   `json:"timestamp,omitzero"`
	} `json:"status"`
	Artifacts []struct {
		ArtifactID string    `json:"artifactId"`
		Name       string    `json:"name"`
		Parts      []A2APart `json:"parts"`
	} `json:"artifacts,omitempty"`
}

func (m A2AMessage) Text() string {
	var out []string
	for _, p := range m.Parts {
		if p.Text != "" {
			out = append(out, p.Text)
		} else if len(p.Data) > 0 {
			out = append(out, string(p.Data))
		}
	}
	return strings.Join(out, "\n")
}

func TaskOf(run platform.AgentRunRecord, question string) A2ATask {
	var x A2ATask
	x.ID, x.ContextID, x.Status.Timestamp = run.ID, run.ID, run.Changed.At
	say := func(text string) *A2AMessage {
		return &A2AMessage{MessageID: run.ID + ":" + fmt.Sprint(len(run.Steps)), TaskID: run.ID, ContextID: run.ID, Role: "ROLE_AGENT", Parts: []A2APart{{Text: text}}}
	}
	switch run.State {
	case "running":
		x.Status.State = "TASK_STATE_WORKING"
		if len(run.Steps) == 0 {
			x.Status.State = "TASK_STATE_SUBMITTED"
		}
	case "waiting":
		x.Status.State, x.Status.Message = "TASK_STATE_INPUT_REQUIRED", say(question)
	case "done":
		x.Status.State = "TASK_STATE_COMPLETED"
		part := A2APart{Text: run.Result}
		if json.Valid([]byte(run.Result)) && strings.HasPrefix(strings.TrimSpace(run.Result), "{") {
			part = A2APart{Data: json.RawMessage(run.Result), MediaType: "application/json"}
		}
		x.Artifacts = append(x.Artifacts, struct {
			ArtifactID string    `json:"artifactId"`
			Name       string    `json:"name"`
			Parts      []A2APart `json:"parts"`
		}{ArtifactID: run.ID + ":result", Name: "result", Parts: []A2APart{part}})
	default:
		x.Status.State, x.Status.Message = "TASK_STATE_FAILED", say(run.Stopped)
		if strings.HasPrefix(run.Stopped, "stopped by ") {
			x.Status.State = "TASK_STATE_CANCELED"
		}
	}
	return x
}

func (d Transport) A2A(ep Endpoint, x platform.Effect, authorizationHeader bool) platform.Outcome {
	sum := sha256.Sum256([]byte(x.Body))
	out := platform.Outcome{Effect: x.ID, Digest: hex.EncodeToString(sum[:])}
	var body struct {
		Data json.RawMessage `json:"data"`
	}
	json.Unmarshal([]byte(x.Body), &body)
	text := string(body.Data)
	var data struct{ Message string }
	if json.Unmarshal(body.Data, &data) == nil && data.Message != "" {
		text = data.Message
	}
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": x.ID, "method": "SendMessage", "params": map[string]any{
		"message": map[string]any{"messageId": x.ID, "role": "ROLE_USER",
			"parts": []map[string]any{{"text": text}, {"data": body.Data, "mediaType": "application/json"}}}}})
	req, _ := http.NewRequest(http.MethodPost, ep.URL, strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("A2A-Version", A2AVersion)
	if ep.Secret != "" {
		consumer := "endpoint/" + ep.ID
		if authorizationHeader {
			consumer = ep.ID
		}
		secret, ok := d.Credentials.CredentialValue(ep.Secret, consumer, ep.URL)
		if authorizationHeader {
			secret, ok = d.Credentials.CredentialHeader(ep.Secret, consumer, ep.URL)
		}
		if !ok {
			out.Result, out.Detail = "retry", "secret "+ep.Secret+" missing"
			return out
		}
		value := string(secret)
		if !authorizationHeader {
			value = "Bearer " + value
		}
		req.Header.Set("Authorization", strings.TrimSpace(value))
	}
	send := d.Send
	resp, err := send(req, ep.AllowPrivate)
	if err != nil {
		out.Result, out.Detail = "retry", "no answer (resent as the same message): "+err.Error()
		return out
	}
	answer, readErr := io.ReadAll(io.LimitReader(resp.Body, (64<<10)+1))
	resp.Body.Close()
	if readErr != nil || len(answer) > 64<<10 {
		out.Result, out.Detail = "unknown", "The A2A answer is incomplete or exceeds its retained evidence budget"
		return out
	}
	var rpc struct {
		Result struct {
			Task    *A2ATask        `json:"task"`
			Message json.RawMessage `json:"message"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	switch {
	case resp.StatusCode >= 500 || resp.StatusCode == 408 || resp.StatusCode == 429:
		out.Result, out.Detail = "retry", resp.Status
	case resp.StatusCode/100 != 2 || json.Unmarshal(answer, &rpc) != nil:
		out.Result, out.Detail = "rejected", resp.Status+": not an A2A answer"
	case rpc.Error != nil:
		out.Result, out.Detail = "rejected", rpc.Error.Message
	case rpc.Result.Message != nil:
		out.Result, out.Answer = "delivered", rpc.Result.Message
	case rpc.Result.Task == nil:
		out.Result, out.Detail = "rejected", "neither a task nor a message"
	default:
		task := rpc.Result.Task
		task.Status.Timestamp = time.Time{}
		switch task.Status.State {
		case "TASK_STATE_COMPLETED":
			out.Result = "delivered"
			out.Answer, _ = json.Marshal(task)
		case "TASK_STATE_SUBMITTED", "TASK_STATE_WORKING":
			out.Result, out.Detail = "retry", "the agent is still working"
		default:
			out.Result, out.Detail = "rejected", strings.TrimPrefix(task.Status.State, "TASK_STATE_")
			if task.Status.Message != nil {
				out.Detail += ": " + task.Status.Message.Text()
			}
		}
	}
	return out
}

func AnswerOf(o platform.Outcome) string {
	var task A2ATask
	var parts []string
	if json.Unmarshal(o.Answer, &task) == nil && len(task.Artifacts) > 0 {
		for _, a := range task.Artifacts {
			for _, p := range a.Parts {
				parts = append(parts, cmp.Or(p.Text, string(p.Data)))
			}
		}
		return strings.Join(parts, "\n")
	}
	var m A2AMessage
	if json.Unmarshal(o.Answer, &m) == nil && len(m.Parts) > 0 {
		return m.Text()
	}
	return string(o.Answer)
}
