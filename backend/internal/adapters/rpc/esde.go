package rpc

import (
	"context"

	"github.com/chenasraf/decky-emudeck-romm/backend/internal/adapters/rpc/schema"
	"github.com/chenasraf/decky-emudeck-romm/backend/internal/app"
)

func esdeRoutes(s *Server, esde *app.ESDE) {
	Register(s, "esde_notice", func(context.Context, schema.ESDENoticeParams) (schema.ESDENotice, error) {
		if esde == nil {
			return schema.ESDENotice{}, nil
		}
		return toWireESDE(esde.Notice()), nil
	})
}

// ESDEEvents returns a listener that sends each batch ES-DE needs a rescan to see as an
// esde_rescan notification.
func ESDEEvents(s *Server) app.ESDEListener {
	return func(batch, notice app.ESDENotice) {
		event := schema.ESDERescanEvent{Batch: toWireESDE(batch), Notice: toWireESDE(notice)}
		if err := s.Notify("esde_rescan", event); err != nil {
			s.log.Error("send esde rescan", "err", err)
		}
	}
}

func toWireESDE(n app.ESDENotice) schema.ESDENotice {
	return schema.ESDENotice{Added: n.Added, Removed: n.Removed}
}
