// Package rpc serves the backend over JSON-RPC 2.0 on a line-delimited stream (stdin/stdout when
// spawned by main.py): one message per line, in both directions.
package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/rpc/schema"
)

// JSON-RPC 2.0 protocol error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

// maxMessageSize bounds a single incoming line.
const maxMessageSize = 16 << 20

// Error is returned by handlers to control the JSON-RPC error sent back.
type Error schema.Error

func (e *Error) Error() string { return e.Message }

// Handler runs one method call. A nil params means the call carried none.
type Handler func(ctx context.Context, params json.RawMessage) (any, error)

type Server struct {
	log      *slog.Logger
	handlers map[string]Handler

	mu  sync.Mutex
	out io.Writer
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

type notification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

func NewServer(out io.Writer, log *slog.Logger) *Server {
	return &Server{log: log, handlers: map[string]Handler{}, out: out}
}

// Handle registers h for method, replacing any earlier handler.
func (s *Server) Handle(method string, h Handler) {
	s.handlers[method] = h
}

// Register adds a handler whose params are decoded into P.
func Register[P, R any](s *Server, method string, fn func(ctx context.Context, params P) (R, error)) {
	s.Handle(method, func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params P
		if len(raw) > 0 && string(raw) != "null" {
			if err := json.Unmarshal(raw, &params); err != nil {
				return nil, &Error{Code: CodeInvalidParams, Message: err.Error()}
			}
		}
		return fn(ctx, params)
	})
}

// Notify sends a notification (a message without an id), such as an event for the frontend.
func (s *Server) Notify(method string, params any) error {
	return s.write(notification{JSONRPC: "2.0", Method: method, Params: params})
}

// Serve reads requests from in until it reaches EOF or ctx ends, then cancels in-flight calls and
// waits for them. EOF is the normal shutdown: main.py closes stdin to stop the backend.
func (s *Server) Serve(ctx context.Context, in io.Reader) error {
	ctx, cancel := context.WithCancel(ctx)

	lines := make(chan []byte)
	readErr := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(in)
		scanner.Buffer(make([]byte, 64<<10), maxMessageSize)
		for scanner.Scan() {
			line := append([]byte(nil), scanner.Bytes()...)
			select {
			case lines <- line:
			case <-ctx.Done():
				return
			}
		}
		readErr <- scanner.Err()
	}()

	var wg sync.WaitGroup
	defer wg.Wait()
	defer cancel() // runs before wg.Wait, so in-flight handlers see the cancellation

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-readErr:
			return err
		case line := <-lines:
			if len(line) == 0 {
				continue
			}
			wg.Go(func() { s.dispatch(ctx, line) })
		}
	}
}

func (s *Server) dispatch(ctx context.Context, line []byte) {
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		s.reply(json.RawMessage("null"), nil, &Error{Code: CodeParseError, Message: err.Error()})
		return
	}
	isNotification := len(req.ID) == 0
	if req.JSONRPC != "2.0" || req.Method == "" {
		if !isNotification {
			s.reply(req.ID, nil, &Error{Code: CodeInvalidRequest, Message: "invalid request"})
		}
		return
	}

	handler, ok := s.handlers[req.Method]
	if !ok {
		if !isNotification {
			s.reply(req.ID, nil, &Error{Code: CodeMethodNotFound, Message: "method not found: " + req.Method})
		}
		return
	}

	result, err := handler(ctx, req.Params)
	if isNotification {
		if err != nil {
			s.log.Warn("notification handler failed", "method", req.Method, "err", err)
		}
		return
	}
	if err != nil {
		var rpcErr *Error
		if !errors.As(err, &rpcErr) {
			s.log.Error("call failed", "method", req.Method, "err", err)
			rpcErr = &Error{Code: CodeInternalError, Message: err.Error()}
		}
		s.reply(req.ID, nil, rpcErr)
		return
	}
	s.reply(req.ID, result, nil)
}

func (s *Server) reply(id json.RawMessage, result any, rpcErr *Error) {
	if rpcErr == nil && result == nil {
		// "result" is required on success; omitempty would drop a nil one.
		result = struct{}{}
	}
	if err := s.write(response{JSONRPC: "2.0", ID: id, Result: result, Error: rpcErr}); err != nil {
		s.log.Error("write response failed", "err", err)
	}
}

func (s *Server) write(msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("encode message: %w", err)
	}
	data = append(data, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()
	_, err = s.out.Write(data)
	return err
}
