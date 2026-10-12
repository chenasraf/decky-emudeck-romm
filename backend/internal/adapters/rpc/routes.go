package rpc

import (
	"context"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/rpc/schema"
)

// Routes registers a handler for every method in schema.Methods.
func Routes(s *Server, version string) {
	Register(s, "ping", func(context.Context, schema.PingParams) (schema.PingResult, error) {
		return schema.PingResult{Version: version}, nil
	})
}
