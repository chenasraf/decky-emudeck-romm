package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/rpc/schema"
)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) lines(t *testing.T) []map[string]any {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(b.buf.String()), "\n") {
		if line == "" {
			continue
		}
		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			t.Fatalf("output line is not JSON: %q", line)
		}
		out = append(out, msg)
	}
	return out
}

func serve(t *testing.T, setup func(*Server), input string) []map[string]any {
	t.Helper()
	out := &syncBuffer{}
	s := NewServer(out, slog.New(slog.NewTextHandler(io.Discard, nil)))
	setup(s)
	if err := s.Serve(context.Background(), strings.NewReader(input)); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	return out.lines(t)
}

func errorCode(msg map[string]any) float64 {
	e, _ := msg["error"].(map[string]any)
	code, _ := e["code"].(float64)
	return code
}

func TestServe(t *testing.T) {
	type echoParams struct {
		N int `json:"n"`
	}
	setup := func(s *Server) {
		Register(s, "echo", func(_ context.Context, p echoParams) (echoParams, error) { return p, nil })
		Register(s, "fail", func(context.Context, struct{}) (any, error) { return nil, errors.New("boom") })
		Register(s, "domain", func(context.Context, struct{}) (any, error) {
			return nil, &Error{Code: 1000, Message: "nope", Data: &schema.ErrorData{Kind: "not_found"}}
		})
		Register(s, "empty", func(context.Context, struct{}) (any, error) { return nil, nil })
	}

	tests := []struct {
		name  string
		input string
		check func(t *testing.T, msgs []map[string]any)
	}{
		{
			name:  "result echoes id and params",
			input: `{"jsonrpc":"2.0","id":7,"method":"echo","params":{"n":3}}`,
			check: func(t *testing.T, msgs []map[string]any) {
				if msgs[0]["id"] != float64(7) || msgs[0]["result"].(map[string]any)["n"] != float64(3) {
					t.Errorf("got %v", msgs[0])
				}
			},
		},
		{
			name:  "missing params decode as zero value",
			input: `{"jsonrpc":"2.0","id":1,"method":"echo"}`,
			check: func(t *testing.T, msgs []map[string]any) {
				if msgs[0]["result"].(map[string]any)["n"] != float64(0) {
					t.Errorf("got %v", msgs[0])
				}
			},
		},
		{
			name:  "nil result still sends a result",
			input: `{"jsonrpc":"2.0","id":1,"method":"empty"}`,
			check: func(t *testing.T, msgs []map[string]any) {
				if _, ok := msgs[0]["result"]; !ok {
					t.Errorf("got %v", msgs[0])
				}
			},
		},
		{
			name:  "parse error",
			input: `{not json`,
			check: func(t *testing.T, msgs []map[string]any) {
				if errorCode(msgs[0]) != CodeParseError || msgs[0]["id"] != nil {
					t.Errorf("got %v", msgs[0])
				}
			},
		},
		{
			name:  "invalid request",
			input: `{"jsonrpc":"1.0","id":1,"method":"echo"}`,
			check: func(t *testing.T, msgs []map[string]any) {
				if errorCode(msgs[0]) != CodeInvalidRequest {
					t.Errorf("got %v", msgs[0])
				}
			},
		},
		{
			name:  "method not found",
			input: `{"jsonrpc":"2.0","id":1,"method":"nope"}`,
			check: func(t *testing.T, msgs []map[string]any) {
				if errorCode(msgs[0]) != CodeMethodNotFound {
					t.Errorf("got %v", msgs[0])
				}
			},
		},
		{
			name:  "invalid params",
			input: `{"jsonrpc":"2.0","id":1,"method":"echo","params":{"n":"x"}}`,
			check: func(t *testing.T, msgs []map[string]any) {
				if errorCode(msgs[0]) != CodeInvalidParams {
					t.Errorf("got %v", msgs[0])
				}
			},
		},
		{
			name:  "plain error becomes internal error",
			input: `{"jsonrpc":"2.0","id":1,"method":"fail"}`,
			check: func(t *testing.T, msgs []map[string]any) {
				if errorCode(msgs[0]) != CodeInternalError {
					t.Errorf("got %v", msgs[0])
				}
			},
		},
		{
			name:  "domain error keeps code and kind",
			input: `{"jsonrpc":"2.0","id":1,"method":"domain"}`,
			check: func(t *testing.T, msgs []map[string]any) {
				data := msgs[0]["error"].(map[string]any)["data"].(map[string]any)
				if errorCode(msgs[0]) != 1000 || data["kind"] != "not_found" {
					t.Errorf("got %v", msgs[0])
				}
			},
		},
		{
			name:  "notifications get no response",
			input: `{"jsonrpc":"2.0","method":"echo"}` + "\n" + `{"jsonrpc":"2.0","method":"nope"}`,
			check: func(t *testing.T, msgs []map[string]any) {
				if len(msgs) != 0 {
					t.Errorf("got %v", msgs)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgs := serve(t, setup, tt.input+"\n")
			if len(msgs) == 0 && tt.name != "notifications get no response" {
				t.Fatal("no response")
			}
			tt.check(t, msgs)
		})
	}
}

func TestServeCancelsInFlightCallsOnEOF(t *testing.T) {
	done := make(chan struct{})
	setup := func(s *Server) {
		Register(s, "wait", func(ctx context.Context, _ struct{}) (any, error) {
			<-ctx.Done()
			close(done)
			return nil, ctx.Err()
		})
	}
	pr, pw := io.Pipe()
	s := NewServer(io.Discard, slog.New(slog.NewTextHandler(io.Discard, nil)))
	setup(s)
	served := make(chan error, 1)
	go func() { served <- s.Serve(context.Background(), pr) }()

	if _, err := pw.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"wait"}` + "\n")); err != nil {
		t.Fatal(err)
	}
	_ = pw.Close()

	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("Serve: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return after EOF")
	}
	select {
	case <-done:
	default:
		t.Fatal("in-flight call was not cancelled")
	}
}

func TestRoutesCoverSchema(t *testing.T) {
	s := NewServer(io.Discard, slog.New(slog.NewTextHandler(io.Discard, nil)))
	Routes(s, "test", nil)

	methods := reflect.TypeFor[schema.Methods]()
	for i := range methods.NumField() {
		name, _, _ := strings.Cut(methods.Field(i).Tag.Get("json"), ",")
		if _, ok := s.handlers[name]; !ok {
			t.Errorf("schema method %q has no handler", name)
		}
	}
	if len(s.handlers) != methods.NumField() {
		t.Errorf("%d handlers registered for %d schema methods", len(s.handlers), methods.NumField())
	}
}
