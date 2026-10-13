package rpc

import (
	"context"
	"encoding/base64"
	"net/http"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/rpc/schema"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/app"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/domain"
)

func coverRoutes(s *Server, covers *app.Covers) {
	// Covers travel as data: URLs because the frontend can't load them from the server itself: a
	// self-signed certificate or plain HTTP (mixed content) blocks it, and offline there's nothing
	// to load from.
	Register(s, "get_cover", func(ctx context.Context, p schema.GetCoverParams) (schema.Cover, error) {
		size := domain.CoverSize(p.Size)
		if size != domain.CoverSmall && size != domain.CoverLarge {
			return schema.Cover{}, wireError(&domain.Error{Kind: domain.KindInvalidInput, Message: "A cover is small or large."})
		}
		data, err := covers.Cover(ctx, p.ID, size)
		if err != nil {
			return schema.Cover{}, wireError(err)
		}
		if len(data) == 0 {
			return schema.Cover{}, nil
		}
		return schema.Cover{DataURL: "data:" + http.DetectContentType(data) + ";base64," + base64.StdEncoding.EncodeToString(data)}, nil
	})
}
