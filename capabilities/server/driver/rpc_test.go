package driver

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCloseWaitJoinsActualChildAndBlockedTransport(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX controlled child")
	}
	path := filepath.Join(t.TempDir(), "blocked-driver")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexec sleep 60\n"), 0700); err != nil {
		t.Fatal(err)
	}
	client, err := Start(path)
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { finished <- client.Call(context.Background(), "read", make([]byte, 1<<20), nil) }()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err = client.CloseWait(ctx); err != nil {
		t.Fatal(err)
	}
	if client.command.ProcessState == nil || client.Available() {
		t.Fatal("close returned before child was reaped")
	}
	select {
	case err = <-finished:
		if err == nil {
			t.Fatal("blocked call unexpectedly succeeded")
		}
	case <-ctx.Done():
		t.Fatal("transport call did not exit")
	}
	if err = client.CloseWait(ctx); err != nil {
		t.Fatal("repeated close", err)
	}
}

func TestTransportChild(t *testing.T) {
	if os.Getenv("PLATFORM_RPC_TEST_CHILD") != "1" {
		return
	}
	handler := HandlerFunc(func(ctx context.Context, method string, raw json.RawMessage) (any, error) {
		if method == "delayed" {
			time.Sleep(120 * time.Millisecond)
		}
		return method, nil
	})
	if err := Serve(os.Stdin, os.Stdout, handler); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

func TestCancelledCallKeepsSharedProcessAndIsolatesLateAnswer(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// Start the same real test child through the production transport. The small
	// launcher carries no fixture JSON through a shell command string.
	if runtime.GOOS == "windows" {
		t.Skip("POSIX controlled child")
	}
	launcher := filepath.Join(t.TempDir(), "driver")
	script := "#!/bin/sh\nexport PLATFORM_RPC_TEST_CHILD=1\nexec '" + strings.ReplaceAll(executable, "'", "'\\''") + "' -test.run=^TestTransportChild$\n"
	if err := os.WriteFile(launcher, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	client, err := Start(launcher)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := client.Call(ctx, "delayed", struct{}{}, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	var result string
	next, done := context.WithTimeout(context.Background(), time.Second)
	defer done()
	if err := client.Call(next, "peer", struct{}{}, &result); err != nil || result != "peer" || !client.Available() {
		t.Fatal("one scope's timeout damaged its peer", result, err)
	}
}
