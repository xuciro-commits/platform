// Package driver contains the shared driver/v1 process transport. Drivers own
// protocol code; this package knows only typed messages and bounded JSON-RPC.
package driver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

const MaxMessageBytes = 4 << 20

type Handler interface {
	Handle(context.Context, string, json.RawMessage) (any, error)
}
type HandlerFunc func(context.Context, string, json.RawMessage) (any, error)

func (f HandlerFunc) Handle(c context.Context, m string, p json.RawMessage) (any, error) {
	return f(c, m, p)
}

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Message }

type request struct {
	JSONRPC  string          `json:"jsonrpc"`
	ID       uint64          `json:"id"`
	Method   string          `json:"method"`
	Params   json.RawMessage `json:"params"`
	Deadline time.Time       `json:"deadline,omitempty"`
}
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

func Serve(input io.Reader, output io.Writer, handler Handler) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), MaxMessageBytes)
	encoder := json.NewEncoder(output)
	for scanner.Scan() {
		var call request
		if err := json.Unmarshal(scanner.Bytes(), &call); err != nil {
			return fmt.Errorf("invalid driver request: %w", err)
		}
		answer := response{JSONRPC: "2.0", ID: call.ID}
		if call.JSONRPC != "2.0" || call.ID == 0 {
			answer.Error = &Error{Code: -32600, Message: "Invalid JSON-RPC request"}
		} else {
			ctx := context.Background()
			cancel := func() {}
			if !call.Deadline.IsZero() {
				ctx, cancel = context.WithDeadline(ctx, call.Deadline)
			}
			result, err := handler.Handle(ctx, call.Method, call.Params)
			cancel()
			if err != nil {
				var rpc *Error
				if !errors.As(err, &rpc) {
					rpc = &Error{Code: -32000, Message: err.Error()}
				}
				answer.Error = rpc
			} else {
				answer.Result, err = json.Marshal(result)
				if err != nil {
					return err
				}
			}
		}
		raw, err := json.Marshal(answer)
		if err != nil {
			return err
		}
		if len(raw) > MaxMessageBytes {
			return fmt.Errorf("driver reply exceeds message budget")
		}
		if err = encoder.Encode(answer); err != nil {
			return err
		}
	}
	return scanner.Err()
}

type Client struct {
	command   *exec.Cmd
	input     io.WriteCloser
	responses chan response
	done      chan struct{}
	stopped   chan struct{}
	mu        sync.Mutex
	sequence  atomic.Uint64
	close     sync.Once
}

func Start(executable string) (*Client, error) {
	command := exec.Command(executable)
	input, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	command.Stderr = io.Discard
	if err = command.Start(); err != nil {
		return nil, err
	}
	client := &Client{command: command, input: input, responses: make(chan response, 1), done: make(chan struct{}), stopped: make(chan struct{})}
	go func() {
		defer close(client.done)
		scanner := bufio.NewScanner(output)
		scanner.Buffer(make([]byte, 4096), MaxMessageBytes)
		for scanner.Scan() {
			var answer response
			if json.Unmarshal(scanner.Bytes(), &answer) != nil || answer.JSONRPC != "2.0" {
				break
			}
			select {
			case client.responses <- answer:
			case <-client.stopped:
				return
			}
		}
		_ = command.Process.Kill()
		_ = command.Wait()
	}()
	return client, nil
}
func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	deadline, _ := ctx.Deadline()
	call := request{JSONRPC: "2.0", ID: c.sequence.Add(1), Method: method, Params: raw, Deadline: deadline}
	encoded, err := json.Marshal(call)
	if err != nil {
		return err
	}
	if len(encoded) > MaxMessageBytes {
		return fmt.Errorf("driver request exceeds message budget")
	}
	select {
	case <-c.done:
		return fmt.Errorf("driver process is unavailable")
	default:
	}
	written := make(chan error, 1)
	go func() { _, err := c.input.Write(append(encoded, '\n')); written <- err }()
	select {
	case err = <-written:
		if err != nil {
			c.Close()
			return err
		}
	case <-ctx.Done():
		c.Close()
		return ctx.Err()
	case <-c.stopped:
		return fmt.Errorf("driver process is unavailable")
	}
	select {
	case <-ctx.Done():
		c.Close()
		return ctx.Err()
	case <-c.done:
		return fmt.Errorf("driver process stopped before answering")
	case answer := <-c.responses:
		if answer.ID != call.ID {
			c.Close()
			return fmt.Errorf("driver returned another call's identity")
		}
		if answer.Error != nil {
			return answer.Error
		}
		if result != nil {
			return json.Unmarshal(answer.Result, result)
		}
		return nil
	}
}
func (c *Client) Available() bool {
	select {
	case <-c.done:
		return false
	case <-c.stopped:
		return false
	default:
		return true
	}
}
func (c *Client) Close() {
	c.close.Do(func() { close(c.stopped); _ = c.input.Close(); _ = c.command.Process.Kill() })
}
