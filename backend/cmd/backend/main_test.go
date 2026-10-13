package main

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/rpc/schema"
)

// TestRelayEndToEnd drives the built binary the way main.py does: wait for "ready", call a method,
// then close stdin and expect a clean exit.
func TestRelayEndToEnd(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "backend")
	build := exec.Command("go", "build", "-ldflags", "-X main.version=1.2.3", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), "DECKY_PLUGIN_RUNTIME_DIR="+t.TempDir(), "DECKY_PLUGIN_SETTINGS_DIR="+t.TempDir())
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	lines := bufio.NewScanner(stdout)

	var ready struct {
		Method string       `json:"method"`
		Params schema.Ready `json:"params"`
	}
	readLine(t, lines, &ready)
	if ready.Method != "ready" || ready.Params.Version != "1.2.3" || ready.Params.Protocol != schema.ProtocolVersion {
		t.Fatalf("unexpected handshake: %+v", ready)
	}

	if _, err := stdin.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n")); err != nil {
		t.Fatal(err)
	}
	var pong struct {
		ID     int               `json:"id"`
		Method string            `json:"method"`
		Result schema.PingResult `json:"result"`
	}
	// Notifications may come before the answer, as main.py sees them too.
	for readLine(t, lines, &pong); pong.Method != ""; readLine(t, lines, &pong) {
		pong.Method = ""
	}
	if pong.ID != 1 || pong.Result.Version != "1.2.3" {
		t.Fatalf("unexpected ping response: %+v", pong)
	}

	_ = stdin.Close()
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	select {
	case err := <-exited:
		if err != nil {
			t.Fatalf("backend exited with %v after stdin closed", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("backend did not exit after stdin closed")
	}
}

func readLine(t *testing.T, lines *bufio.Scanner, v any) {
	t.Helper()
	if !lines.Scan() {
		t.Fatalf("no output: %v", lines.Err())
	}
	if err := json.Unmarshal(lines.Bytes(), v); err != nil {
		t.Fatalf("decode %q: %v", lines.Text(), err)
	}
}
